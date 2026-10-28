package initflow

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/leanhanc/tero/internal/config"
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

// testKey is a real public key line, since the guard parses keys.
func testKey(t *testing.T, comment string) string {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshKey, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshKey))) + " " + comment + "\n"
}

const defaultSSHSettings = "port 22\npubkeyauthentication yes\nstrictmodes yes\nauthorizedkeysfile .ssh/authorized_keys .ssh/authorized_keys2\npubkeyacceptedalgorithms ssh-ed25519,rsa-sha2-512,rsa-sha2-256\n"

// newGuardHost has ana in the sudo group and bo without sudo, both owned by
// the test's own uid so StrictModes checks pass on the temp files.
func newGuardHost(t *testing.T) host {
	t.Helper()
	files := hostfs.FS{Root: t.TempDir()}
	uid := os.Getuid()
	writeHostFile(t, files, "/usr/sbin/sshd", "")
	writeHostFile(t, files, "/etc/passwd", fmt.Sprintf("root:x:0:0:root:/root:/bin/bash\nana:x:%d:1000::/home/ana:/bin/bash\nbo:x:%d:1001::/home/bo:/bin/bash\n", uid, uid))
	writeHostFile(t, files, "/etc/group", "root:x:0:\nsudo:x:27:ana\nana:x:1000:\nbo:x:1001:\n")

	recorder := &sysexec.Recorder{Outputs: map[string]string{
		"sshd -T": defaultSSHSettings,
		"sshd -T -C user=ana,host=localhost,addr=127.0.0.1": defaultSSHSettings,
		"sshd -T -C user=bo,host=localhost,addr=127.0.0.1":  defaultSSHSettings,
	}}

	return host{run: recorder, files: files}
}

func setSSHSettings(h host, user, settings string) {
	h.run.(*sysexec.Recorder).Outputs["sshd -T -C user="+user+",host=localhost,addr=127.0.0.1"] = settings
}

func TestSSHLockoutRefusesWithoutKeys(t *testing.T) {
	h := newGuardHost(t)
	t.Setenv("SUDO_USER", "ana")
	writeHostFile(t, h.files, "/home/ana/.ssh/authorized_keys", "# no keys yet\n")

	err := checkSSHLockout(context.Background(), h)
	if err == nil || !strings.Contains(err.Error(), "wouldn't accept a key for ana") {
		t.Fatalf("checkSSHLockout() = %v", err)
	}
}

func TestSSHLockoutPassesWithTheAdminsKey(t *testing.T) {
	h := newGuardHost(t)
	t.Setenv("SUDO_USER", "ana")
	writeHostFile(t, h.files, "/home/ana/.ssh/authorized_keys2", testKey(t, "ana@laptop"))

	if err := checkSSHLockout(context.Background(), h); err != nil {
		t.Fatalf("checkSSHLockout() = %v", err)
	}
}

func TestSSHLockoutAcceptsRSAKeysThroughSHA2Algorithms(t *testing.T) {
	if !isAcceptedKeyType("ssh-rsa", []string{"ssh-ed25519", "rsa-sha2-512"}) {
		t.Error("an ssh-rsa key is refused although rsa-sha2-512 is accepted")
	}
	if isAcceptedKeyType("ssh-dss", []string{"ssh-ed25519", "rsa-sha2-512"}) {
		t.Error("a DSA key is accepted")
	}
}

// Review finding: a root login with someone else's key on the server passed.
func TestSSHLockoutRefusesRootLogin(t *testing.T) {
	h := newGuardHost(t)
	t.Setenv("SUDO_USER", "")
	writeHostFile(t, h.files, "/home/ana/.ssh/authorized_keys", testKey(t, "ana@laptop"))

	err := checkSSHLockout(context.Background(), h)
	if err == nil || !strings.Contains(err.Error(), "logged in as root") {
		t.Fatalf("checkSSHLockout() as root = %v", err)
	}
}

