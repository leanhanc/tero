package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCreatesSharedDatabaseWithSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "tero.db")

	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o660 {
		t.Errorf("database mode = %o, want 660", mode)
	}
	db.Exec("CREATE TABLE wal_check (id INTEGER)")
	walInfo, err := os.Stat(path + "-wal")
	if err != nil {
		t.Fatal(err)
	}
	if mode := walInfo.Mode().Perm(); mode != 0o660 {
		t.Errorf("WAL mode = %o, want 660", mode)
	}

	for _, table := range []string{"apps", "deploys", "served_routes"} {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
	}
}

// The admin's credentials, passkeys, sessions, trusted browsers, throttles
// and the security log belong to the key service (dashboard-access: No
// credentials in the dashboard's database).
func TestDatabaseHoldsNoCredentials(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "tero.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	tables := []string{"admin", "setup_links", "security_events", "passkeys", "sessions", "trusted_devices", "login_throttles"}
	for _, table := range tables {
		var count int
		db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name = ?", table).Scan(&count)
		if count != 0 {
			t.Errorf("table %s exists in tero's database", table)
		}
	}
}

func TestOpenWithCreatesPrivateDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys.db")

	db, err := OpenWith(context.Background(), path, migrations)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("database mode = %o, want 600", mode)
	}
}

func TestOpenTwiceKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tero.db")
	ctx := context.Background()

	first, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Exec("INSERT INTO apps (name, repository, branch, created_at) VALUES ('shop', 'https://github.com/a/b', 'main', 1)"); err != nil {
		t.Fatal(err)
	}
	first.Close()

	second, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	var count int
	if err := second.QueryRow("SELECT count(*) FROM apps").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("apps after reopening = %d, want 1", count)
	}
}

func TestDeploysAllowOneRunningOneQueuedAndOneLivePerApp(t *testing.T) {
	db, err := Open(context.Background(), filepath.Join(t.TempDir(), "tero.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mustExec(t, db, "INSERT INTO apps (name, repository, branch, created_at) VALUES ('api', 'https://github.com/o/r', 'main', 1)")
	mustExec(t, db, "INSERT INTO deploys (app_id, state, queued_at) VALUES (1, 'live', 1)")
	mustExec(t, db, "INSERT INTO deploys (app_id, state, queued_at) VALUES (1, 'building', 2)")
	mustExec(t, db, "INSERT INTO deploys (app_id, state, queued_at) VALUES (1, 'queued', 3)")

	if _, err := db.Exec("INSERT INTO deploys (app_id, state, queued_at) VALUES (1, 'queued', 4)"); err == nil {
		t.Error("a second queued deploy for the same app was accepted")
	}
	if _, err := db.Exec("INSERT INTO deploys (app_id, state, queued_at) VALUES (1, 'starting', 4)"); err == nil {
		t.Error("a second running deploy for the same app was accepted")
	}
	if _, err := db.Exec("INSERT INTO deploys (app_id, state, queued_at) VALUES (1, 'live', 3)"); err == nil {
		t.Error("a second live deploy for the same app was accepted")
	}
}

func mustExec(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	if _, err := db.Exec(statement); err != nil {
		t.Fatal(err)
	}
}
