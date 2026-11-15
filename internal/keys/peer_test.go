//go:build linux

package keys

import (
	"os"
	"os/exec"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestHasExitedSeesARunningProcess(t *testing.T) {
	pidfd, err := unix.PidfdOpen(os.Getpid(), 0)
	if err != nil {
		t.Skip("pidfd_open isn't available:", err)
	}
	defer syscall.Close(pidfd)

	if hasExited(pidfd) {
		t.Error("this process counts as exited")
	}
}

func TestHasExitedSeesAnExitedProcess(t *testing.T) {
	command := exec.Command("true")
	if err := command.Start(); err != nil {
		t.Skip(err)
	}
	pidfd, err := unix.PidfdOpen(command.Process.Pid, 0)
	if err != nil {
		t.Skip("pidfd_open isn't available:", err)
	}
	defer syscall.Close(pidfd)
	command.Wait()

	if !hasExited(pidfd) {
		t.Error("an exited process counts as running")
	}
}
