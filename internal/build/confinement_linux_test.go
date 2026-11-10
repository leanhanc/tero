//go:build linux

package build

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

// confinementHelperEnv makes the test binary act as a confined planner:
// Landlock can't be lifted, so the check runs in a child process.
const confinementHelperEnv = "TERO_CONFINEMENT_HELPER"

func TestConfinedPlannerCannotFollowALinkOutOfTheApp(t *testing.T) {
	if sourceDir := os.Getenv(confinementHelperEnv); sourceDir != "" {
		runConfinementHelper(sourceDir)
		return
	}

	root := t.TempDir()
	sourceDir := filepath.Join(root, "app")
	outside := filepath.Join(root, "other-app.env")
	if err := os.Mkdir(sourceDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("SECRET=canary"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(sourceDir, "Procfile")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "package.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestConfinedPlannerCannotFollowALinkOutOfTheApp$")
	cmd.Env = append(os.Environ(), confinementHelperEnv+"="+sourceDir)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("helper: %v\n%s", err, output)
	}
}

// runConfinementHelper exits 0 only when the app's own file reads and the
// linked file outside it doesn't.
func runConfinementHelper(sourceDir string) {
	if err := ensureMiseDirectory(); err != nil {
		println("mise directory:", err.Error())
		os.Exit(2)
	}
	if err := confineToSource(sourceDir); err != nil {
		println("confine:", err.Error())
		os.Exit(2)
	}
	if _, err := os.ReadFile(filepath.Join(sourceDir, "package.json")); err != nil {
		println("read the app's own file:", err.Error())
		os.Exit(3)
	}
	if _, err := os.ReadFile(filepath.Join(sourceDir, "Procfile")); err == nil {
		println("read the linked file outside the app")
		os.Exit(4)
	}
	if _, err := os.ReadFile(filepath.Join(sourceDir, "..", "other-app.env")); err == nil {
		println("read a file through ../")
		os.Exit(5)
	}
	if _, err := net.Dial("unix", "/run/dbus/system_bus_socket"); err == nil || !errors.Is(err, syscall.EPERM) {
		println("a Unix socket wasn't refused:", fmt.Sprint(err))
		os.Exit(6)
	}
	os.Exit(0)
}
