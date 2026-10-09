package initflow

import (
	"context"
	"embed"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/leanhanc/tero/internal/config"
)

//go:embed files
var embedded embed.FS

// step is one idempotent part of init.
type step struct {
	title string
	apply func(ctx context.Context, h host) error
}

// packages are installed from the distro's own repositories.
var packages = []string{
	"podman", "uidmap", "passt", "dbus-user-session",
	"nftables", "auditd", "apparmor", "apparmor-utils", "unattended-upgrades",
}

func steps(domain string) []step {
	return []step{
		{"installing Podman, nftables, auditd and automatic updates", installPackages},
		{"creating the tero system user", createTeroUser},
		{"locking down SSH", hardenSSH},
		{"turning on the firewall", enableFirewall},
		{"turning on automatic security updates", enableAutomaticUpdates},
		{"hardening kernel settings", hardenKernel},
		{"turning on auditing and checking AppArmor", enableAuditing},
		{"disabling unneeded services", disableUnneededServices},
		{"setting permissions on sensitive files", setSensitivePermissions},
		{"saving Tero's configuration", saveConfig(domain)},
		{"starting the Tero service", startService},
	}
}

// installPackages runs apt to completion even if init is interrupted:
// killing dpkg halfway leaves the package database broken. It first finishes
// any install an earlier interruption left half-done, and keeps the admin's
// own changes to config files instead of prompting about them.
func installPackages(ctx context.Context, h host) error {
	uninterruptible := context.WithoutCancel(ctx)
	keepConfigFiles := []string{"-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold"}

	if _, err := h.run.Run(uninterruptible, "dpkg", "--configure", "-a"); err != nil {
		return err
	}
	if _, err := h.run.Run(uninterruptible, "apt-get", "update"); err != nil {
		return err
	}

	installArgs := append(append([]string{"install", "-y"}, keepConfigFiles...), packages...)
	_, err := h.run.Run(uninterruptible, "apt-get", installArgs...)
	return err
}

func hardenSSH(ctx context.Context, h host) error {
	if !hasSSHServer(h.files) {
		return nil
	}

	settings, err := hardeningDropIn(ctx, h)
	if err != nil {
		return err
	}

	const dropIn = "/etc/ssh/sshd_config.d/00-tero-hardening.conf"
	if _, err := h.files.WriteFile(dropIn, settings, 0o600); err != nil {
		return err
	}

	// Validated and reloaded on every run, even when the file is unchanged:
	// an earlier run may have stopped between writing it and reloading.
	if _, err := h.run.Run(ctx, "sshd", "-t"); err != nil {
		os.Remove(h.files.Path(dropIn))
		return fmt.Errorf("the SSH server rejected Tero's settings, so they were removed again: %w", err)
	}
	if err := checkEffectiveSSH(ctx, h); err != nil {
		os.Remove(h.files.Path(dropIn))
		return fmt.Errorf("%w\nTero's SSH settings were removed again", err)
	}

	_, err = h.run.Run(ctx, "systemctl", "try-reload-or-restart", "ssh.service")
	return err
}

// requiredSSHSettings must hold in the SSH server's effective configuration,
// for the admin as well as by default.
var requiredSSHSettings = map[string]string{
	"permitrootlogin":              "no",
	"passwordauthentication":       "no",
	"kbdinteractiveauthentication": "no",
	"permitemptypasswords":         "no",
}

// checkEffectiveSSH catches settings that override Tero's drop-in: lines
// placed before its Include in sshd_config, or Match blocks.
func checkEffectiveSSH(ctx context.Context, h host) error {
	checks := [][]string{{"-T"}}
	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser != "" && sudoUser != "root" {
		checks = append(checks, []string{"-T", "-C", "user=" + sudoUser + ",host=localhost,addr=127.0.0.1"})
	}

	for _, args := range checks {
		output, err := h.run.Run(ctx, "sshd", args...)
		if err != nil {
			return err
		}

		settings := parseSSHSettings(output)
		for key, want := range requiredSSHSettings {
			if got := settings.first(key); got != want {
				return fmt.Errorf("another SSH setting overrides Tero's: %s is %q instead of %q. Check /etc/ssh/sshd_config and its Match blocks", key, got, want)
			}
		}
	}

	return nil
}

// preferredKex lists the SSH key exchanges Tero allows, best first. Older
// OpenSSH releases lack some of them, such as the post-quantum mlkem768x25519
// in OpenSSH 9.6 on Ubuntu 24.04.
var preferredKex = []string{
	"mlkem768x25519-sha256",
	"sntrup761x25519-sha512",
	"sntrup761x25519-sha512@openssh.com",
	"curve25519-sha256",
	"curve25519-sha256@libssh.org",
}

// hardeningDropIn is the SSH hardening drop-in with a KexAlgorithms line
// limited to the exchanges this host's OpenSSH supports.
func hardeningDropIn(ctx context.Context, h host) ([]byte, error) {
	settings, err := embedded.ReadFile("files/sshd-hardening.conf")
	if err != nil {
		return nil, err
	}

	supportedOutput, err := h.run.Run(ctx, "ssh", "-Q", "kex")
	if err != nil {
		return nil, err
	}

	supported := strings.Fields(supportedOutput)
	var kex []string
	for _, algorithm := range preferredKex {
		if slices.Contains(supported, algorithm) {
			kex = append(kex, algorithm)
		}
	}
	if len(kex) == 0 {
		return nil, fmt.Errorf("this server's OpenSSH supports none of the key exchanges Tero allows: %s", strings.Join(preferredKex, ", "))
	}

	return append(settings, "KexAlgorithms "+strings.Join(kex, ",")+"\n"...), nil
}