// Review finding: the admin's missing key was covered by another sudo user's.
func TestSSHLockoutIgnoresOtherUsersKeys(t *testing.T) {
	h := newGuardHost(t)
	t.Setenv("SUDO_USER", "bo")
	writeHostFile(t, h.files, "/home/ana/.ssh/authorized_keys", testKey(t, "ana@laptop"))

	if err := checkSSHLockout(context.Background(), h); err == nil {
		t.Fatal("another user's key satisfied the guard for bo")
	}
}

// Review finding: any non-comment line counted as a key.
func TestSSHLockoutRefusesGarbageAndForcedCommandKeys(t *testing.T) {
	forcedCommand := `command="/usr/bin/backup" ` + testKey(t, "backup")
	for name, contents := range map[string]string{"garbage": "this is not a key\n", "forced command": forcedCommand} {
		t.Run(name, func(t *testing.T) {
			h := newGuardHost(t)
			t.Setenv("SUDO_USER", "ana")
			writeHostFile(t, h.files, "/home/ana/.ssh/authorized_keys", contents)

			if err := checkSSHLockout(context.Background(), h); err == nil {
				t.Fatal("the guard passed")
			}
		})
	}
}

// Review finding: sshd ignores keys files others can write (StrictModes).
func TestSSHLockoutAppliesStrictModes(t *testing.T) {
	h := newGuardHost(t)
	t.Setenv("SUDO_USER", "ana")
	writeHostFile(t, h.files, "/home/ana/.ssh/authorized_keys", testKey(t, "ana@laptop"))
	os.Chmod(h.files.Path("/home/ana"), 0o775)

	err := checkSSHLockout(context.Background(), h)
	if err == nil || !strings.Contains(err.Error(), "StrictModes") {
		t.Fatalf("checkSSHLockout() with a group-writable home = %v", err)
	}

	setSSHSettings(h, "ana", strings.Replace(defaultSSHSettings, "strictmodes yes", "strictmodes no", 1))
	if err := checkSSHLockout(context.Background(), h); err != nil {
		t.Errorf("checkSSHLockout() with StrictModes off = %v", err)
	}
}

// Review finding: AllowUsers and friends weren't considered.
func TestSSHLockoutAppliesAllowAndDenyLists(t *testing.T) {
	cases := map[string]string{
		"allowusers without ana": "allowusers bo\n",
		"denyusers ana":          "denyusers an*\n",
		"denygroups sudo":        "denygroups sudo\n",
		"allowgroups other":      "allowgroups ssh-users\n",
		"pubkey off":             "pubkeyauthentication no\n",
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			h := newGuardHost(t)
			t.Setenv("SUDO_USER", "ana")
			writeHostFile(t, h.files, "/home/ana/.ssh/authorized_keys", testKey(t, "ana@laptop"))
			setSSHSettings(h, "ana", extra+defaultSSHSettings)

			if err := checkSSHLockout(context.Background(), h); err == nil {
				t.Fatal("the guard passed")
			}
		})
	}
}

// Review finding: an existing tero account was taken over.
func TestServiceAccountCheckRefusesForeignAccounts(t *testing.T) {
	cases := map[string]struct {
		service    serviceAccount
		passwdLine string
	}{
		"regular tero user":         {teroAccount, "tero:x:1005:1005::/home/tero:/bin/bash\n"},
		"tero in sudo":              {teroAccount, "tero:x:998:998::/var/lib/tero:/usr/sbin/nologin\n"},
		"regular tero-agent user":   {agentAccount, "tero-agent:x:1007:1007::/home/tero-agent:/bin/bash\n"},
		"tero-agent in sudo":        {agentAccount, "tero-agent:x:996:996::/var/lib/tero-agent:/usr/sbin/nologin\n"},
		"tero-agent with tero home": {agentAccount, "tero-agent:x:995:995::/var/lib/tero:/usr/sbin/nologin\n"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			h := newGuardHost(t)
			writeHostFile(t, h.files, "/etc/passwd", "root:x:0:0::/root:/bin/bash\n"+test.passwdLine)
			writeHostFile(t, h.files, "/etc/group", "sudo:x:27:tero,tero-agent\ntero:x:998:\ntero-agent:x:996:\n")

			if err := checkServiceAccount(h, test.service); err == nil {
				t.Fatalf("init would take over an existing %s account", test.service.name)
			}
		})
	}
}

