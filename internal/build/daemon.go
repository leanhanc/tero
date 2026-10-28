package build

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/moby/buildkit/client"
	_ "github.com/moby/buildkit/client/connhelper/podmancontainer"

	"github.com/leanhanc/tero/internal/podman"
)

// BuildKitImage is pinned by index digest to the release of the BuildKit
// client Railpack compiles against, so the daemon understands its LLB.
const BuildKitImage = "docker.io/moby/buildkit:v0.33.0-rootless@sha256:80b15f0735e87bab7bf59ec4d695dfb4a7cfb25521cf56dc75d6f256285b63ef"

// CacheVolume keeps BuildKit's state between throwaway daemons, so base
// images and package layers are reused.
const CacheVolume = "tero-buildkit-cache"

// buildkitIDs is the slice of tero-agent's 1,000,000 subordinate ids
// BuildKit runs in: the top 200,000, enough for rootlesskit's nested range
// (100000-165535). Apps get --userns=auto slices, which Podman hands out from
// the bottom of the range and never from a running container's map, so
// BuildKit and apps don't share host uids. A fixed map also keeps the cache
// volume's files owned by the same ids on every build.
const buildkitIDs = "0:800001:200000"

const minimumMemoryLimit = 1 << 30

// PullImage downloads the BuildKit image when it's missing, so the first
// deploy doesn't wait for it. A deploy that starts during the pull may
// download the same layers alongside it, which is only slower.
func PullImage(ctx context.Context) error {
	// podman image exists answers through its exit code alone: 1 is missing.
	if _, err := podman.Run(ctx, "image", "exists", BuildKitImage); err == nil {
		return nil
	}

	_, err := podman.Run(ctx, "pull", "--quiet", BuildKitImage)
	return err
}

type daemon struct {
	name   string
	client *client.Client
}

// startDaemon starts a fresh rootless BuildKit container and waits until it
// answers. The client reaches it through `podman exec ... buildctl
// dial-stdio`, so it publishes no port and exposes no socket.
func startDaemon(ctx context.Context, deployID int64) (*daemon, error) {
	name := fmt.Sprintf("tero-buildkit-%d", deployID)
	if err := podman.RemoveContainer(ctx, name); err != nil {
		return nil, err
	}

	_, err := podman.Run(ctx, "run", "--detach", "--rm", "--name", name,
		// runc inside BuildKit sets each RUN step's hostname and mounts a
		// fresh /proc; Podman's default seccomp profile and masked /proc paths
		// forbid both. BuildKit still applies its own seccomp profile to steps.
		"--security-opt", "seccomp=unconfined",
		"--security-opt", "unmask=/proc/*",
		"--uidmap", buildkitIDs, "--gidmap", buildkitIDs,
		"--memory", strconv.FormatInt(memoryLimit(), 10),
		"--volume", CacheVolume+":/home/user/.local/share/buildkit",
		BuildKitImage)
	if err != nil {
		return nil, fmt.Errorf("start BuildKit: %w", err)
	}

	buildkitClient, err := client.New(ctx, "podman-container://"+name)
	if err != nil {
		_ = podman.RemoveContainer(context.WithoutCancel(ctx), name)
		return nil, fmt.Errorf("connect to BuildKit: %w", err)
	}

	started := &daemon{name: name, client: buildkitClient}
	if err := started.waitReady(ctx); err != nil {
		started.stop()
		return nil, err
	}

	return started, nil
}

func (d *daemon) waitReady(ctx context.Context) error {
	deadline := time.Now().Add(30 * time.Second)
	for {
		infoCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, err := d.client.Info(infoCtx)
		cancel()
		if err == nil {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("BuildKit never became ready: %w", err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("BuildKit never became ready: %w", ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// hitMemoryLimit reports whether the kernel killed anything in the
// container for going over its memory limit.
func (d *daemon) hitMemoryLimit(ctx context.Context) bool {
	cgroupPath, err := podman.Run(ctx, "inspect", "--format", "{{.State.CgroupPath}}", d.name)
	if err != nil {
		return false
	}

	events, err := os.Open("/sys/fs/cgroup" + cgroupPath + "/memory.events")
	if err != nil {
		return false
	}
	defer events.Close()

	scanner := bufio.NewScanner(events)
	for scanner.Scan() {
		key, value, _ := strings.Cut(scanner.Text(), " ")
		if key == "oom_kill" && value != "0" {
			return true
		}
	}

	return false
}

// stop removes the container even when the build's context was cancelled.
func (d *daemon) stop() {
	_ = d.client.Close()
	_ = podman.RemoveContainer(context.Background(), d.name)
}

// memoryLimit is half the server's memory, and at least 1 GiB.
func memoryLimit() int64 {
	total := totalMemory()
	return max(total/2, minimumMemoryLimit)
}

func totalMemory() int64 {
	meminfo, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}

	for _, line := range strings.Split(string(meminfo), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "MemTotal:" {
			kibibytes, _ := strconv.ParseInt(fields[1], 10, 64)
			return kibibytes * 1024
		}
	}

	return 0
}
