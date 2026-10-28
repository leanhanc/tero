//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"
)

// keysStatus asks the key service for its status over its socket, the way
// the dashboard does: a 4-byte big-endian length, then JSON. The key service
// closes the connection without an answer when it refuses a caller, so the
// probe then exits non-zero.
const keysStatus = `python3 -c 'import json, socket, struct
s = socket.socket(socket.AF_UNIX)
s.connect("/run/tero-keys/keys.sock")
body = json.dumps({"verb": "status"}).encode()
s.sendall(struct.pack(">I", len(body)) + body)
size = struct.unpack(">I", s.recv(4))[0]
print(s.recv(size).decode())'`

// inDashboardCgroup runs a command as tero from inside tero.service's cgroup,
// which is what a compromised dashboard can do.
func inDashboardCgroup(command string) string {
	return `sudo sh -c 'echo $$ > /sys/fs/cgroup/system.slice/tero.service/cgroup.procs && exec setpriv --reuid tero --regid tero --init-groups "$0" -c "$1"' bash ` + shellQuote(command)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func testKeyServiceRunning(t *testing.T) {
	must(t, "systemctl is-enabled tero-keys.socket")
	must(t, "systemctl is-active tero-keys.socket")
	assertContains(t, must(t, "sudo "+keysStatus), `"keyVersions":[1]`)
	must(t, "systemctl is-active tero-keys")
}

func testKeyServiceIdentity(t *testing.T) {
	keysUID := must(t, "id -u tero-keys")
	status := must(t, "sudo cat /proc/$(systemctl show tero-keys --property=MainPID --value)/status")

	fields := map[string]string{}
	for _, line := range strings.Split(status, "\n") {
		key, value, _ := strings.Cut(line, ":")
		fields[key] = strings.TrimSpace(value)
	}
	if uids := strings.Fields(fields["Uid"]); len(uids) == 0 || uids[0] != keysUID {
		t.Errorf("key service uid = %q, want %s", fields["Uid"], keysUID)
	}
	for _, capabilitySet := range []string{"CapEff", "CapPrm", "CapBnd"} {
		if fields[capabilitySet] != "0000000000000000" {
			t.Errorf("%s = %s, want no capabilities", capabilitySet, fields[capabilitySet])
		}
	}

	if got := must(t, "stat -c '%U:%G %a' /run/tero-keys/keys.sock"); got != "tero-keys:tero-keys-callers 660" {
		t.Errorf("socket is %q, want tero-keys:tero-keys-callers 660", got)
	}

	keysPID := must(t, "systemctl show tero-keys --property=MainPID --value")
	if got := run(t, "sudo ss -Hlnp -A inet | grep 'pid="+keysPID+",'"); got.exitCode == 0 {
		t.Errorf("key service listens on the network:\n%s", got.output)
	}
}

func testKeysUserNoLogin(t *testing.T) {
	if shell := must(t, "getent passwd tero-keys | cut -d: -f7"); shell != "/usr/sbin/nologin" {
		t.Errorf("tero-keys shell = %q, want /usr/sbin/nologin", shell)
	}
	if state := must(t, "sudo passwd -S tero-keys | cut -d' ' -f2"); state != "L" {
		t.Errorf("tero-keys password state = %q, want L (locked)", state)
	}
	if groups := must(t, "id -nG tero-keys"); groups != "tero-keys" {
		t.Errorf("tero-keys groups = %q, want only tero-keys", groups)
	}
}

func testKeyOnDiskIsSealed(t *testing.T) {
	if got := must(t, "sudo stat -c '%U %a' /etc/tero/keys/1"); got != "root 600" {
		t.Errorf("key file is %q, want root 600", got)
	}
	// A sealed credential is base64 text with a systemd header, never 32 raw
	// bytes.
	if size := must(t, "sudo stat -c %s /etc/tero/keys/1"); size == "32" {
		t.Error("the key file is 32 bytes, so the key isn't sealed")
	}
}

// sealKeyID prints the first 16 bytes of a credential, the id of what
// systemd-creds sealed it with: the host key, a TPM2, or both.
func sealKeyID(path string) string {
	return "sudo base64 -d " + path + " | head -c 16 | od -An -tx1"
}

func testTPMUsedWhenPresent(t *testing.T) {
	if run(t, "test -e /dev/tpmrm0").exitCode != 0 {
		t.Skip("this server has no TPM")
	}

	if got := run(t, "systemd-creds has-tpm2"); got.exitCode != 0 {
		t.Fatalf("systemd-creds can't use the TPM:\n%s", got.output)
	}
	must(t, "printf x | sudo systemd-creds encrypt --with-key=host --name=probe - /tmp/e2e-host-sealed 2>/dev/null")
	hostOnly := must(t, sealKeyID("/tmp/e2e-host-sealed"))
	if must(t, sealKeyID("/etc/tero/keys/1")) == hostOnly {
		t.Error("the master key is sealed with the host key alone, not tpm2+host")
	}
}

func testTeroCantReadKeys(t *testing.T) {
	for _, path := range []string{"/etc/tero/keys/1", "/var/lib/tero-keys/keys.db"} {
		for _, command := range []string{asTero("cat " + path), inDashboardCgroup("cat " + path)} {
			if got := run(t, command+" >/dev/null"); got.exitCode == 0 {
				t.Errorf("tero can read %s", path)
			}
		}
	}
}

func testOnlyTheDashboardIsAnswered(t *testing.T) {
	assertContains(t, must(t, inDashboardCgroup(keysStatus)), `"isSetUp"`)

	if got := run(t, asTero(keysStatus)); got.exitCode == 0 {
		t.Errorf("a tero process outside tero.service was answered:\n%s", got.output)
	}
}

// key-service: Agent asks for account verbs. The agent is a caller the key
// service knows, but status isn't one of its verbs.
func testAgentKnownButLimited(t *testing.T) {
	got := must(t, asAgent(keysStatus))
	if strings.Contains(got, "isSetUp") || !strings.Contains(got, `"error"`) {
		t.Errorf("the agent asked for status and got:\n%s", got)
	}
}

func testDashboardStaysInItsCgroup(t *testing.T) {
	probe := `for procs in /sys/fs/cgroup/cgroup.procs /sys/fs/cgroup/system.slice/cgroup.procs /sys/fs/cgroup/user.slice/cgroup.procs; do
		{ echo $$ > "$procs"; } 2>/dev/null && echo "moved to $procs"
	done; true`
	if got := must(t, inDashboardCgroup(probe)); got != "" {
		t.Errorf("the dashboard can leave its cgroup: %s", got)
	}
}

func testRotateKey(t *testing.T) {
	assertContains(t, must(t, "sudo tero rotate-key"), "version 2")

	if got := must(t, "sudo ls /etc/tero/keys"); got != "2" {
		t.Errorf("key files after rotation = %q, want only 2", got)
	}
	status := must(t, "sudo "+keysStatus)
	assertContains(t, status, `"keyVersions":[2]`)
	assertContains(t, status, `"isSetUp":true`)
	must(t, "systemctl is-active tero")
}

func testKeyCantBeOpened(t *testing.T) {
	must(t, "sudo sh -c 'echo not-a-credential > /etc/tero/keys/2'")
	run(t, "sudo systemctl restart tero-keys")

	if got := run(t, "systemctl is-active tero-keys"); got.exitCode == 0 {
		t.Fatal("the key service started with a key that doesn't open")
	}
	must(t, "systemctl is-active tero")

	got := must(t, "sudo tero reset-login")
	assertContains(t, got, "/etc/tero/keys-unreadable")
	if !strings.HasPrefix(lastLine(got), "https://") {
		t.Errorf("the last line of reset-login is not a link: %q", lastLine(got))
	}
	must(t, "systemctl is-active tero-keys")
	assertContains(t, must(t, "sudo "+keysStatus), `"isSetUp":false`)
}

// caddyCertificates lists the hash of every certificate in Caddy's storage.
const caddyCertificates = `sudo find /var/lib/tero/caddy/certificates -name '*.crt' -exec sha256sum {} + 2>/dev/null | cut -d' ' -f1`

func testAssumeBreach(t *testing.T) {
	if got := run(t, "echo no | sudo tero assume-breach"); got.exitCode == 0 {
		t.Error("assume-breach ran without confirmation")
	}

	// What an attacker running as tero or tero-agent could leave behind.
	must(t, asTero(`mkdir -p ~/.config/systemd/user && printf '[Service]\nExecStart=/bin/true\n' > ~/.config/systemd/user/planted.service`))
	must(t, asAgent(`mkdir -p ~/.config/systemd/user && printf '[Service]\nExecStart=/bin/true\n' > ~/.config/systemd/user/planted-agent.service`))
	must(t, `echo '* * * * * true' | sudo crontab -u tero -`)
	must(t, `echo '* * * * * true' | sudo crontab -u tero-agent -`)
	must(t, "sudo systemd-run --quiet --uid=tero --unit=tero-e2e-leftover sleep 1000")
	must(t, "sudo systemd-run --quiet --uid=tero-agent --unit=tero-e2e-agent-leftover sleep 1000")

	versionsBefore := must(t, "sudo ls /etc/tero/keys")
	certificatesBefore := must(t, caddyCertificates)
	got := must(t, "sudo tero assume-breach --yes")

	if !strings.HasPrefix(lastLine(got), "https://") {
		t.Errorf("the last line of assume-breach is not a link: %q", lastLine(got))
	}
	if after := must(t, "sudo ls /etc/tero/keys"); after == versionsBefore {
		t.Errorf("the master key was not replaced: still %q", after)
	}
	// Tero gets new certificates as soon as it starts again, so the storage
	// can already be back. None of the old certificates may be in it.
	certificatesAfter := strings.Fields(run(t, caddyCertificates).output)
	for _, certificate := range certificatesAfter {
		if strings.Contains(certificatesBefore, certificate) {
			t.Errorf("Caddy's certificate %s survived assume-breach", certificate)
		}
	}
	must(t, "systemctl is-active tero")
	assertContains(t, must(t, "sudo "+keysStatus), `"isSetUp":false`)

	for _, leftover := range []string{"tero-e2e-leftover", "tero-e2e-agent-leftover"} {
		if left := run(t, "systemctl is-active "+leftover); left.exitCode == 0 {
			t.Errorf("%s survived assume-breach", leftover)
		}
	}
	// The moved ~/.config is reported; tero-agent's user manager and Podman
	// may make a fresh, empty one once they start again.
	moved := map[string]string{
		"/var/lib/tero/.config":               "/var/lib/tero/.config/systemd/user/planted.service",
		"/var/lib/tero-agent/.config":         "/var/lib/tero-agent/.config/systemd/user/planted-agent.service",
		"/var/spool/cron/crontabs/tero":       "/var/spool/cron/crontabs/tero",
		"/var/spool/cron/crontabs/tero-agent": "/var/spool/cron/crontabs/tero-agent",
	}
	for reported, planted := range moved {
		if left := run(t, "sudo test -e "+planted); left.exitCode == 0 {
			t.Errorf("%s was left in place", planted)
		}
		assertContains(t, got, reported)
	}
	for _, unit := range []string{"planted.service", "planted-agent.service"} {
		if found := must(t, "sudo find /var/lib/tero-breach -name "+unit); found == "" {
			t.Errorf("%s wasn't kept in /var/lib/tero-breach", unit)
		}
	}
	waitUntil(t, time.Minute, func() bool { return run(t, agentSystemctl+" is-active tero-agent").exitCode == 0 })
}
