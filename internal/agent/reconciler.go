package agent

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/leanhanc/tero/internal/build"
	"github.com/leanhanc/tero/internal/containers"
	"github.com/leanhanc/tero/internal/deploys"
	"github.com/leanhanc/tero/internal/podman"
)

// keepLiveAppsRunning starts the container of every live release that isn't
// running: after the app's process exits, after a reboot, or if the
// container was removed by hand. A running container published on another
// port than the one recorded, left by an agent stopped mid-restart, has its
// port recorded so the proxy finds it.
func keepLiveAppsRunning(ctx context.Context, db *sql.DB) error {
	// Containers are listed before releases: a deploy promoted in between then
	// shows as a new release whose container looks stopped, which resuming
	// leaves alone, instead of an old release whose container is gone, which
	// would be recreated.
	runningPorts, err := containers.RunningPorts(ctx)
	if err != nil {
		return err
	}
	releases, err := deploys.SettledLiveReleases(ctx, db)
	if err != nil {
		return err
	}

	for _, release := range releases {
		hostPort, isRunning := runningPorts[release.Container]
		if !isRunning {
			startLiveApp(ctx, db, release)
			continue
		}

		hasMovedPort := hostPort != 0 && hostPort != release.HostPort
		if hasMovedPort {
			slog.Info("recording a live app's new port", "app", release.AppName, "port", hostPort)
			if err := deploys.RecordContainer(ctx, db, release.DeployID, release.Container, hostPort); err != nil {
				slog.Error("record live app's port", "app", release.AppName, "error", err)
			}
		}
	}

	return nil
}

// startLiveApp restarts the release unless a deploy replaced it or started
// for its app since the releases were listed: restarting other apps can take
// a while.
func startLiveApp(ctx context.Context, db *sql.DB, release deploys.Release) {
	isSettled, err := deploys.IsSettledLive(ctx, db, release.DeployID)
	if err != nil {
		slog.Error("check live app", "app", release.AppName, "error", err)
		return
	}
	if !isSettled {
		return
	}

	slog.Info("starting a live app that wasn't running", "app", release.AppName, "container", release.Container)
	if err := restartRelease(ctx, db, release); err != nil {
		slog.Error("start live app", "app", release.AppName, "error", err)
	}
}

// restartRelease resumes the release's container, or recreates it from the
// release's image when the container is gone.
func restartRelease(ctx context.Context, db *sql.DB, release deploys.Release) error {
	if err := containers.Resume(ctx, release.Container); err == nil {
		return nil
	}

	container, hostPort, err := containers.Start(ctx, release.AppName, release.DeployID, release.Image)
	if err != nil {
		return err
	}

	// The container already runs on the new port, so the port is recorded
	// even when the agent is stopping.
	return deploys.RecordContainer(context.WithoutCancel(ctx), db, release.DeployID, container, hostPort)
}

// removeLeftovers removes BuildKit daemons, app containers and app images
// that a previous agent process left behind when it stopped mid-deploy. Only
// the containers and images of live releases are kept.
func removeLeftovers(ctx context.Context, db *sql.DB) error {
	releases, err := deploys.LiveReleases(ctx, db)
	if err != nil {
		return err
	}
	liveDeployIDs := map[string]bool{}
	liveImages := map[string]bool{}
	for _, release := range releases {
		liveDeployIDs[strconv.FormatInt(release.DeployID, 10)] = true
		liveImages[release.Image] = true
	}

	buildkitNames, err := podman.Run(ctx, "ps", "--all", "--filter", "name=^tero-buildkit-", "--format", "{{.Names}}")
	if err != nil {
		return err
	}
	for _, name := range strings.Fields(buildkitNames) {
		if err := podman.RemoveContainer(ctx, name); err != nil {
			return err
		}
	}

	appContainers, err := podman.Run(ctx, "ps", "--all", "--filter", "label="+containers.AppLabel,
		"--format", fmt.Sprintf(`{{.Names}} {{index .Labels "%s"}}`, containers.DeployLabel))
	if err != nil {
		return err
	}
	for _, line := range strings.Split(appContainers, "\n") {
		name, deployID, hasDeployID := strings.Cut(strings.TrimSpace(line), " ")
		if !hasDeployID || liveDeployIDs[deployID] {
			continue
		}
		if err := podman.RemoveContainer(ctx, name); err != nil {
			return err
		}
	}

	return removeLeftoverImages(ctx, liveImages)
}

// removeLeftoverImages removes app images no live release uses, such as the
// previous release's image when the agent stopped before removing it.
func removeLeftoverImages(ctx context.Context, liveImages map[string]bool) error {
	images, err := podman.Run(ctx, "images", "--format", "{{.Repository}}:{{.Tag}}")
	if err != nil {
		return err
	}

	for _, image := range strings.Fields(images) {
		isAppImage := strings.HasPrefix(image, build.ImagePrefix)
		if !isAppImage || liveImages[image] {
			continue
		}
		if _, err := podman.Run(ctx, "rmi", "--ignore", image); err != nil {
			return err
		}
	}

	return nil
}
