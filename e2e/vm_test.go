//go:build e2e

// Package e2e runs Tero's end-to-end suite against the Lima test VM that
// scripts/vm-test.sh prepares. Tests are named after the spec scenarios they
// cover, as "capability: Scenario".
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

// result is a finished command in the VM.
type result struct {
	output   string
	exitCode int
}

// run runs a bash script in the VM as the default (sudo-capable) user.
func run(t *testing.T, script string) result {
	t.Helper()

	cmd := exec.Command("limactl", "shell", "--workdir", "/", vmName(), "bash", "-c", script)
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

// etcChecksum fingerprints every file under /etc, to prove a refused command
// changed nothing.
func etcChecksum(t *testing.T) string {
	t.Helper()
	return must(t, `sudo find /etc -xdev -type f -print0 | sort -z | sudo xargs -0 sha256sum | sha256sum`)
}
