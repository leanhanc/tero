//go:build e2e

package e2e

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	// publicIP stands in for the VM's public address; the VM sits behind NAT.
	publicIP = "203.0.113.10"
	// skippedDomain is what init picks when the admin gives no domain.
	skippedDomain = publicIP + ".sslip.io"
	// providedDomain is a domain the admin brings. In the VM, Pebble's DNS
	// helper resolves every name to the VM itself.
	providedDomain = "dash.example.com"

	// minimumLynisScore is the hardening index the first green run reached.
	// Raise it when hardening improves; never lower it.
	minimumLynisScore = 64

	capNetBindServiceOnly = "0000000000000400"
)

// TestSlice1 walks one fresh server through init in order: refusals that must
// change nothing, the init itself, every post-init check, a second init, and
// a reboot.
func TestSlice1(t *testing.T) {
	t.Run("tero version runs", func(t *testing.T) {
		must(t, "tero version")
	})

	t.Run("server-init: Init without root", testInitWithoutRoot)
	t.Run("process-isolation: Root command run without sudo", testRootCommandWithoutSudo)
	t.Run("server-init: SSH lockout guard / No SSH key configured", testSSHLockoutGuard)

	t.Run("server-init: Admin skips the domain (runs init)", testInit)
	if t.Failed() {
		t.FailNow()
	}

	t.Run("server-init: Podman available to tero", testPodmanAvailable)
	t.Run("server-init: Subordinate id range sized for rootless BuildKit", testSubIDRange)
	t.Run("server-init: Linger enabled", testLinger)
	t.Run("server-init: tero cannot log in interactively", testNoLogin)
	t.Run("process-isolation: No container process owned by host root", testRootlessContainer)
	t.Run("server-init: SSH locked down", testSSHLockedDown)
	t.Run("server-init: Firewall default deny", testFirewall)
	t.Run("server-init: Automatic security updates", testAutomaticUpdates)
	t.Run("server-init: Kernel settings", testKernelSettings)
	t.Run("server-init: Auditing and AppArmor", testAuditingAndAppArmor)
	t.Run("server-init: Unneeded services and file permissions", testServicesAndPermissions)
	t.Run("server-init: Service running after init", testServiceRunning)
	t.Run("process-isolation: Service process identity", testServiceIdentity)
	t.Run("process-isolation: Built-in proxy binds privileged ports", testProxyPorts)
	t.Run("dashboard-access: Plain HTTP request", testPlainHTTP)
	t.Run("server-init: Admin skips the domain (serves HTTPS)", func(t *testing.T) { assertServesHTTPS(t, skippedDomain) })
	t.Run("server-init: Admin provides a domain", testProvidedDomain)
	t.Run("server-init: Init on an already initialized server", testSecondInit)
	t.Run("server-init: Lynis score in end-to-end tests", testLynis)
	t.Run("server-init: Service survives reboot", testReboot)
}

func testInitWithoutRoot(t *testing.T) {
	got := run(t, "tero init")
	if got.exitCode == 0 {
		t.Fatal("init without root succeeded")
	}
	assertContains(t, got.output, "sudo tero init")
}

func testRootCommandWithoutSudo(t *testing.T) {
	got := run(t, "tero reset-login")
	if got.exitCode == 0 {
		t.Fatal("reset-login without root succeeded")
	}
	assertContains(t, got.output, "sudo tero reset-login")
}

// testSSHLockoutGuard hides the user's SSH key for the duration of a single
// command, since this suite reaches the VM over SSH with that key.
func testSSHLockoutGuard(t *testing.T) {
	before := etcChecksum(t)

	got := run(t, `keys="$HOME/.ssh/authorized_keys"
		mv "$keys" "$keys.hidden"
		sudo env TERO_E2E_PUBLIC_IP=`+publicIP+` tero init; status=$?
		mv "$keys.hidden" "$keys"
		exit $status`)
	if got.exitCode == 0 {
		t.Fatal("init ran without any SSH key for a sudo user")
	}
	assertContains(t, got.output, "no user who can run sudo has an SSH key", "without changing anything")

	if after := etcChecksum(t); after != before {
		t.Fatal("a refused init changed files under /etc")
	}
}

