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
	if _, err := Run(ctx, "run", "--detach", "--userns=auto", "image"); err == nil {
		t.Fatal("Run succeeded after its context ran out")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("Run returned after %s, want soon after its %s wait delay", elapsed, waitDelay)
	}
}

// process-isolation: Containers never run as the agent's uid
func TestRunRefusesContainersThatCouldMapTheAgentsUID(t *testing.T) {
	refused := map[string][]string{
		"no namespace option":   {"run", "image"},
		"host namespace":        {"run", "--userns=host", "image"},
		"keep-id":               {"run", "--userns", "keep-id", "image"},
		"map onto own uid":      {"run", "--uidmap", "0:0:1", "--gidmap", "0:1:1", "image"},
		"uid map only":          {"run", "--uidmap", "0:1:1000", "image"},
		"map in one argument":   {"run", "--uidmap=0:1:1000", "--gidmap=0:1:1000", "image"},
		"named subordinate ids": {"run", "--subuidname", "someone", "image"},
		"auto and maps":         {"run", "--userns=auto", "--uidmap", "0:1:10", "--gidmap", "0:1:10", "image"},
		"create":                {"create", "--userns=auto", "image"},
		"container run":         {"container", "run", "--userns=auto", "image"},
		"run behind a flag":     {"--log-level=debug", "run", "image"},
		"kube play":             {"kube", "play", "pod.yaml"},
	}
	for name, args := range refused {
		t.Run(name, func(t *testing.T) {
			if err := checkUserNamespace(args); err == nil {
				t.Errorf("podman %v was allowed", args)
			}
		})
	}

	allowed := [][]string{
		{"run", "--detach", "--userns=auto", "image"},
		{"run", "--uidmap", "0:800001:200000", "--gidmap", "0:800001:200000", "image"},
		{"rm", "--force", "name"},
	}
	for _, args := range allowed {
		if err := checkUserNamespace(args); err != nil {
			t.Errorf("podman %v was refused: %v", args, err)
		}
	}
}