func enableFirewall(ctx context.Context, h host) error {
	ports, err := sshPorts(ctx, h)
	if err != nil {
		return err
	}
	rules, err := firewallRules(ports)
	if err != nil {
		return err
	}

	if _, err := h.files.WriteFile("/etc/tero/firewall.nft", rules, 0o600); err != nil {
		return err
	}
	if _, err := writeEmbedded(h, "tero-firewall.service", "/etc/systemd/system/tero-firewall.service", 0o644); err != nil {
		return err
	}

	if err := runAll(ctx, h,
		[]string{"systemctl", "daemon-reload"},
		[]string{"systemctl", "enable", "tero-firewall.service"},
		[]string{"systemctl", "reload-or-restart", "tero-firewall.service"},
	); err != nil {
		return err
	}

	if _, err := h.run.Run(ctx, "nft", "list", "table", "inet", "tero"); err != nil {
		return fmt.Errorf("the firewall rules didn't load: %w", err)
	}

	return nil
}

func enableAutomaticUpdates(ctx context.Context, h host) error {
	if _, err := writeEmbedded(h, "20auto-upgrades", "/etc/apt/apt.conf.d/20auto-upgrades", 0o644); err != nil {
		return err
	}

	_, err := h.run.Run(ctx, "systemctl", "enable", "--now", "apt-daily.timer", "apt-daily-upgrade.timer", "unattended-upgrades.service")
	return err
}

// hardenKernel applies the sysctls the way boot does, then checks each one.
// The file sorts after every other sysctl.d file, including sysctl.conf
// (linked as 99-sysctl.conf), so its values win at boot too.
func hardenKernel(ctx context.Context, h host) error {
	const settingsPath = "/etc/sysctl.d/99-zz-tero-hardening.conf"
	if _, err := writeEmbedded(h, "sysctl-hardening.conf", settingsPath, 0o644); err != nil {
		return err
	}
	if _, err := writeEmbedded(h, "limits-core.conf", "/etc/security/limits.d/60-tero-core.conf", 0o644); err != nil {
		return err
	}

	// sysctl --system fails when any file on the host names a key this kernel
	// lacks, which says nothing about Tero's settings; checking each of them
	// afterwards is what matters.
	h.run.Run(ctx, "sysctl", "--system")

	return checkKernelSettings(ctx, h)
}

func checkKernelSettings(ctx context.Context, h host) error {
	settings, err := embedded.ReadFile("files/sysctl-hardening.conf")
	if err != nil {
		return err
	}

	for _, line := range strings.Split(string(settings), "\n") {
		key, want, isSetting := strings.Cut(line, "=")
		isComment := strings.HasPrefix(strings.TrimSpace(line), "#")
		if !isSetting || isComment {
			continue
		}

		key, want = strings.TrimSpace(key), strings.TrimSpace(want)
		got, err := h.run.Run(ctx, "sysctl", "-n", key)
		if err != nil {
			return err
		}
		if strings.Join(strings.Fields(got), " ") != want {
			return fmt.Errorf("the kernel setting %s is %q instead of %q. Another file in /etc/sysctl.d, /run/sysctl.d or /usr/lib/sysctl.d may set it", key, got, want)
		}
	}

	return nil
}

func enableAuditing(ctx context.Context, h host) error {
	if _, err := writeEmbedded(h, "audit.rules", "/etc/audit/rules.d/60-tero.rules", 0o640); err != nil {
		return err
	}

	// From audit 4 on (Ubuntu 26.04), audit-rules.service loads the rules
	// right after auditd starts. Running augenrules next to it makes the two
	// loads race, so restart that unit instead when it exists.
	loadRules := []string{"augenrules", "--load"}
	if h.files.Exists("/usr/lib/systemd/system/audit-rules.service") {
		loadRules = []string{"systemctl", "restart", "audit-rules.service"}
	}

	if err := runAll(ctx, h,
		[]string{"systemctl", "enable", "--now", "auditd.service"},
		loadRules,
	); err != nil {
		return err
	}

	if _, err := h.run.Run(ctx, "aa-enabled", "--quiet"); err != nil {
		return fmt.Errorf("AppArmor is not enabled on this kernel; Tero needs it to confine processes: %w", err)
	}

	return nil
}

func saveConfig(domain string) func(context.Context, host) error {
	return func(_ context.Context, h host) error {
		return config.Save(h.files, config.Config{DashboardDomain: domain})
	}
}

func startService(ctx context.Context, h host) error {
	if _, err := writeEmbedded(h, "tero.service", "/etc/systemd/system/tero.service", 0o644); err != nil {
		return err
	}

	return runAll(ctx, h,
		[]string{"systemctl", "daemon-reload"},
		[]string{"systemctl", "enable", "tero.service"},
		[]string{"systemctl", "restart", "tero.service"},
	)
}

// writeEmbedded copies a file from files/ to the host and reports whether it
// changed.
func writeEmbedded(h host, name, hostPath string, mode os.FileMode) (bool, error) {
	data, err := embedded.ReadFile("files/" + name)
	if err != nil {
		return false, err
	}

	return h.files.WriteFile(hostPath, data, mode)
}

func runAll(ctx context.Context, h host, commands ...[]string) error {
	for _, command := range commands {
		if _, err := h.run.Run(ctx, command[0], command[1:]...); err != nil {
			return err
		}
	}

	return nil
}

// waitFor polls isReady until it returns true or the timeout passes.
func waitFor(ctx context.Context, timeout time.Duration, isReady func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isReady() {
			return true
		}

		select {
		case <-ctx.Done():
			return false
		case <-time.After(time.Second):
		}
	}

	return false
}