func testInit(t *testing.T) {
	got := run(t, "sudo env TERO_E2E_PUBLIC_IP="+publicIP+" tero init")
	if got.exitCode != 0 {
		t.Fatalf("init failed (%d):\n%s", got.exitCode, got.output)
	}
	t.Logf("init output:\n%s", got.output)

	assertContains(t, got.output, "Done. Open https://"+skippedDomain)
	if strings.Contains(got.output, "may take a few minutes") {
		t.Error("init did not see the HTTPS certificate")
	}
}

func asTero(command string) string {
	return `sudo runuser -u tero -- env HOME=/var/lib/tero XDG_RUNTIME_DIR=/run/user/$(id -u tero) ` + command
}

func testPodmanAvailable(t *testing.T) {
	got := must(t, asTero(`podman info --format '{{.Host.Security.Rootless}}'`))
	if got != "true" {
		t.Fatalf("podman rootless = %q", got)
	}
}

func testSubIDRange(t *testing.T) {
	for _, file := range []string{"/etc/subuid", "/etc/subgid"} {
		contents := must(t, "cat "+file)
		assertOneLargeNonOverlappingRange(t, file, contents)
	}
}

func assertOneLargeNonOverlappingRange(t *testing.T, file, contents string) {
	t.Helper()

	type span struct{ start, end int }
	var teroSpans, otherSpans []span
	for _, line := range strings.Split(contents, "\n") {
		fields := strings.Split(line, ":")
		if len(fields) != 3 {
			continue
		}
		start, _ := strconv.Atoi(fields[1])
		count, _ := strconv.Atoi(fields[2])
		if fields[0] == "tero" {
			if count != 1000000 {
				t.Errorf("%s: tero has %d ids, want 1000000", file, count)
			}
			teroSpans = append(teroSpans, span{start, start + count})
		} else {
			otherSpans = append(otherSpans, span{start, start + count})
		}
	}

	if len(teroSpans) != 1 {
		t.Fatalf("%s: tero has %d ranges, want 1:\n%s", file, len(teroSpans), contents)
	}
	for _, other := range otherSpans {
		isOverlapping := other.start < teroSpans[0].end && teroSpans[0].start < other.end
		if isOverlapping {
			t.Errorf("%s: tero's range overlaps %v", file, other)
		}
	}
}

func testLinger(t *testing.T) {
	assertContains(t, must(t, "loginctl show-user tero --property=Linger"), "Linger=yes")
	must(t, "test -d /run/user/$(id -u tero)")
}

func testNoLogin(t *testing.T) {
	shell := must(t, "getent passwd tero | cut -d: -f7")
	if shell != "/usr/sbin/nologin" {
		t.Errorf("tero's shell is %q", shell)
	}

	passwordState := strings.Fields(must(t, "sudo passwd --status tero"))
	if len(passwordState) < 2 || passwordState[1] != "L" {
		t.Errorf("tero's password is not locked: %v", passwordState)
	}
}

func testRootlessContainer(t *testing.T) {
	must(t, asTero("podman run -d --rm --name e2e-probe quay.io/libpod/alpine:latest sleep 60"))
	defer run(t, asTero("podman rm -f e2e-probe"))

	containerOwners := must(t, asTero("podman top e2e-probe huser"))
	for _, line := range strings.Split(containerOwners, "\n")[1:] {
		if strings.TrimSpace(line) == "0" || strings.TrimSpace(line) == "root" {
			t.Errorf("a container process runs as host root:\n%s", containerOwners)
		}
	}

	helperOwners := must(t, "ps -o user= -C conmon,pasta,rootlessport | sort -u")
	if helperOwners != "tero" {
		t.Errorf("container helpers run as %q, want only tero", helperOwners)
	}
}

