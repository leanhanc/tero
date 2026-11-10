//go:build linux

package build

import (
	"fmt"
	"runtime"
	"unsafe"

	"github.com/landlock-lsm/go-landlock/landlock"
	llsyscall "github.com/landlock-lsm/go-landlock/landlock/syscall"
	"golang.org/x/sys/unix"
)

// minimumLandlockABI is Landlock ABI 3 (Linux 6.2), the oldest that confines
// every file operation the planner needs: a kernel without it fails the
// build rather than plan unconfined.
const minimumLandlockABI = 3

// confineToSource lets this process and every process it starts read only
// the app's folder and the system files mise needs, and write only to mise's
// directory. Landlock also stops the child from reading other processes'
// memory or environment through /proc. Unix sockets are refused outright,
// since Landlock covers them only from ABI 9, and tero-agent's D-Bus would
// let the child start anything outside the sandbox.
func confineToSource(sourceDir string) error {
	abi, err := llsyscall.LandlockGetABIVersion()
	if err != nil {
		return fmt.Errorf("this kernel has no Landlock: %w", err)
	}
	if abi < minimumLandlockABI {
		return fmt.Errorf("this kernel has Landlock ABI %d; planning a build needs %d or later", abi, minimumLandlockABI)
	}

	err = landlock.V11.BestEffort().RestrictPaths(
		landlock.RODirs(sourceDir),
		landlock.RWDirs(miseRoot()),
		landlock.RODirs("/usr", "/bin", "/sbin", "/lib", "/lib64", "/etc", "/proc", "/run/systemd/resolve").IgnoreIfMissing(),
		landlock.RWFiles("/dev/null"),
		landlock.ROFiles("/dev/urandom"),
	)
	if err != nil {
		return err
	}
	if err := landlock.V11.BestEffort().RestrictScoped(); err != nil {
		return err
	}

	return refuseUnixSockets()
}

// refuseUnixSockets installs a seccomp filter, on every thread and inherited
// by every child, that fails socket(AF_UNIX, ...) with EPERM. Syscalls from
// another architecture, such as 32-bit socketcall, fail the same way.
// Landlock has already set no_new_privs, which seccomp needs.
func refuseUnixSockets() error {
	auditArch, err := nativeAuditArch()
	if err != nil {
		return err
	}

	const (
		archOffset   = 4
		nrOffset     = 0
		arg0Offset   = 16
		returnDenied = unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)
		returnAllow  = unix.SECCOMP_RET_ALLOW
	)
	filter := []unix.SockFilter{
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: archOffset},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jt: 1, Jf: 0, K: auditArch},
		{Code: unix.BPF_RET | unix.BPF_K, K: returnDenied},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: nrOffset},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jt: 0, Jf: 3, K: uint32(unix.SYS_SOCKET)},
		{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: arg0Offset},
		{Code: unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K, Jt: 0, Jf: 1, K: unix.AF_UNIX},
		{Code: unix.BPF_RET | unix.BPF_K, K: returnDenied},
		{Code: unix.BPF_RET | unix.BPF_K, K: returnAllow},
	}
	program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}

	_, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&program)))
	runtime.KeepAlive(filter)
	if errno != 0 {
		return fmt.Errorf("install the seccomp filter: %w", errno)
	}

	return nil
}

// nativeAuditArch is the seccomp architecture of the released binaries.
func nativeAuditArch() (uint32, error) {
	switch runtime.GOARCH {
	case "amd64":
		return unix.AUDIT_ARCH_X86_64, nil
	case "arm64":
		return unix.AUDIT_ARCH_AARCH64, nil
	}

	return 0, fmt.Errorf("no seccomp filter for %s", runtime.GOARCH)
}
