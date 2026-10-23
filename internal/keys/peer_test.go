//go:build linux

package keys

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// Uids for the helper processes. The server runs as a user other than the
// callers and, like tero-keys, without capabilities.
const (
	helperServerUID = 65533
	helperTeroUID   = 65534
	helperOtherUID  = 65532
)

// key-service: Other process running as tero
func TestIdentifyAcrossUsers(t *testing.T) {
	if os.Getuid() != 0 {
		t.Skip("needs root to run the server and callers as other users")
	}

	dir, err := os.MkdirTemp("", "tero-keys-peer")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	os.Chmod(dir, 0o777)

	binary := filepath.Join(dir, "keys.test")
	copyFile(t, os.Args[0], binary)
	socket := filepath.Join(dir, "keys.sock")
	cgroup, err := cgroupOf(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}

	server := helper(binary, helperServerUID, "serve", socket, cgroup)
	output, err := server.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	server.Stderr = os.Stderr
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Process.Kill(); server.Wait() })
	lines := bufio.NewScanner(output)

	if !lines.Scan() || lines.Text() != "ready" {
		t.Fatalf("server didn't start: %q", lines.Text())
	}

	for _, test := range []struct {
		uid  uint32
		want string
	}{
		{helperTeroUID, string(CallerDashboard)},
		{helperOtherUID, "refused"},
	} {
		client := helper(binary, test.uid, "connect", socket, "")
		client.Stderr = os.Stderr
		if err := client.Start(); err != nil {
			t.Fatal(err)
		}
		if !lines.Scan() {
			t.Fatal("the server stopped")
		}
		if got := lines.Text(); got != test.want {
			t.Errorf("uid %d identified as %q, want %q", test.uid, got, test.want)
		}
		client.Process.Kill()
		client.Wait()
	}
}

func helper(binary string, uid uint32, mode, socket, cgroup string) *exec.Cmd {
	command := exec.Command(binary, "-test.run=^TestPeerHelper$")
	command.Env = []string{
		"KEYS_PEER_HELPER=" + mode,
		"KEYS_PEER_SOCKET=" + socket,
		"KEYS_PEER_CGROUP=" + cgroup,
		"KEYS_PEER_TERO_UID=" + strconv.Itoa(helperTeroUID),
	}
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: uid}}
	return command
}

// TestPeerHelper is the server or a caller for TestIdentifyAcrossUsers.
func TestPeerHelper(t *testing.T) {
	socket := os.Getenv("KEYS_PEER_SOCKET")
	switch os.Getenv("KEYS_PEER_HELPER") {
	case "serve":
		teroUID, _ := strconv.Atoi(os.Getenv("KEYS_PEER_TERO_UID"))
		peers := peers{teroUID: teroUID, dashboard: os.Getenv("KEYS_PEER_CGROUP")}

		listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		os.Chmod(socket, 0o777)
		fmt.Println("ready")
		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				os.Exit(1)
			}
			caller, err := peers.identify(conn)
			if err != nil {
				fmt.Println("refused")
			} else {
				fmt.Println(caller)
			}
			conn.Close()
		}
	case "connect":
		conn, err := net.Dial("unix", socket)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		io.Copy(io.Discard, conn)
		os.Exit(0)
	default:
		t.Skip("only runs as a helper process")
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	contents, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, contents, 0o755); err != nil {
		t.Fatal(err)
	}
}

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