func testSSHLockedDown(t *testing.T) {
	effective := must(t, "sudo sshd -T")
	assertContains(t, effective,
		"permitrootlogin no",
		"passwordauthentication no",
		"kbdinteractiveauthentication no",
		"permitemptypasswords no",
		"ciphers chacha20-poly1305@openssh.com,aes256-gcm@openssh.com,aes128-gcm@openssh.com,aes256-ctr,aes192-ctr,aes128-ctr",
		"macs hmac-sha2-512-etm@openssh.com,hmac-sha2-256-etm@openssh.com,umac-128-etm@openssh.com",
		"kexalgorithms ",
	)

	for _, line := range strings.Split(effective, "\n") {
		kex, isKexLine := strings.CutPrefix(line, "kexalgorithms ")
		if isKexLine && !strings.Contains(kex, "curve25519-sha256") {
			t.Errorf("key exchanges are not limited to Tero's list: %s", kex)
		}
		if isKexLine && (strings.Contains(kex, "diffie-hellman") || strings.Contains(kex, "ecdh-sha2")) {
			t.Errorf("weak key exchanges allowed: %s", kex)
		}
	}
}

// testFirewall probes the VM from a separate network namespace joined by a
// veth pair, so the packets arrive on a real interface and go through the
// input chain, like traffic from the internet.
func testFirewall(t *testing.T) {
	assertContains(t, must(t, "sudo nft list table inet tero"), "policy drop", "tcp dport { 22, 80, 443 } accept")

	must(t, `sudo ip netns add e2e-probe
		sudo ip link add e2e-host type veth peer name e2e-probe netns e2e-probe
		sudo ip addr add 10.200.0.1/30 dev e2e-host && sudo ip link set e2e-host up
		sudo ip -n e2e-probe addr add 10.200.0.2/30 dev e2e-probe
		sudo ip -n e2e-probe link set e2e-probe up && sudo ip -n e2e-probe link set lo up`)
	defer run(t, "sudo ip link del e2e-host; sudo ip netns del e2e-probe")

	must(t, "sudo systemd-run --unit=e2e-listener --collect nc -lk 8080")
	defer run(t, "sudo systemctl stop e2e-listener")

	for port, isOpen := range map[int]bool{22: true, 443: true, 8080: false} {
		probe := run(t, "sudo ip netns exec e2e-probe nc -z -w 3 10.200.0.1 "+strconv.Itoa(port))
		isReachable := probe.exitCode == 0
		if isReachable != isOpen {
			t.Errorf("port %d reachable from outside = %v, want %v", port, isReachable, isOpen)
		}
	}
}

func testAutomaticUpdates(t *testing.T) {
	assertContains(t, must(t, "apt-config dump APT::Periodic"),
		`APT::Periodic::Update-Package-Lists "1";`,
		`APT::Periodic::Unattended-Upgrade "1";`)
	must(t, "systemctl is-enabled apt-daily-upgrade.timer")
}

var kernelSettings = map[string]string{
	"kernel.kptr_restrict":                  "2",
	"kernel.dmesg_restrict":                 "1",
	"net.ipv4.conf.all.rp_filter":           "1",
	"net.ipv4.conf.all.accept_redirects":    "0",
	"net.ipv6.conf.all.accept_redirects":    "0",
	"net.ipv4.conf.all.send_redirects":      "0",
	"net.ipv4.conf.all.accept_source_route": "0",
	"net.ipv4.tcp_syncookies":               "1",
	"net.ipv4.icmp_echo_ignore_broadcasts":  "1",
	"fs.suid_dumpable":                      "0",
}

