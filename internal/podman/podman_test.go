package podman

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// e2e finding: on a slow disk, podman run's chown child kept podman's output
// pipes open after its context ran out, so the deploy waited forever.
func TestRunReturnsWhenAChildHoldsTheOutputOpen(t *testing.T) {
	bin := t.TempDir()
	childPID := filepath.Join(bin, "child.pid")
	fakePodman := "#!/bin/sh\nsleep 30 &\necho $! > " + childPID + "\nwait\n"
	if err := os.WriteFile(filepath.Join(bin, "podman"), []byte(fakePodman), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	t.Cleanup(func() {
		if pid, err := os.ReadFile(childPID); err == nil {
			_ = exec.Command("kill", strings.TrimSpace(string(pid))).Run()
		}
	})

	previousDelay := waitDelay
	waitDelay = 200 * time.Millisecond
	t.Cleanup(func() { waitDelay = previousDelay })

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	started := time.Now()
	if _, err := Run(ctx, "run", "--detach", "image"); err == nil {
		t.Fatal("Run succeeded after its context ran out")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("Run returned after %s, want soon after its %s wait delay", elapsed, waitDelay)
	}
}
