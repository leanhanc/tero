package sysexec

import (
	"context"
	"strings"
	"testing"
)

// Review finding: commands were resolved through the caller's PATH and ran
// with the caller's environment.
func TestHostIgnoresCallersPathAndEnvironment(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("TERO_TEST_LEAK", "leaked")

	output, err := Host{Env: []string{"LC_ALL=C"}}.Run(context.Background(), "env")
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(output, "TERO_TEST_LEAK") {
		t.Error("the caller's environment reached the command")
	}
	if !strings.Contains(output, "PATH=/usr/sbin:/usr/bin:/sbin:/bin") || !strings.Contains(output, "LC_ALL=C") {
		t.Errorf("unexpected environment:\n%s", output)
	}
}

func TestHostRefusesCommandsOutsideSystemDirs(t *testing.T) {
	if _, err := (Host{}).Run(context.Background(), "definitely-not-a-tero-command"); err == nil {
		t.Fatal("an unknown command ran")
	}
}

// Real-VPS finding: commands inherited the caller's folder, and `podman` run
// as tero failed in /home/ubuntu, which tero can't enter.
func TestHostRunsCommandsFromRoot(t *testing.T) {
	t.Chdir(t.TempDir())

	output, err := (Host{}).Run(context.Background(), "pwd")
	if err != nil {
		t.Fatal(err)
	}

	if output != "/" {
		t.Errorf("command ran in %q, want /", output)
	}
}