func TestServiceAccountCheckAcceptsOnesInitCreated(t *testing.T) {
	h := newGuardHost(t)
	writeHostFile(t, h.files, "/etc/passwd", "tero:x:998:998::/var/lib/tero:/usr/sbin/nologin\ntero-agent:x:996:996::/var/lib/tero-agent:/usr/sbin/nologin\n")
	writeHostFile(t, h.files, "/etc/group", "sudo:x:27:ana\ntero:x:998:\ntero-agent:x:996:\n")

	for _, service := range []serviceAccount{teroAccount, agentAccount} {
		if err := checkServiceAccount(h, service); err != nil {
			t.Fatalf("checkServiceAccount(%s) = %v", service.name, err)
		}
	}
}

func TestSharedGroupsCheckRefusesOtherMembers(t *testing.T) {
	cases := map[string]string{
		"foreign member":   "tero-shared:x:994:tero,ana\n",
		"regular group":    "tero-keys-callers:x:1010:\n",
		"only an outsider": "tero-keys-callers:x:993:bo\n",
	}
	for name, groupLine := range cases {
		t.Run(name, func(t *testing.T) {
			h := newGuardHost(t)
			writeHostFile(t, h.files, "/etc/group", "sudo:x:27:ana\n"+groupLine)

			if err := checkSharedGroups(h); err == nil {
				t.Fatal("init would share Tero's database or key socket with another user")
			}
		})
	}
}

func TestSharedGroupsCheckAcceptsOnesInitCreated(t *testing.T) {
	h := newGuardHost(t)
	writeHostFile(t, h.files, "/etc/group", "tero-shared:x:994:tero,tero-agent\ntero-keys-callers:x:993:tero,tero-agent\n")

	if err := checkSharedGroups(h); err != nil {
		t.Fatalf("checkSharedGroups() = %v", err)
	}
}

func TestEnsureAccountReusesLeftoverGroup(t *testing.T) {
	h := newGuardHost(t)
	writeHostFile(t, h.files, "/etc/group", "tero:x:998:\n")

	if err := ensureAccount(context.Background(), h, teroAccount); err != nil {
		t.Fatal(err)
	}
	commands := strings.Join(h.run.(*sysexec.Recorder).Commands, "\n")
	if !strings.Contains(commands, "useradd --system --gid tero ") {
		t.Errorf("useradd doesn't reuse the existing group:\n%s", commands)
	}
}

// server-init: tero has no containers
func TestTeroUserGetsNoContainers(t *testing.T) {
	h := newGuardHost(t)

	if err := createTeroUser(context.Background(), h); err != nil {
		t.Fatal(err)
	}
	commands := strings.Join(h.run.(*sysexec.Recorder).Commands, "\n")
	for _, forbidden := range []string{"--add-subuids", "enable-linger", "podman"} {
		if strings.Contains(commands, forbidden) {
			t.Errorf("creating tero ran %q:\n%s", forbidden, commands)
		}
	}
}

// server-init: State shared by the service and the agent
func TestShareStateCreatesGroupsAndSetgidDirectory(t *testing.T) {
	h := newGuardHost(t)

	if err := shareState(context.Background(), h); err != nil {
		t.Fatal(err)
	}
	commands := strings.Join(h.run.(*sysexec.Recorder).Commands, "\n")
	for _, want := range []string{
		"groupadd --system tero-shared",
		"groupadd --system tero-keys-callers",
		"usermod --append --groups tero-shared,tero-keys-callers tero",
		"usermod --append --groups tero-shared,tero-keys-callers tero-agent",
		"install -d -o root -g tero-shared -m 2770 /var/lib/tero-shared",
		"install -d -o tero-agent -g tero-shared -m 2750 /var/lib/tero-shared/logs /var/lib/tero-shared/logs/deploys",
	} {
		if !strings.Contains(commands, want) {
			t.Errorf("missing %q in:\n%s", want, commands)
		}
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
		"Dash.Example.com":    "dash.example.com",
		"dash.example.com.":   "dash.example.com",
		"203.0.113.10.nip.io": "203.0.113.10.nip.io",
	}
	for input, want := range valid {
		got, err := normalizeDomain(input)
		if err != nil || got != want {
			t.Errorf("normalizeDomain(%q) = %q, %v", input, got, err)
		}
	}

	for _, input := range []string{"localhost", "203.0.113.10", "1.2.3", "dash.example.123", "-bad.example.com", "dash_board.example.com", "https://dash.example.com"} {
		if _, err := normalizeDomain(input); err == nil {
			t.Errorf("normalizeDomain(%q) should fail", input)
		}
	}
}