func testKernelSettings(t *testing.T) {
	for key, want := range kernelSettings {
		if got := must(t, "sysctl -n "+key); got != want {
			t.Errorf("%s = %s, want %s", key, got, want)
		}
	}

	coreLimit := run(t, `grep -hx '\* hard core 0' /etc/security/limits.conf /etc/security/limits.d/*.conf`)
	if coreLimit.exitCode != 0 {
		t.Error("core dumps are not disabled with a hard limit in /etc/security/limits.d")
	}
}

func testAuditingAndAppArmor(t *testing.T) {
	must(t, "systemctl is-active auditd")
	assertContains(t, must(t, "sudo auditctl -l"), "-w /etc/tero -p wa -k tero", "-w /etc/sudoers -p wa -k scope")
	if got := must(t, "aa-enabled"); got != "Yes" {
		t.Errorf("aa-enabled = %q", got)
	}
}

func testServicesAndPermissions(t *testing.T) {
	enabledUnneeded := run(t, `for unit in apport.service avahi-daemon.service cups.service rpcbind.service rpcbind.socket; do
		systemctl is-enabled "$unit" 2>/dev/null | grep -qx enabled && echo "$unit"; done; true`)
	if strings.TrimSpace(enabledUnneeded.output) != "" {
		t.Errorf("unneeded services still enabled: %s", enabledUnneeded.output)
	}

	permissions := map[string]string{
		"/etc/shadow":           "640 root shadow",
		"/etc/gshadow":          "640 root shadow",
		"/etc/passwd":           "644 root root",
		"/etc/ssh/sshd_config":  "600 root root",
		"/etc/crontab":          "600 root root",
		"/etc/cron.d":           "700 root root",
		"/etc/tero/config.json": "644 root root",
	}
	for path, want := range permissions {
		if got := must(t, "sudo stat -c '%a %U %G' "+path); got != want {
			t.Errorf("%s is %q, want %q", path, got, want)
		}
	}
}

func testServiceRunning(t *testing.T) {
	must(t, "systemctl is-active tero")
	must(t, "systemctl is-enabled tero")
}

func testServiceIdentity(t *testing.T) {
	status := must(t, "sudo cat /proc/$(systemctl show tero --property=MainPID --value)/status")
	teroUID := must(t, "id -u tero")

	fields := map[string]string{}
	for _, line := range strings.Split(status, "\n") {
		key, value, _ := strings.Cut(line, ":")
		fields[key] = strings.TrimSpace(value)
	}

	if uids := strings.Fields(fields["Uid"]); len(uids) == 0 || uids[0] != teroUID {
		t.Errorf("service uid = %q, want %s", fields["Uid"], teroUID)
	}
	for _, capabilitySet := range []string{"CapEff", "CapPrm", "CapBnd"} {
		if fields[capabilitySet] != capNetBindServiceOnly {
			t.Errorf("%s = %s, want %s (CAP_NET_BIND_SERVICE only)", capabilitySet, fields[capabilitySet], capNetBindServiceOnly)
		}
	}
}

func testProxyPorts(t *testing.T) {
	listeners := must(t, `sudo ss -Hltnp '( sport = :80 or sport = :443 )'`)
	assertContains(t, listeners, `"tero"`)

	listenerUsers := must(t, `sudo ss -Hltnpe '( sport = :80 or sport = :443 )' | grep -o 'uid:[0-9]*' | sort -u`)
	if listenerUsers != "uid:"+must(t, "id -u tero") {
		t.Errorf("ports 80/443 are held by %q, want only tero", listenerUsers)
	}
}

func testPlainHTTP(t *testing.T) {
	got := must(t, fmt.Sprintf(`curl -sS -D - -o /dev/null --resolve %s:80:127.0.0.1 http://%s/`, skippedDomain, skippedDomain))
	assertContains(t, got, "Location: https://"+skippedDomain+"/")
	if !strings.HasPrefix(got, "HTTP/1.1 30") {
		t.Errorf("plain HTTP is not redirected:\n%s", got)
	}
	if strings.Contains(strings.ToLower(got), "set-cookie") {
		t.Errorf("a cookie was set over plain HTTP:\n%s", got)
	}
}

