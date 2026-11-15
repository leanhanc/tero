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
// by every child, that fails socket(AF_UNIX, ...) with EPERM. io_uring can
// create and connect a socket without the socket system call ever being
// made, so its three system calls fail the same way. So do syscalls from
// another architecture, such as 32-bit socketcall, and x32 system call
// numbers on amd64, which seccomp sees as the native architecture with bit 30
// set. Landlock has already set no_new_privs, which seccomp needs.
func refuseUnixSockets() error {
	filter, err := unixSocketFilter()
	if err != nil {
		return err
	}
	program := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}

	_, _, errno := unix.Syscall(unix.SYS_SECCOMP, unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC, uintptr(unsafe.Pointer(&program)))
	runtime.KeepAlive(filter)
	if errno != 0 {
		return fmt.Errorf("install the seccomp filter: %w", errno)
	}

	return nil
}

// x32SyscallBit marks an x32 system call number on amd64. No native system
// call number on either architecture reaches it.
const x32SyscallBit = 0x40000000

// refusedSyscalls fail whatever their arguments.
var refusedSyscalls = []uint32{unix.SYS_IO_URING_SETUP, unix.SYS_IO_URING_ENTER, unix.SYS_IO_URING_REGISTER}

// unixSocketFilter is the classic BPF program refuseUnixSockets installs.
func unixSocketFilter() ([]unix.SockFilter, error) {
	auditArch, err := nativeAuditArch()
	if err != nil {
		return nil, err
	}

	const (
		archOffset   = 4
		nrOffset     = 0
		arg0Offset   = 16
		returnDenied = unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)
		returnAllow  = unix.SECCOMP_RET_ALLOW
	)

	load := func(offset uint32) unix.SockFilter {
		return unix.SockFilter{Code: unix.BPF_LD | unix.BPF_W | unix.BPF_ABS, K: offset}
	}
	ret := func(value uint32) unix.SockFilter {
		return unix.SockFilter{Code: unix.BPF_RET | unix.BPF_K, K: value}
	}

	jump := func(op uint16, value uint32, ifTrue, ifFalse uint8) unix.SockFilter {
		return unix.SockFilter{Code: unix.BPF_JMP | op | unix.BPF_K, Jt: ifTrue, Jf: ifFalse, K: value}
	}

	filter := []unix.SockFilter{
		load(archOffset),
		jump(unix.BPF_JEQ, auditArch, 1, 0),
		ret(returnDenied),
		load(nrOffset),
	}

	// The checks of the system call number come next, then this ending:
	//
	//	jeq socket ? (skip 1) : allow
	//	allow:  ret allow
	//	        ld arg0
	//	        jeq AF_UNIX ? deny : (skip 1)
	//	deny:   ret deny
	//	        ret allow
	//
	// A jump counts the instructions it skips, so each check skips the checks
	// after it and the first four instructions of the ending to reach deny.
	type check struct {
		op    uint16
		value uint32
	}
	checks := []check{{unix.BPF_JGE, x32SyscallBit}}
	for _, refused := range refusedSyscalls {
		checks = append(checks, check{unix.BPF_JEQ, refused})
	}
	for index, c := range checks {
		checksAfter := len(checks) - index - 1
		filter = append(filter, jump(c.op, c.value, uint8(checksAfter+4), 0))
	}
	filter = append(filter,
		jump(unix.BPF_JEQ, uint32(unix.SYS_SOCKET), 1, 0),
		ret(returnAllow),
		load(arg0Offset),
		jump(unix.BPF_JEQ, unix.AF_UNIX, 0, 1),
		ret(returnDenied),
		ret(returnAllow),
	)

	return filter, nil
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