func TestResolveDomainUsesTheFlag(t *testing.T) {
	opts := Options{Domain: "Dash.Example.com"}

	got, err := resolveDomain(opts, netip.MustParseAddr("203.0.113.10"))
	if err != nil || got != "dash.example.com" {
		t.Fatalf("resolveDomain() = %q, %v", got, err)
	}
}

func TestResolveDomainPromptFallsBackOnEmptyAnswer(t *testing.T) {
	var out strings.Builder
	opts := Options{IsInteractive: true, In: strings.NewReader("\n"), Out: &out}

	got, err := resolveDomain(opts, netip.MustParseAddr("203.0.113.10"))
	if err != nil || got != "203.0.113.10.nip.io" {
		t.Fatalf("resolveDomain() = %q, %v", got, err)
	}
	if !strings.Contains(out.String(), "press Enter to use 203.0.113.10.nip.io") {
		t.Fatalf("prompt = %q", out.String())
	}
}

func TestResolveDomainPromptUsesTheAnswer(t *testing.T) {
	opts := Options{IsInteractive: true, In: strings.NewReader("Dash.Example.com\n"), Out: &strings.Builder{}}

	got, err := resolveDomain(opts, netip.MustParseAddr("203.0.113.10"))
	if err != nil || got != "dash.example.com" {
		t.Fatalf("resolveDomain() = %q, %v", got, err)
	}
}

func TestHardeningDropInKeepOnlySupportedKex(t *testing.T) {
	recorder := &sysexec.Recorder{Outputs: map[string]string{
		"ssh -Q kex": "diffie-hellman-group14-sha256\ncurve25519-sha256\ncurve25519-sha256@libssh.org\nsntrup761x25519-sha512@openssh.com\n",
	}}

	settings, err := hardeningDropIn(context.Background(), host{run: recorder})
	if err != nil {
		t.Fatal(err)
	}

	want := "\nKexAlgorithms sntrup761x25519-sha512@openssh.com,curve25519-sha256,curve25519-sha256@libssh.org\n"
	if !strings.HasSuffix(string(settings), want) {
		t.Fatalf("settings end with:\n%s", settings[len(settings)-120:])
	}
}

func TestHardeningDropInRefuseWithoutAllowedKex(t *testing.T) {
	recorder := &sysexec.Recorder{Outputs: map[string]string{"ssh -Q kex": "diffie-hellman-group1-sha1\n"}}

	if _, err := hardeningDropIn(context.Background(), host{run: recorder}); err == nil {
		t.Fatal("sshSettings() accepted a host with only weak key exchanges")
	}
}

// Review finding: the firewall only ever opened port 22.
func TestFirewallOpensEverySSHPort(t *testing.T) {
	h := newGuardHost(t)
	recorder := h.run.(*sysexec.Recorder)
	recorder.Outputs["sshd -T"] = "port 2222\nlistenaddress 0.0.0.0:2222\n"
	recorder.Outputs["systemctl show ssh.socket --property=Listen"] = "Listen=0.0.0.0:2200 (Stream)\nListen=[::]:2200 (Stream)\n"

	if err := enableFirewall(context.Background(), h); err != nil {
		t.Fatal(err)
	}

	rules, _ := h.files.ReadFile("/etc/tero/firewall.nft")
	if !strings.Contains(string(rules), "tcp dport { 2200, 2222, 80, 443 } accept") {
		t.Errorf("rules don't open the SSH ports:\n%s", rules)
	}
}

func TestFirewallRulesForDefaultPort(t *testing.T) {
	rules, err := firewallRules([]int{22})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rules), "tcp dport { 22, 80, 443 } accept") || strings.Contains(string(rules), "@") {
		t.Errorf("rules:\n%s", rules)
	}
}

