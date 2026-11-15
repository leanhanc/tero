package keys

import (
	"testing"
	"time"
)

// Audit finding (2026-11-10): junk passkey answers from strangers used up the
// whole server's passkey budget, so the admin's browser got "busy" too.
func TestStrangersCantUseUpTrustedBrowsersPasskeyBudget(t *testing.T) {
	var l limits
	now := time.Unix(1_800_000_000, 0)
	for range passkeyCheckLimit {
		if !l.allowPasskeyCheck(now, false) {
			t.Fatal("a stranger's answer was refused within the budget")
		}
	}
	if l.allowPasskeyCheck(now, false) {
		t.Error("a stranger's answer past the budget was allowed")
	}
	if !l.allowPasskeyCheck(now, true) {
		t.Error("a trusted browser's answer was refused after strangers used up their budget")
	}
}
