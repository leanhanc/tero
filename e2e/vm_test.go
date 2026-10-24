//go:build e2e

// Package e2e runs Tero's end-to-end suite against the Lima test VM that
// scripts/vm-test.sh prepares, or the cloud server that scripts/ec2-test.sh
// prepares. Tests are named after the spec scenarios they cover, as
// "capability: Scenario".
package e2e

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func vmName() string {
	if name := os.Getenv("TERO_VM"); name != "" {
		return name
	}
	return "tero-ubuntu-26-04"
}

// sshTarget is the user@host of a cloud server to test over SSH instead of a
// Lima VM, and sshKey the private key that logs in to it.
func sshTarget() string { return os.Getenv("TERO_SSH") }
func sshKey() string    { return os.Getenv("TERO_SSH_KEY") }

// sshArgs logs in without prompts and gives up on a dead connection quickly,
// so waiting for a rebooting server doesn't hang. Commands share one
// connection: a new login to a distant server takes seconds, longer than
// some states the tests wait for last.
func sshArgs() []string {
	return []string{"-i", sshKey(), "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10", "-o", "ServerAliveInterval=15",
		"-o", "ControlMaster=auto", "-o", "ControlPath=/tmp/tero-e2e-ssh-%C", "-o", "ControlPersist=120",
		sshTarget()}
}

// result is a finished command in the VM.
type result struct {
	output   string
	exitCode int
}

// run runs a bash script in the VM as the default (sudo-capable) user.
func run(t *testing.T, script string) result {
	t.Helper()

	cmd := exec.Command("limactl", "shell", "--workdir", "/", vmName(), "bash", "-c", script)
	if sshTarget() != "" {
		// The script goes in on stdin, so the remote shell doesn't reparse it.
		cmd = exec.Command("ssh", append(sshArgs(), "cd / && bash -s")...)
		cmd.Stdin = strings.NewReader(script)
	}
	output, err := cmd.CombinedOutput()

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return result{output: string(output), exitCode: exitErr.ExitCode()}
	}
	if err != nil {
		t.Fatalf("run %q: %v\n%s", script, err, output)
	}

	return result{output: string(output)}
}

// must runs a script that has to succeed and returns its trimmed output.
func must(t *testing.T, script string) string {
	t.Helper()

	got := run(t, script)
	if got.exitCode != 0 {
		t.Fatalf("%q exited %d:\n%s", script, got.exitCode, got.output)
	}

	return strings.TrimSpace(got.output)
}

func assertContains(t *testing.T, haystack string, needles ...string) {
	t.Helper()
	for _, needle := range needles {
		if !strings.Contains(haystack, needle) {
			t.Errorf("missing %q in:\n%s", needle, haystack)
		}
	}
}

// etcChecksum fingerprints /etc and /usr/local, where init installs things,
// to prove a refused command changed nothing: every entry's type, mode, owner
// and symlink target, plus every regular file's contents. /var/lib/tero is
// left out because the running service writes there.
func etcChecksum(t *testing.T) string {
	t.Helper()
	return must(t, `paths="/etc /usr/local"
		{ sudo find $paths -xdev -printf '%y %m %u %g %p -> %l\n' 2>/dev/null | sort
		  sudo find $paths -xdev -type f -print0 2>/dev/null | sort -z | sudo xargs -0 sha256sum
		} | sha256sum`)
}
