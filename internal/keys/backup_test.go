package keys

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/leanhanc/tero/internal/recovery"
)

// backup-restore: Variables moved to a new server
func TestBackupMovesVariablesToANewServer(t *testing.T) {
	old := newTestEnv(t, map[int][]byte{1: testKey(1)})
	old.setUp(t)
	old.mustSave(t, setVariable("API_URL", "https://api", ScopeBoth), sealVariable("TOKEN", "s3cret"))
	if _, err := old.release(PhaseRun, testSource); err != nil {
		t.Fatal(err)
	}
	key, err := recovery.Generate()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	exported, err := old.root.ExportBackup(ctx, key.Recipient)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(exported.Sealed), "s3cret") {
		t.Fatal("the export holds a value in the clear")
	}
	if _, err := old.dashboard.ExportBackup(ctx, key.Recipient); !errors.Is(err, ErrRefused) {
		t.Errorf("the dashboard exported a backup: %v", err)
	}
	if _, err := old.agent().ExportBackup(ctx, key.Recipient); !errors.Is(err, ErrRefused) {
		t.Errorf("the agent exported a backup: %v", err)
	}

	fresh := newTestEnv(t, map[int][]byte{3: testKey(3)})
	other, _ := recovery.Generate()
	if _, err := fresh.root.ImportBackup(ctx, other.Secret, exported.Sealed); !errors.Is(err, recovery.ErrWrongKey) {
		t.Errorf("another recovery key: %v, want ErrWrongKey", err)
	}
	restored, err := fresh.root.ImportBackup(ctx, key.Secret, exported.Sealed)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Apps) != 1 || len(restored.Apps[0].Variables) != 2 {
		t.Fatalf("restored %+v", restored)
	}

	// The pinned repository id came along, so the same source is released and
	// another repository at the URL isn't.
	values, err := fresh.agent().ReleaseVariables(ctx, ReleaseRequest{App: testApp, Source: withRepositoryID(testSource, testRepositoryID), Phase: PhaseRun, DeployID: 1})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]ReleasedVariable{}
	for _, value := range values {
		got[value.Name] = value
	}
	if got["TOKEN"].Value != "s3cret" || !got["TOKEN"].IsSealed || got["API_URL"].Value != "https://api" {
		t.Errorf("released %+v", values)
	}
	_, err = fresh.agent().ReleaseVariables(ctx, ReleaseRequest{App: testApp, Source: withRepositoryID(testSource, 999), Phase: PhaseRun, DeployID: 2})
	if !errors.Is(err, ErrSourceNotApproved) {
		t.Errorf("another repository at the URL: %v, want ErrSourceNotApproved", err)
	}

	// The same backup again succeeds, so a restore that stopped after this
	// step can be run again; any other is refused.
	if _, err := fresh.root.ImportBackup(ctx, key.Secret, exported.Sealed); err != nil {
		t.Errorf("the same backup again: %v", err)
	}
	old.mustSave(t, setVariable("API_URL", "https://other", ScopeBoth))
	changed, err := old.root.ExportBackup(ctx, key.Recipient)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.root.ImportBackup(ctx, key.Secret, changed.Sealed); !errors.Is(err, ErrNotEmpty) {
		t.Errorf("another backup: %v, want ErrNotEmpty", err)
	}
	assertEventRecorded(t, old, EventBackupExported)
	assertEventRecorded(t, fresh, EventBackupRestored)
}

// backup-restore: Crafted backup refused
func TestCraftedBackupRefused(t *testing.T) {
	key, _ := recovery.Generate()
	ctx := context.Background()
	crafted := map[string]string{
		"bad app name":    `{"format":1,"apps":[{"app":"../x","source":{"repository":"r","branch":"b"},"variables":[]}]}`,
		"PORT":            `{"format":1,"apps":[{"app":"shop","source":{"repository":"r","branch":"b"},"variables":[{"name":"PORT","value":"1","scope":"both"}]}]}`,
		"sealed RAILPACK": `{"format":1,"apps":[{"app":"shop","source":{"repository":"r","branch":"b"},"variables":[{"name":"RAILPACK_X","value":"1","scope":"runtime","isSealed":true}]}]}`,
		"no source":       `{"format":1,"apps":[{"app":"shop","source":{},"variables":[]}]}`,
		"other format":    `{"format":2,"apps":[]}`,
	}
	for name, contents := range crafted {
		env := newTestEnv(t, map[int][]byte{1: testKey(1)})
		sealed, err := recovery.Seal(key.Recipient, []byte(contents))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := env.root.ImportBackup(ctx, key.Secret, sealed); !errors.Is(err, ErrInvalidBackup) {
			t.Errorf("%s: %v, want ErrInvalidBackup", name, err)
		}
	}
}

func withRepositoryID(source AppSource, id int64) AppSource {
	source.RepositoryID = id
	return source
}

func assertEventRecorded(t *testing.T, env testEnv, eventType EventType) {
	t.Helper()
	events, err := env.root.RecentEvents(context.Background(), "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Type == eventType {
			return
		}
	}
	t.Errorf("no %s event in %+v", eventType, events)
}
