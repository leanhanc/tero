package keys

import (
	"context"
	"testing"
)

// Audit finding (2026-11-10): a flood of refused webhooks pushed out the
// record of released sealed variables and other events missing from a kept
// list. Only the kinds anyone can cause from the internet are pruned now.
func TestFloodKeepsEveryEventItCantCause(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	ctx := context.Background()
	kept := []EventType{EventSealedVariableReleased, EventVariablesChanged, EventSessionsEnded, EventAccountConfirmed,
		EventPasskeyCounterMismatch, EventSetupLinkIssued}
	for _, eventType := range kept {
		if err := env.service.record(ctx, Event{At: env.clock.Now(), Type: eventType, Source: SourceKeys}); err != nil {
			t.Fatal(err)
		}
	}

	tx, _ := env.service.db.BeginTx(ctx, nil)
	for range maxEvents + 10 {
		tx.ExecContext(ctx, "INSERT INTO security_events (at, type, source, ip, count) VALUES (?, 'webhook_signature_failed', 'tero-keys', '', 1)",
			env.clock.Now().Unix())
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := env.service.Prune(ctx); err != nil {
		t.Fatal(err)
	}

	for _, eventType := range kept {
		var count int
		env.service.db.QueryRowContext(ctx, "SELECT count(*) FROM security_events WHERE type = ?", string(eventType)).Scan(&count)
		if count != 1 {
			t.Errorf("%s was pruned", eventType)
		}
	}
	var total int
	env.service.db.QueryRowContext(ctx, "SELECT count(*) FROM security_events").Scan(&total)
	if total > maxEvents+len(kept) {
		t.Errorf("%d events left after the prune", total)
	}
}
