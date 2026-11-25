//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

// recoveryKey is the key init printed. testBackup replaces it with a new one
// and keeps the first in firstRecoveryKey.
var recoveryKey, firstRecoveryKey string

// backupFile is the backup testBackup made, which testRestore restores at the
// end of the suite.
const backupFile = "/home/ubuntu/tero-e2e-backup.age"

// recoveryKeyFrom finds the recovery key in a root command's output.
func recoveryKeyFrom(t *testing.T, output string) string {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		key := strings.TrimSpace(line)
		if strings.HasPrefix(key, "AGE-SECRET-KEY-PQ-1") {
			return key
		}
	}
	t.Fatalf("no recovery key in:\n%s", output)
	return ""
}

// backup-restore: Backup made / Wrong recovery key / Server with apps refused / New recovery key
func testBackup(t *testing.T) {
	if recoveryKey == "" {
		t.Fatal("init printed no recovery key")
	}
	if got := run(t, "tero backup"); got.exitCode == 0 {
		t.Error("backup ran without root")
	}

	got := must(t, "cd /home/ubuntu && sudo tero backup --output "+backupFile)
	assertContains(t, got, backupFile, "recovery key")
	if owner := must(t, "stat -c '%U %a' "+backupFile); owner != "ubuntu 600" {
		t.Errorf("the backup is %q, want ubuntu 600", owner)
	}
	if found := run(t, "grep -c --binary-files=text -e '"+searchPattern()+"' -e leanhanc/tero "+backupFile).output; strings.TrimSpace(found) != "0" {
		t.Error("the backup holds a value or a repository in the clear")
	}
	if got := run(t, "sudo tero backup --output "+backupFile); got.exitCode == 0 {
		t.Error("backup replaced an existing file")
	}

	// A backup opens only with its recovery key, and never onto a server
	// that has apps.
	other := run(t, "echo AGE-SECRET-KEY-PQ-1QQQQ | sudo tero restore --yes "+backupFile)
	if other.exitCode == 0 || !strings.Contains(other.output, "recovery key") {
		t.Errorf("restore with a wrong key = %d:\n%s", other.exitCode, other.output)
	}
	full := run(t, "echo "+recoveryKey+" | sudo tero restore --yes "+backupFile)
	if full.exitCode == 0 || !strings.Contains(full.output, "already has apps") {
		t.Errorf("restore onto a server with apps = %d:\n%s", full.exitCode, full.output)
	}
	assertContains(t, full.output, variablesApp+", from "+fixtureRepository)

	// A new key opens new backups; the old one still opens this backup.
	firstRecoveryKey = recoveryKey
	recoveryKey = recoveryKeyFrom(t, must(t, "sudo tero new-recovery-key"))
	if recoveryKey == firstRecoveryKey {
		t.Fatal("new-recovery-key printed the same key")
	}
	withNewKey := run(t, "echo "+recoveryKey+" | sudo tero restore --yes "+backupFile)
	if !strings.Contains(withNewKey.output, "doesn't open the backup") {
		t.Errorf("the new key opened an older backup:\n%s", withNewKey.output)
	}
}

// testRestore empties the server of apps and variables after assume-breach
// has run, as a fresh server would be, and restores the backup testBackup
// made.
//
// backup-restore: Apps and variables moved to a new server
func testRestore(t *testing.T) {
	const sharedDB = "/var/lib/tero-shared/db/tero.db"
	const keysDB = "/var/lib/tero-keys/keys.db"
	must(t, "sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -q sqlite3")
	must(t, agentSystemctl+" stop tero-agent")
	must(t, "sudo systemctl stop tero tero-keys.service tero-keys.socket")
	must(t, `sudo sqlite3 `+sharedDB+` "DELETE FROM app_pending_changes; DELETE FROM deploys; DELETE FROM apps;"`)
	must(t, `sudo sqlite3 `+keysDB+` "DELETE FROM app_variables; DELETE FROM app_sources;"`)
	must(t, "sudo systemctl start tero-keys.socket tero")

	got := must(t, "echo "+firstRecoveryKey+" | sudo tero restore --yes "+backupFile)
	assertContains(t, got, "Restored", "connect GitHub again")

	// tero-keys holds the variables again, sealed with this server's key.
	summary := must(t, "sudo "+keysCall("variables.summary"))
	assertContains(t, summary, `"app":"`+variablesApp+`"`, `"name":"E2E_SEALED","isSealed":true`, `"name":"E2E_RUNTIME"`)
	if strings.Contains(summary, valueMarker) {
		t.Errorf("the key service's summary shows a value: %s", summary)
	}

	// The restored app's queued deploy runs, and stops at the GitHub lookup:
	// the fixtures' repository became private in the GitHub tests, the GitHub
	// connection went with assume-breach, and the fake GitHub has stopped by
	// now. The release of its values is covered by the key service's unit
	// tests.
	must(t, agentSystemctl+" start tero-agent")
	var state string
	waitUntil(t, deployTimeout, func() bool {
		state = strings.TrimSpace(run(t, `sudo sqlite3 `+sharedDB+` "SELECT state || ': ' || reason FROM deploys JOIN apps ON apps.id = deploys.app_id WHERE name = '`+variablesApp+`' ORDER BY deploys.id DESC LIMIT 1"`).output)
		return strings.HasPrefix(state, "live") || strings.HasPrefix(state, "failed")
	})
	t.Logf("the variables app's deploy: %s", state)
	if !strings.HasPrefix(state, "failed") || !strings.Contains(state, "GitHub") {
		t.Errorf("the variables app's deploy = %q, want failed at the GitHub lookup", state)
	}
}
