// Package podman runs the podman CLI as a client of the Podman service that
// tero-podman's user manager runs. tero-podman owns every image and container,
// and the network helpers Podman starts for them (pasta or slirp4netns, and
// rootlessport), which carry the apps' traffic. The agent, which tero-keys
// trusts with every value, runs as tero-agent, so a compromised helper can't
// ask tero-keys for anything. The CLI is used instead of Podman's Go bindings
// because it behaves the same on every tested Podman version.
package podman

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ServiceURL is the Podman service's socket. Only tero-podman and the members
// of its group, which is tero-agent alone, can open it.
const ServiceURL = "unix:///run/tero-podman/podman.sock"

// UseService points every podman command this process and its children run,
// BuildKit's connection helper included, at the Podman service: with
// CONTAINER_HOST set, the podman CLI is a remote client.
func UseService() error {
	return os.Setenv("CONTAINER_HOST", ServiceURL)
}

// waitDelay bounds the wait for podman's output pipes once its context is
// done. A child podman leaves behind, like the chown of an image into a
// container's user namespace, can hold them open after podman is killed.
var waitDelay = 10 * time.Second

// Run runs podman with args and returns its trimmed standard output. On
// failure the error carries podman's standard error. Arguments are never
// passed through a shell.
func Run(ctx context.Context, args ...string) (string, error) {
	if err := checkUserNamespace(args); err != nil {
		return "", err
	}

	return run(exec.CommandContext(ctx, "podman", args...), args)
}

// RunCombined runs podman and returns its stdout and stderr together, in the
// order they were written. podman logs replays a container's stderr on its
// own stderr, so Run would drop it.
func RunCombined(ctx context.Context, args ...string) (string, error) {
	if err := checkUserNamespace(args); err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "podman", args...)
	cmd.WaitDelay = waitDelay
	output, err := cmd.CombinedOutput()
	if err != nil {
		subcommand := strings.Join(args[:min(len(args), 2)], " ")
		return "", fmt.Errorf("podman %s: %w: %s", subcommand, err, strings.TrimSpace(string(output)))
	}

	return strings.TrimSpace(string(output)), nil
}

func run(cmd *exec.Cmd, args []string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = waitDelay

	if err := cmd.Run(); err != nil {
		subcommand := strings.Join(args[:min(len(args), 2)], " ")
		return "", fmt.Errorf("podman %s: %w: %s", subcommand, err, strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(stdout.String()), nil
}

// RemoveContainer stops and removes a container if it exists.
func RemoveContainer(ctx context.Context, name string) error {
	_, err := Run(ctx, "rm", "--force", "--ignore", "--time", "10", name)
	return err
}

// ErrOwnUIDMapped refuses a container that could run as tero-podman's own
// uid, which owns Podman's storage and every other container, so no container
// may ever run as it.
var ErrOwnUIDMapped = errors.New("a container must run only as subordinate ids, never as tero-podman's own uid")

// checkUserNamespace refuses `podman run` unless the container gets an
// automatic user namespace or explicit uid and gid maps that leave out the
// service user's own uid. In rootless Podman, host id 0 of a map is the user's own
// uid, and the subordinate ids start at 1; without a namespace option,
// container root is the user's own uid.
func checkUserNamespace(args []string) error {
	subcommand := firstSubcommand(args)
	if containerMakers[subcommand] {
		return ErrOwnUIDMapped
	}
	if subcommand != "run" {
		return nil
	}
	// Global flags before run aren't parsed here, so they aren't allowed.
	if args[0] != "run" {
		return ErrOwnUIDMapped
	}

	var hasAutoNamespace, hasUIDMap, hasGIDMap bool
	for index, arg := range args {
		value := ""
		if index+1 < len(args) {
			value = args[index+1]
		}

		switch {
		case arg == "--userns=auto":
			hasAutoNamespace = true
		case strings.HasPrefix(arg, "--userns"), strings.HasPrefix(arg, "--subuidname"), strings.HasPrefix(arg, "--subgidname"):
			return ErrOwnUIDMapped
		case arg == "--uidmap":
			if !mapsOnlySubordinateIDs(value) {
				return ErrOwnUIDMapped
			}
			hasUIDMap = true
		case arg == "--gidmap":
			if !mapsOnlySubordinateIDs(value) {
				return ErrOwnUIDMapped
			}
			hasGIDMap = true
		case strings.HasPrefix(arg, "--uidmap="), strings.HasPrefix(arg, "--gidmap="):
			return ErrOwnUIDMapped
		}
	}

	hasExplicitMaps := hasUIDMap && hasGIDMap
	if hasAutoNamespace == hasExplicitMaps {
		return ErrOwnUIDMapped
	}

	return nil
}

// containerMakers are the other subcommands that create containers. Tero
// never uses them, so they are refused rather than checked.
var containerMakers = map[string]bool{"create": true, "container": true, "pod": true, "kube": true, "play": true}

// firstSubcommand is the first argument that isn't a flag.
func firstSubcommand(args []string) string {
	for _, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			return arg
		}
	}

	return ""
}

// mapsOnlySubordinateIDs reports whether a container:host:size map starts at
// host id 1 or above.
func mapsOnlySubordinateIDs(mapping string) bool {
	fields := strings.Split(mapping, ":")
	if len(fields) != 3 {
		return false
	}

	hostStart, err := strconv.Atoi(fields[1])
	return err == nil && hostStart >= 1
}
