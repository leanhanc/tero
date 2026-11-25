package backup

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/recovery"
	"github.com/leanhanc/tero/internal/store"
)

// fakeKeys stands in for tero-keys: it returns an opaque blob on export and
// expects the same blob back on import.
type fakeKeys struct {
	blob      []byte
	importErr error
	imported  [][]byte
}

func (k *fakeKeys) ExportBackup(context.Context, string) (keys.ExportedVariables, error) {
	return keys.ExportedVariables{Sealed: k.blob, Skipped: []string{}}, nil
}

func (k *fakeKeys) ImportBackup(_ context.Context, _ string, sealed []byte) (keys.RestoredVariables, error) {
	k.imported = append(k.imported, sealed)
	if k.importErr != nil {
		return keys.RestoredVariables{}, k.importErr
	}
	return keys.RestoredVariables{Apps: []keys.AppVariablesSummary{
		{App: "shop", Variables: []keys.SummarizedVariable{{Name: "TOKEN", IsSealed: true}}},
	}}, nil
}

func openStore(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func addApp(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO apps (name, display_name, repository, branch, root_directory, deploy_on_push,
		pinned_repository_id, github_repository_id, poll_failures, created_at)
		VALUES (?, 'Shop', 'https://github.com/acme/shop', 'main', 'web', 0, 77, 55, 2, 1)`, name)
	if err != nil {
		t.Fatal(err)
	}
}

var now = time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)

// backup-restore: Apps moved to a new server
func TestBackupMovesAppsToANewServer(t *testing.T) {
	ctx := context.Background()
	key, err := recovery.Generate()
	if err != nil {
		t.Fatal(err)
	}
	old := openStore(t)
	addApp(t, old, "shop")
	addApp(t, old, "blog")
	keyService := &fakeKeys{blob: []byte("sealed variables")}

	created, err := Create(ctx, old, keyService, key.Recipient, "old.example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(created.File, []byte("acme/shop")) {
		t.Fatal("the backup file isn't encrypted")
	}

	contents, err := Open(key.Secret, created.File)
	if err != nil {
		t.Fatal(err)
	}
	if contents.Domain != "old.example.com" || len(contents.Apps) != 2 {
		t.Fatalf("opened %+v", contents)
	}

	fresh := openStore(t)
	restored, err := Restore(ctx, fresh, keyService, key.Secret, contents, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Apps) != 2 || !bytes.Equal(keyService.imported[0], []byte("sealed variables")) {
		t.Fatalf("restored %+v, imported %q", restored, keyService.imported)
	}

	var deployOnPush, hasVariables bool
	var pinned, gitHubID int64
	var pollFailures, queued int
	err = fresh.QueryRow(`SELECT deploy_on_push, has_variables, pinned_repository_id, github_repository_id, poll_failures,
		(SELECT count(*) FROM deploys WHERE app_id = apps.id AND state = 'queued') FROM apps WHERE name = 'shop'`).
		Scan(&deployOnPush, &hasVariables, &pinned, &gitHubID, &pollFailures, &queued)
	if err != nil {
		t.Fatal(err)
	}
	if deployOnPush || !hasVariables || pinned != 77 || gitHubID != 0 || pollFailures != 0 || queued != 1 {
		t.Errorf("shop restored as deployOnPush=%v hasVariables=%v pinned=%d gitHubID=%d pollFailures=%d queued=%d",
			deployOnPush, hasVariables, pinned, gitHubID, pollFailures, queued)
	}

	if _, err := Restore(ctx, fresh, keyService, key.Secret, contents, now); !errors.Is(err, ErrNotEmpty) {
		t.Errorf("a second restore: %v, want ErrNotEmpty", err)
	}
}

// backup-restore: Nothing kept from a failed restore
func TestFailedVariablesKeepNoApps(t *testing.T) {
	ctx := context.Background()
	key, _ := recovery.Generate()
	old := openStore(t)
	addApp(t, old, "shop")
	keyService := &fakeKeys{blob: []byte("sealed")}
	created, err := Create(ctx, old, keyService, key.Recipient, "old.example.com", now)
	if err != nil {
		t.Fatal(err)
	}
	contents, err := Open(key.Secret, created.File)
	if err != nil {
		t.Fatal(err)
	}

	fresh := openStore(t)
	keyService.importErr = keys.ErrNotEmpty
	if _, err := Restore(ctx, fresh, keyService, key.Secret, contents, now); !errors.Is(err, keys.ErrNotEmpty) {
		t.Fatalf("Restore = %v, want the key service's error", err)
	}
	var count int
	fresh.QueryRow("SELECT (SELECT count(*) FROM apps) + (SELECT count(*) FROM deploys)").Scan(&count)
	if count != 0 {
		t.Errorf("%d apps and deploys stayed after the variables failed", count)
	}
}

// backup-restore: Crafted backup refused
func TestCraftedAppsRefused(t *testing.T) {
	key, _ := recovery.Generate()
	crafted := map[string]App{
		"bad name":        {Name: "../x", Repository: "https://github.com/acme/shop", Branch: "main"},
		"not GitHub":      {Name: "shop", Repository: "https://evil.example/acme/shop", Branch: "main"},
		"bad branch":      {Name: "shop", Repository: "https://github.com/acme/shop", Branch: "-main"},
		"control in name": {Name: "shop", DisplayName: "Shop‮", Repository: "https://github.com/acme/shop", Branch: "main"},
	}
	for name, app := range crafted {
		plaintext, _ := json.Marshal(Contents{Format: format, Apps: []App{app}})
		file, err := recovery.Seal(key.Recipient, plaintext)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Open(key.Secret, file); !errors.Is(err, ErrInvalidBackup) {
			t.Errorf("%s: %v, want ErrInvalidBackup", name, err)
		}
	}
}