// Review finding: a re-run skipped sshd -t and the reload when the drop-in
// was already there.
func TestHardenSSHValidatesAndReloadsOnEveryRun(t *testing.T) {
	h := newGuardHost(t)
	recorder := h.run.(*sysexec.Recorder)
	recorder.Outputs["ssh -Q kex"] = "curve25519-sha256\n"
	recorder.Outputs["sshd -T"] = "permitrootlogin no\npasswordauthentication no\nkbdinteractiveauthentication no\npermitemptypasswords no\n"

	for run := range 2 {
		recorder.Commands = nil
		if err := hardenSSH(context.Background(), h); err != nil {
			t.Fatalf("run %d: %v", run+1, err)
		}
		commands := strings.Join(recorder.Commands, "\n")
		if !strings.Contains(commands, "sshd -t") || !strings.Contains(commands, "systemctl try-reload-or-restart ssh.service") {
			t.Errorf("run %d didn't validate and reload:\n%s", run+1, commands)
		}
	}
}

// Review finding: settings above the Include or in Match blocks could
// silently override the drop-in.
func TestHardenSSHRefusesOverriddenSettings(t *testing.T) {
	h := newGuardHost(t)
	t.Setenv("SUDO_USER", "ana")
	recorder := h.run.(*sysexec.Recorder)
	recorder.Outputs["ssh -Q kex"] = "curve25519-sha256\n"
	recorder.Outputs["sshd -T"] = "permitrootlogin no\npasswordauthentication no\nkbdinteractiveauthentication no\npermitemptypasswords no\n"
	setSSHSettings(h, "ana", "permitrootlogin no\npasswordauthentication yes\nkbdinteractiveauthentication no\npermitemptypasswords no\n")

	err := hardenSSH(context.Background(), h)
	if err == nil || !strings.Contains(err.Error(), "passwordauthentication") {
		t.Fatalf("hardenSSH() with a Match block turning passwords back on = %v", err)
	}
	if h.files.Exists("/etc/ssh/sshd_config.d/00-tero-hardening.conf") {
		t.Error("the drop-in was left in place")
	}
	if strings.Contains(strings.Join(recorder.Commands, "\n"), "try-reload-or-restart") {
		t.Error("sshd was reloaded")
	}
}

