//go:build linux

package build

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
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
	if err := os.MkdirAll(miseRoot(), 0o700); err != nil {
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
	if errno := ioUringSetup(); errno != syscall.EPERM {
		println("io_uring_setup wasn't refused:", errno.Error())
		os.Exit(7)
	}
	if runtime.GOARCH == "amd64" {
		_, _, errno := unix.RawSyscall(unix.SYS_SOCKET|x32SyscallBit, unix.AF_UNIX, unix.SOCK_STREAM, 0)
		if errno != syscall.EPERM {
			println("an x32 socket call wasn't refused:", errno.Error())
			os.Exit(8)
		}
	}
	if _, err := net.Dial("tcp", "127.0.0.1:1"); errors.Is(err, syscall.EPERM) {
		println("a TCP socket was refused:", err.Error())
		os.Exit(9)
	}
	os.Exit(0)
}

// ioUringSetup asks for an io_uring with one entry; without the filter it
// succeeds or fails for another reason than EPERM.
func ioUringSetup() syscall.Errno {
	var params [120]byte
	fd, _, errno := unix.RawSyscall(unix.SYS_IO_URING_SETUP, 1, uintptr(unsafe.Pointer(&params)), 0)
	if errno == 0 {
		unix.Close(int(fd))
	}
	return errno
}
