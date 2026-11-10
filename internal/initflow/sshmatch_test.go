package initflow

import (
	"context"
	"strings"
	"testing"

	"github.com/leanhanc/tero/internal/hostfs"
	"github.com/leanhanc/tero/internal/sysexec"
)

// Audit finding TERO-08: `sshd -T` checks missed Match blocks for other users
// and addresses.
func TestHardenSSHRefusesAMatchBlockForAnotherConnection(t *testing.T) {
	h := newGuardHost(t)
	recorder := h.run.(*sysexec.Recorder)
	recorder.Outputs["ssh -Q kex"] = "curve25519-sha256\n"
	writeHostFile(t, h.files, sshdConfigPath, "Include /etc/ssh/sshd_config.d/*.conf\nPasswordAuthentication no\n")
	writeHostFile(t, h.files, "/etc/ssh/sshd_config.d/50-office.conf", "Match Address 10.0.0.0/8\n    PasswordAuthentication yes\n")

	err := hardenSSH(context.Background(), h)
	if err == nil || !strings.Contains(err.Error(), "50-office.conf sets passwordauthentication") {
		t.Fatalf("hardenSSH() = %v", err)
	}
	if h.files.Exists("/etc/ssh/sshd_config.d/00-tero-hardening.conf") {
		t.Error("the drop-in was left in place")
	}
}

func TestFindMatchOverride(t *testing.T) {
	tests := map[string]struct {
		config   string
		included string
		want     string
	}{
		"no Match block":                {config: "PasswordAuthentication yes\n"},
		"Match block keeping Tero's":    {config: "Match User git\n  PasswordAuthentication no\n  ForceCommand git-shell\n"},
		"root login in a Match block":   {config: "Match Address 192.0.2.1\n  PermitRootLogin prohibit-password\n", want: "permitrootlogin"},
		"equals sign and other case":    {config: "match group admins\nKbdInteractiveAuthentication=yes\n", want: "kbdinteractiveauthentication"},
		"Include inside a Match block":  {config: "Match User ana\n  Include extra.conf\n", included: "PermitEmptyPasswords yes\n", want: "permitemptypasswords"},
		"Match carried out of include":  {config: "Include extra.conf\nPasswordAuthentication yes\n", included: "Match User bo\n", want: "sshd_config sets passwordauthentication"},
		"commented out Match block":     {config: "#Match User anoncvs\n#\tPasswordAuthentication yes\n"},
		"deprecated alias":              {config: "Match User other\n  ChallengeResponseAuthentication yes\n", want: "challengeresponseauthentication"},
		"quoted include":                {config: "Match User ana\n  Include \"/etc/ssh/extra.conf\"\n", included: "PasswordAuthentication yes\n", want: "passwordauthentication"},
		"quoted value and comment":      {config: "Match User git\n  PasswordAuthentication \"no\" # keys only\n"},
		"included file with no matches": {config: "Include /etc/ssh/missing.d/*.conf\n"},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			files := hostfs.FS{Root: t.TempDir()}
			writeHostFile(t, files, sshdConfigPath, test.config)
			if test.included != "" {
				writeHostFile(t, files, "/etc/ssh/extra.conf", test.included)
			}

			override, err := findMatchOverride(files)
			if err != nil {
				t.Fatal(err)
			}
			isExpected := (test.want == "" && override == "") || (test.want != "" && strings.Contains(override, test.want))
			if !isExpected {
				t.Errorf("findMatchOverride() = %q, want one naming %q", override, test.want)
			}
		})
	}
}
