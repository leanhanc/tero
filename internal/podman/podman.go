// Package podman runs the podman CLI as the current user, which for Tero is
// always the agent running as tero. The CLI is used instead of Podman's Go
// bindings because it behaves the same on every tested Podman version.
package podman

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// waitDelay bounds the wait for podman's output pipes once its context is
// done. A child podman leaves behind, like the chown of an image into a
// container's user namespace, can hold them open after podman is killed.
var waitDelay = 10 * time.Second

// Run runs podman with args and returns its trimmed standard output. On
// failure the error carries podman's standard error. Arguments are never
// passed through a shell.
func Run(ctx context.Context, args ...string) (string, error) {
	return run(exec.CommandContext(ctx, "podman", args...), args)
}

// RunCombined runs podman and returns its stdout and stderr together, in the
// order they were written. podman logs replays a container's stderr on its
// own stderr, so Run would drop it.
func RunCombined(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "podman", args...)
	cmd.WaitDelay = waitDelay
	output, err := cmd.CombinedOutput()
	if err != nil {
		subcommand := strings.Join(args[:min(len(args), 2)], " ")
		return "", fmt.Errorf("podman %s: %w: %s", subcommand, err, strings.TrimSpace(string(output)))
	}

	return strings.TrimSpace(string(output)), nil
}

// RunInOwnScope runs podman in a transient systemd scope of its own, so the
// conmon and port-forwarding processes it leaves behind for a container don't
// belong to the agent's unit. Otherwise restarting the agent would kill them
// and cut every app off from the network.
func RunInOwnScope(ctx context.Context, scope string, args ...string) (string, error) {
	scopeArgs := append([]string{"--user", "--scope", "--quiet", "--collect", "--unit=" + scope, "podman"}, args...)
	return run(exec.CommandContext(ctx, "systemd-run", scopeArgs...), args)
}

// KillScope kills every process still in a scope RunInOwnScope started. A
// plain stop would wait 90 seconds for processes stuck on the disk before
// killing them.
func KillScope(ctx context.Context, scope string) error {
	output, err := exec.CommandContext(ctx, "systemctl", "--user", "kill", "--signal=SIGKILL", scope).CombinedOutput()
	if err != nil {
		return fmt.Errorf("kill %s: %w: %s", scope, err, strings.TrimSpace(string(output)))
	}

	return nil
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