// Review finding: a later sysctl.d file could override Tero's values.
func TestHardenKernelChecksEffectiveValues(t *testing.T) {
	h := newGuardHost(t)
	recorder := h.run.(*sysexec.Recorder)
	settings, _ := embedded.ReadFile("files/sysctl-hardening.conf")
	for _, line := range strings.Split(string(settings), "\n") {
		key, value, isSetting := strings.Cut(line, "=")
		if isSetting && !strings.HasPrefix(line, "#") {
			recorder.Outputs["sysctl -n "+strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}

	if err := hardenKernel(context.Background(), h); err != nil {
		t.Fatalf("hardenKernel() = %v", err)
	}
	if !h.files.Exists("/etc/sysctl.d/99-zz-tero-hardening.conf") {
		t.Error("the sysctl file doesn't sort last")
	}

	recorder.Outputs["sysctl -n net.ipv4.ip_forward"] = "1"
	if err := hardenKernel(context.Background(), h); err == nil || !strings.Contains(err.Error(), "net.ipv4.ip_forward") {
		t.Errorf("hardenKernel() with an overridden value = %v", err)
	}
}

// Ubuntu 26.04 ships limits.d/10-coredump-debian.conf with "root hard core
// infinity", and pam_limits never applies "*" to root, so root kept core dumps.
func TestCoreLimitCoversRoot(t *testing.T) {
	limits, _ := embedded.ReadFile("files/limits-core.conf")
	for _, want := range []string{"* - core 0", "root - core 0"} {
		if !strings.Contains(string(limits), "\n"+want+"\n") {
			t.Errorf("limits-core.conf is missing %q", want)
		}
	}
}

// Review finding: interrupting init killed dpkg mid-install, and dpkg config
// file prompts failed without a terminal.
func TestInstallPackagesSurvivesInterruptsAndKeepsConfigFiles(t *testing.T) {
	h := newGuardHost(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	recording := &contextRecorder{}
	h.run = recording
	if err := installPackages(ctx, h); err != nil {
		t.Fatal(err)
	}

	if recording.cancelledCalls > 0 {
		t.Errorf("%d apt commands ran with a cancelled context", recording.cancelledCalls)
	}
	commands := strings.Join(recording.commands, "\n")
	for _, want := range []string{"dpkg --configure -a", "--force-confold", "--force-confdef"} {
		if !strings.Contains(commands, want) {
			t.Errorf("missing %q in:\n%s", want, commands)
		}
	}
}

type contextRecorder struct {
	commands       []string
	cancelledCalls int
}

func (r *contextRecorder) Run(ctx context.Context, name string, args ...string) (string, error) {
	r.commands = append(r.commands, strings.Join(append([]string{name}, args...), " "))
	if ctx.Err() != nil {
		r.cancelledCalls++
	}
	return "", nil
}

// Review findings: stopping the firewall unit opened every port, the service
// started without the firewall, and the service had no memory bound.
func TestUnitsKeepTheFirewallAndBoundMemory(t *testing.T) {
	firewallUnit, _ := embedded.ReadFile("files/tero-firewall.service")
	serviceUnit, _ := embedded.ReadFile("files/tero.service")

	if strings.Contains(string(firewallUnit), "\nExecStop=") {
		t.Error("stopping tero-firewall.service deletes the rules")
	}
	for _, want := range []string{"Requires=tero-firewall.service", "MemoryMax="} {
		if !strings.Contains(string(serviceUnit), want) {
			t.Errorf("tero.service is missing %s", want)
		}
	}
}

// server-init: Key service running after init. The dashboard only wants the
// key service, so restarting or failing it never stops the dashboard and
// apps (key-service: Restart while the dashboard waits).
func TestDashboardOnlyWantsTheKeyService(t *testing.T) {
	serviceUnit, _ := embedded.ReadFile("files/tero.service")
	unit := string(serviceUnit)

	if strings.Contains(unit, "Requires=tero-keys") || strings.Contains(unit, "BindsTo=tero-keys") || strings.Contains(unit, "PartOf=tero-keys") {
		t.Error("tero.service is stopped or restarted with the key service")
	}
	for _, want := range []string{"Wants=network-online.target tero-keys.socket", "After=network-online.target tero-firewall.service tero-keys.socket", "LogRateLimitBurst="} {
		if !strings.Contains(unit, want) {
			t.Errorf("tero.service is missing %q", want)
		}
	}
}

// key-service: Process identity and sandbox, No network, Socket permissions
func TestKeyServiceUnitsAreLockedDown(t *testing.T) {
	serviceUnit, _ := embedded.ReadFile("files/tero-keys.service")
	socketUnit, _ := embedded.ReadFile("files/tero-keys.socket")

	for _, want := range []string{
		"User=tero-keys", "ExecStart=/usr/local/bin/tero keys", "LoadCredentialEncrypted=kek:/etc/tero/keys/",
		"CapabilityBoundingSet=\n", "NoNewPrivileges=yes", "PrivateNetwork=yes", "RestrictAddressFamilies=AF_UNIX\n",
		"ProtectSystem=strict", "ProtectHome=yes", "MemoryDenyWriteExecute=yes", "SystemCallFilter=@system-service",
		"MemoryMax=", "LimitCORE=0",
	} {
		if !strings.Contains(string(serviceUnit), want) {
			t.Errorf("tero-keys.service is missing %q", want)
		}
	}
	for _, forbidden := range []string{"\nProtectProc=", "\nPrivateUsers="} {
		if strings.Contains(string(serviceUnit), forbidden) {
			t.Errorf("tero-keys.service sets %q, which hides its callers", forbidden)
		}
	}
	for _, want := range []string{"ListenStream=/run/tero-keys/keys.sock", "SocketUser=tero-keys", "SocketGroup=tero-keys-callers\n", "SocketMode=0660"} {
		if !strings.Contains(string(socketUnit), want) {
			t.Errorf("tero-keys.socket is missing %q", want)
		}
	}
}

// server-init: tero-keys can't log in
func TestKeysAccountCheckRefusesForeignAccounts(t *testing.T) {
	cases := map[string]string{
		"regular user":      "tero-keys:x:1006:1006::/home/tero-keys:/bin/bash\n",
		"in the tero group": "tero-keys:x:997:997::/var/lib/tero-keys:/usr/sbin/nologin\n",
	}
	for name, passwdLine := range cases {
		t.Run(name, func(t *testing.T) {
			h := newGuardHost(t)
			writeHostFile(t, h.files, "/etc/passwd", "root:x:0:0::/root:/bin/bash\n"+passwdLine)
			writeHostFile(t, h.files, "/etc/group", "tero:x:998:tero-keys\ntero-keys:x:997:\n")

			if err := checkKeysAccount(h); err == nil {
				t.Fatal("init would take over an existing tero-keys account")
			}
		})
	}
}

func TestKeysAccountCheckRefusesSharedGroups(t *testing.T) {
	for _, groupName := range []string{"tero-agent", "tero-shared", "tero-keys-callers"} {
		t.Run(groupName, func(t *testing.T) {
			h := newGuardHost(t)
			writeHostFile(t, h.files, "/etc/passwd", "tero-keys:x:997:997::/var/lib/tero-keys:/usr/sbin/nologin\n")
			writeHostFile(t, h.files, "/etc/group", groupName+":x:990:tero-keys\ntero-keys:x:997:\n")

			if err := checkKeysAccount(h); err == nil {
				t.Fatalf("the key service's user would share %s", groupName)
			}
		})
	}
}

func TestKeysAccountCheckAcceptsOneInitCreated(t *testing.T) {
	h := newGuardHost(t)
	writeHostFile(t, h.files, "/etc/passwd", "tero-keys:x:997:997::/var/lib/tero-keys:/usr/sbin/nologin\n")
	writeHostFile(t, h.files, "/etc/group", "tero:x:998:\ntero-keys:x:997:\n")

	if err := checkKeysAccount(h); err != nil {
		t.Fatalf("checkKeysAccount() = %v", err)
	}
}

func TestCreateKeysUserHasNoShellOrPassword(t *testing.T) {
	h := newGuardHost(t)

	if err := createKeysUser(context.Background(), h); err != nil {
		t.Fatal(err)
	}
	commands := strings.Join(h.run.(*sysexec.Recorder).Commands, "\n")
	for _, want := range []string{
		"useradd --system --user-group --no-create-home --home-dir /var/lib/tero-keys --shell /usr/sbin/nologin tero-keys",
		"usermod --lock tero-keys",
	} {
		if !strings.Contains(commands, want) {
			t.Errorf("missing %q in:\n%s", want, commands)
		}
	}
}

func TestKeyServiceStartsBeforeTheDashboard(t *testing.T) {
	var titles []string
	for _, s := range steps(config.Config{}) {
		titles = append(titles, s.title)
	}

	keysAt := slices.Index(titles, "starting the key service")
	sealAt := slices.Index(titles, "sealing the master key")
	dashboardAt := slices.Index(titles, "starting the Tero service")
	if sealAt < 0 || keysAt < sealAt || dashboardAt < keysAt {
		t.Errorf("steps in the wrong order: %v", titles)
	}
}

// e2e finding: a user manager keeps the groups it started with, so the agent
// couldn't open the shared database when tero-agent joined tero-shared after
// its manager started.
func TestAgentManagerStartsAfterTheSharedGroups(t *testing.T) {
	var titles []string
	for _, s := range steps(config.Config{}) {
		titles = append(titles, s.title)
	}

	shareAt := slices.Index(titles, "sharing the database between tero and tero-agent")
	managerAt := slices.Index(titles, "starting tero-agent's user manager")
	keysAt := slices.Index(titles, "starting the key service")
	agentAt := slices.Index(titles, "starting the Tero agent")
	if shareAt < 0 || managerAt < shareAt || keysAt < managerAt || agentAt < keysAt {
		t.Errorf("steps in the wrong order: %v", titles)
	}
}
