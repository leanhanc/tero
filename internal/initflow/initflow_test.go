package initflow

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leanhanc/tero/internal/hostfs"
	"github.com/leanhanc/tero/internal/sysexec"
)

func writeHostFile(t *testing.T, files hostfs.FS, hostPath, contents string) {
	t.Helper()
	target := files.Path(hostPath)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newGuardHost(t *testing.T) host {
	t.Helper()
	files := hostfs.FS{Root: t.TempDir()}
	writeHostFile(t, files, "/usr/sbin/sshd", "")
	writeHostFile(t, files, "/etc/passwd", "root:x:0:0:root:/root:/bin/bash\nana:x:1000:1000::/home/ana:/bin/bash\nbo:x:1001:1001::/home/bo:/bin/bash\n")
	writeHostFile(t, files, "/etc/group", "root:x:0:\nsudo:x:27:ana\nbo:x:1001:\n")

	recorder := &sysexec.Recorder{Outputs: map[string]string{
		"sshd -T": "port 22\nauthorizedkeysfile .ssh/authorized_keys .ssh/authorized_keys2\n",
	}}

	return host{run: recorder, files: files}
}

func TestSSHLockoutRefusesWithoutKeys(t *testing.T) {
	h := newGuardHost(t)
	writeHostFile(t, h.files, "/home/ana/.ssh/authorized_keys", "# no keys yet\n")

	err := checkSSHLockout(context.Background(), h)
	if err == nil || !strings.Contains(err.Error(), "no user who can run sudo has an SSH key") {
		t.Fatalf("checkSSHLockout() = %v", err)
	}
}

func TestSSHLockoutPassesWithSudoGroupKey(t *testing.T) {
	h := newGuardHost(t)
	writeHostFile(t, h.files, "/home/ana/.ssh/authorized_keys2", "ssh-ed25519 AAAA ana@laptop\n")

	if err := checkSSHLockout(context.Background(), h); err != nil {
		t.Fatalf("checkSSHLockout() = %v", err)
	}
}

func TestSSHLockoutIgnoresKeysOfUsersWithoutSudo(t *testing.T) {
	h := newGuardHost(t)
	writeHostFile(t, h.files, "/home/bo/.ssh/authorized_keys", "ssh-ed25519 AAAA bo@laptop\n")

	if err := checkSSHLockout(context.Background(), h); err == nil {
		t.Fatal("a key for a user without sudo should not satisfy the guard")
	}
}

func TestSSHLockoutCountsTheSudoUser(t *testing.T) {
	h := newGuardHost(t)
	writeHostFile(t, h.files, "/home/bo/.ssh/authorized_keys", "ssh-ed25519 AAAA bo@laptop\n")
	t.Setenv("SUDO_USER", "bo")

	if err := checkSSHLockout(context.Background(), h); err != nil {
		t.Fatalf("checkSSHLockout() = %v", err)
	}
}

func TestExpandKeysPattern(t *testing.T) {
	ana := account{name: "ana", home: "/home/ana"}
	cases := map[string]string{
		".ssh/authorized_keys":   "/home/ana/.ssh/authorized_keys",
		"/etc/ssh/keys/%u":       "/etc/ssh/keys/ana",
		"%h/.ssh/keys":           "/home/ana/.ssh/keys",
		"/etc/ssh/100%%/%u.keys": "/etc/ssh/100%/ana.keys",
	}

	for pattern, want := range cases {
		if got := expandKeysPattern(pattern, ana); got != want {
			t.Errorf("expandKeysPattern(%q) = %q, want %q", pattern, got, want)
		}
	}
}

func TestNextFreeSubIDStart(t *testing.T) {
	ranges := parseSubIDs("ubuntu:100000:65536\nlima:165536:65536\ntero:231072:65536\n")

	got := nextFreeSubIDStart(ranges, "tero")
	if got != 262144 {
		t.Fatalf("nextFreeSubIDStart() = %d, want 262144", got)
	}
}

func TestNextFreeSubIDStartOnEmptyFile(t *testing.T) {
	if got := nextFreeSubIDStart(nil, "tero"); got != 131072 {
		t.Fatalf("nextFreeSubIDStart() = %d, want 131072", got)
	}
}

func TestNormalizeDomain(t *testing.T) {
	valid := map[string]string{
		"Dash.Example.com":      "dash.example.com",
		"dash.example.com.":     "dash.example.com",
		"203.0.113.10.sslip.io": "203.0.113.10.sslip.io",
	}
	for input, want := range valid {
		got, err := normalizeDomain(input)
		if err != nil || got != want {
			t.Errorf("normalizeDomain(%q) = %q, %v", input, got, err)
		}
	}

	for _, input := range []string{"localhost", "203.0.113.10", "-bad.example.com", "dash_board.example.com", "https://dash.example.com"} {
		if _, err := normalizeDomain(input); err == nil {
			t.Errorf("normalizeDomain(%q) should fail", input)
		}
	}
}

func TestResolveDomainUsesTheFlag(t *testing.T) {
	opts := Options{Domain: "Dash.Example.com"}

	got, err := resolveDomain(context.Background(), opts)
	if err != nil || got != "dash.example.com" {
		t.Fatalf("resolveDomain() = %q, %v", got, err)
	}
}

func TestResolveDomainPromptFallsBackOnEmptyAnswer(t *testing.T) {
	var out strings.Builder
	opts := Options{IsInteractive: true, In: strings.NewReader("\n"), Out: &out}

	got, err := resolveDomainWithIP(opts, netip.MustParseAddr("203.0.113.10"))
	if err != nil || got != "203.0.113.10.sslip.io" {
		t.Fatalf("resolveDomainWithIP() = %q, %v", got, err)
	}
	if !strings.Contains(out.String(), "press Enter to use 203.0.113.10.sslip.io") {
		t.Fatalf("prompt = %q", out.String())
	}
}

func TestResolveDomainPromptUsesTheAnswer(t *testing.T) {
	opts := Options{IsInteractive: true, In: strings.NewReader("Dash.Example.com\n"), Out: &strings.Builder{}}

	got, err := resolveDomainWithIP(opts, netip.MustParseAddr("203.0.113.10"))
	if err != nil || got != "dash.example.com" {
		t.Fatalf("resolveDomainWithIP() = %q, %v", got, err)
	}
}

func TestSSHSettingsKeepOnlySupportedKex(t *testing.T) {
	recorder := &sysexec.Recorder{Outputs: map[string]string{
		"ssh -Q kex": "diffie-hellman-group14-sha256\ncurve25519-sha256\ncurve25519-sha256@libssh.org\nsntrup761x25519-sha512@openssh.com\n",
	}}

	settings, err := sshSettings(context.Background(), host{run: recorder})
	if err != nil {
		t.Fatal(err)
	}

	want := "\nKexAlgorithms sntrup761x25519-sha512@openssh.com,curve25519-sha256,curve25519-sha256@libssh.org\n"
	if !strings.HasSuffix(string(settings), want) {
		t.Fatalf("settings end with:\n%s", settings[len(settings)-120:])
	}
}

func TestSSHSettingsRefuseWithoutAllowedKex(t *testing.T) {
	recorder := &sysexec.Recorder{Outputs: map[string]string{"ssh -Q kex": "diffie-hellman-group1-sha1\n"}}

	if _, err := sshSettings(context.Background(), host{run: recorder}); err == nil {
		t.Fatal("sshSettings() accepted a host with only weak key exchanges")
	}
}
