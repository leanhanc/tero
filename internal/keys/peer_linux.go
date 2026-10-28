//go:build linux

package keys

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// soPeerPIDFD is SO_PEERPIDFD from asm-generic/socket.h (Linux 6.5), the
// value on amd64 and arm64. golang.org/x/sys doesn't name it yet.
const soPeerPIDFD = 77

// identify works out who connected. The uid is the one the kernel recorded at
// connect(), so a process that execs a setuid program afterwards is still
// what it was. Root and the agent are known by uid. For tero it takes a pidfd
// for the peer, reads that process's cgroup, and only then checks the pidfd
// still refers to a running process, so a pid reused after the peer exited
// can't pass as it. The check polls the pidfd rather than signalling it:
// tero-keys has no capabilities and another uid, so the kernel refuses it
// even signal 0 to a tero process.
func (p peers) identify(conn *net.UnixConn) (Caller, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return "", err
	}

	var credentials *unix.Ucred
	var pidfd int
	var socketErr error
	err = raw.Control(func(fd uintptr) {
		credentials, socketErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		if socketErr != nil {
			return
		}
		pidfd, socketErr = unix.GetsockoptInt(int(fd), unix.SOL_SOCKET, soPeerPIDFD)
	})
	if err != nil {
		return "", err
	}
	if socketErr != nil {
		return "", socketErr
	}
	defer unix.Close(pidfd)

	if credentials.Uid == 0 {
		return CallerRoot, nil
	}
	if int(credentials.Uid) == p.agentUID {
		return CallerAgent, nil
	}
	if int(credentials.Uid) != p.teroUID {
		return "", errUnknownCaller
	}

	pid, err := pidOf(pidfd)
	if err != nil {
		return "", err
	}
	cgroup, err := cgroupOf(pid)
	if err != nil {
		return "", err
	}
	if hasExited(pidfd) {
		return "", errors.New("the caller exited while connecting")
	}

	return p.callerFor(cgroup)
}

// hasExited reports whether the process behind pidfd has exited: a pidfd
// becomes readable when its process exits. Anything unexpected counts as
// exited, so the caller is refused.
func hasExited(pidfd int) bool {
	fds := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN}}
	for {
		ready, err := unix.Poll(fds, 0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return err != nil || ready != 0
	}
}

// pidOf reads the pid a pidfd refers to from its fdinfo.
func pidOf(pidfd int) (int, error) {
	info, err := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", pidfd))
	if err != nil {
		return 0, err
	}

	for _, line := range strings.Split(string(info), "\n") {
		value, isPid := strings.CutPrefix(line, "Pid:")
		if isPid {
			pid, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || pid <= 0 {
				return 0, errors.New("the caller has already exited")
			}
			return pid, nil
		}
	}

	return 0, errors.New("the kernel didn't report the caller's pid")
}

// cgroupOf returns the process's cgroup v2 path. Every supported release runs
// cgroup v2 only, so the file has one "0::<path>" line.
func cgroupOf(pid int) (string, error) {
	file, err := os.Open(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		path, isUnified := strings.CutPrefix(scanner.Text(), "0::")
		if isUnified {
			return path, nil
		}
	}

	return "", errors.New("the caller has no cgroup v2 path")
}
