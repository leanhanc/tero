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
)

// Run runs podman with args and returns its trimmed standard output. On
// failure the error carries podman's standard error. Arguments are never
// passed through a shell.
func Run(ctx context.Context, args ...string) (string, error) {
	return run(exec.CommandContext(ctx, "podman", args...), args)
}

// RunInOwnScope runs podman in a transient systemd scope of its own, so the
// conmon and port-forwarding processes it leaves behind for a container don't
// belong to the agent's unit. Otherwise restarting the agent would kill them
// and cut every app off from the network.
func RunInOwnScope(ctx context.Context, scope string, args ...string) (string, error) {
	scopeArgs := append([]string{"--user", "--scope", "--quiet", "--collect", "--unit=" + scope, "podman"}, args...)
	return run(exec.CommandContext(ctx, "systemd-run", scopeArgs...), args)
}

func run(cmd *exec.Cmd, args []string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

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