// assertServesHTTPS fetches the dashboard over HTTPS, trusting only the test
// CA's issuing root.
func assertServesHTTPS(t *testing.T, domain string) {
	t.Helper()

	must(t, "curl -sSk https://127.0.0.1:15000/roots/0 -o /tmp/pebble-root.pem")
	got := must(t, fmt.Sprintf(`curl -sS -D - --cacert /tmp/pebble-root.pem --resolve %s:443:127.0.0.1 https://%s/`, domain, domain))
	assertContains(t, got, "Tero is running", "strict-transport-security: max-age=31536000")
}

func testProvidedDomain(t *testing.T) {
	setDomain := func(domain string) {
		must(t, fmt.Sprintf(`echo '{"dashboardDomain": "%s"}' | sudo tee /etc/tero/config.json >/dev/null && sudo systemctl restart tero`, domain))
	}
	defer setDomain(skippedDomain)

	setDomain(providedDomain)
	waitUntil(t, 2*time.Minute, func() bool {
		return run(t, "curl -sSk --resolve "+providedDomain+":443:127.0.0.1 https://"+providedDomain+"/").exitCode == 0
	})
	assertServesHTTPS(t, providedDomain)
}

func testSecondInit(t *testing.T) {
	before := etcChecksum(t)

	got := run(t, "sudo env TERO_E2E_PUBLIC_IP="+publicIP+" tero init")
	if got.exitCode == 0 {
		t.Fatal("a second init succeeded")
	}
	assertContains(t, got.output, "already a Tero host", "https://"+skippedDomain, "sudo tero reset-login")

	if after := etcChecksum(t); after != before {
		t.Fatal("a refused second init changed files under /etc")
	}
}

func testLynis(t *testing.T) {
	must(t, "sudo DEBIAN_FRONTEND=noninteractive apt-get install -y lynis >/dev/null")
	must(t, "sudo lynis audit system --quick --no-colors --cronjob >/dev/null")

	reportLine := must(t, "sudo grep '^hardening_index=' /var/log/lynis-report.dat")
	score, err := strconv.Atoi(strings.TrimPrefix(reportLine, "hardening_index="))
	if err != nil {
		t.Fatalf("unexpected report line %q", reportLine)
	}

	t.Logf("Lynis hardening index: %d (minimum %d)", score, minimumLynisScore)
	if score < minimumLynisScore {
		t.Errorf("Lynis hardening index %d is below the minimum %d", score, minimumLynisScore)
	}
}

func testReboot(t *testing.T) {
	for _, args := range [][]string{{"stop", vmName()}, {"start", "--tty=false", vmName()}} {
		if output, err := exec.Command("limactl", args...).CombinedOutput(); err != nil {
			t.Fatalf("limactl %v: %v\n%s", args, err, output)
		}
	}

	waitUntil(t, 2*time.Minute, func() bool { return run(t, "systemctl is-active tero").exitCode == 0 })
	testServiceIdentity(t)
	testKernelSettings(t)
	assertContains(t, must(t, "sudo nft list table inet tero"), "policy drop")
	// Pebble creates a new root on every start, so after a reboot the stored
	// certificate no longer chains to Pebble's current root. Checking that the
	// page is served with that certificate is enough here.
	waitUntil(t, time.Minute, func() bool {
		page := run(t, "curl -sSk --resolve "+skippedDomain+":443:127.0.0.1 https://"+skippedDomain+"/")
		return page.exitCode == 0 && strings.Contains(page.output, "Tero is running")
	})
}

func waitUntil(t *testing.T, timeout time.Duration, isReady func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isReady() {
			return
		}
		time.Sleep(2 * time.Second)
	}

	t.Fatalf("not ready after %s", timeout)
}
