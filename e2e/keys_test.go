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

	if got := must(t, "stat -c '%U:%G %a' /run/tero-keys/keys.sock"); got != "tero-keys:tero 660" {
		t.Errorf("socket is %q, want tero-keys:tero 660", got)
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
		t.Errorf("a tero process in its user session was answered:\n%s", got.output)
	}
	if got := run(t, "sudo -u tero "+keysStatus); got.exitCode == 0 {
		t.Errorf("a tero process outside tero.service was answered:\n%s", got.output)
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

func testAssumeBreach(t *testing.T) {
	if got := run(t, "echo no | sudo tero assume-breach"); got.exitCode == 0 {
		t.Error("assume-breach ran without confirmation")
	}

	// What an attacker running as tero could leave behind.
	must(t, asTero(`mkdir -p ~/.config/systemd/user && printf '[Service]\nExecStart=/bin/true\n' > ~/.config/systemd/user/planted.service`))
	must(t, `echo '* * * * * true' | sudo crontab -u tero -`)
	must(t, "sudo systemd-run --quiet --uid=tero --unit=tero-e2e-leftover sleep 1000")

	versionsBefore := must(t, "sudo ls /etc/tero/keys")
	got := must(t, "sudo tero assume-breach --yes")

	if !strings.HasPrefix(lastLine(got), "https://") {
		t.Errorf("the last line of assume-breach is not a link: %q", lastLine(got))
	}
	if after := must(t, "sudo ls /etc/tero/keys"); after == versionsBefore {
		t.Errorf("the master key was not replaced: still %q", after)
	}
	if got := run(t, "sudo test -e /var/lib/tero/caddy/certificates"); got.exitCode == 0 {
		t.Error("Caddy's storage survived assume-breach")
	}
	must(t, "systemctl is-active tero")
	assertContains(t, must(t, "sudo "+keysStatus), `"isSetUp":false`)

	if left := run(t, "systemctl is-active tero-e2e-leftover"); left.exitCode == 0 {
		t.Error("a process running as tero survived assume-breach")
	}
	for _, path := range []string{"/var/lib/tero/.config/systemd/user", "/var/spool/cron/crontabs/tero"} {
		if left := run(t, "sudo test -e "+path); left.exitCode == 0 {
			t.Errorf("%s was left in place", path)
		}
		assertContains(t, got, path)
	}
	if found := must(t, "sudo find /var/lib/tero-breach -name planted.service"); found == "" {
		t.Error("the planted unit wasn't kept in /var/lib/tero-breach")
	}
	waitUntil(t, time.Minute, func() bool { return run(t, agentSystemctl+" is-active tero-agent").exitCode == 0 })
}
