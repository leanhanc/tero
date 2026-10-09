package store

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCreatesPrivateDatabaseWithSchema(t *testing.T) {
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
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("database mode = %o, want 600", mode)
	}

	for _, table := range []string{"admin", "setup_links", "sessions", "login_throttles", "security_events"} {
		var name string
		err := db.QueryRow("SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %s missing: %v", table, err)
		}
	}
}

func TestOpenTwiceKeepsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tero.db")
	ctx := context.Background()

	first, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Exec("INSERT INTO security_events (at, type) VALUES (1, 'test')"); err != nil {
		t.Fatal(err)
	}
	first.Close()

	second, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	var count int
	if err := second.QueryRow("SELECT count(*) FROM security_events").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("events after reopening = %d, want 1", count)
	}
}
