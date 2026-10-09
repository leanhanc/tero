// Package store opens Tero's SQLite database, which holds the dashboard login,
// sessions, login throttles and security events. The database belongs to the
// tero user: the service opens it directly, and root CLI commands open it
// through a child process running as tero, so every file stays owned by tero.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

// Path is the database file inside the service's StateDirectory.
const Path = "/var/lib/tero/tero.db"

//go:embed migrations/*.sql
var migrations embed.FS

// Open opens the database at path, creating it with mode 0600 when missing,
// and applies any pending migrations.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if err := createPrivateFile(path); err != nil {
		return nil, err
	}

	// WAL lets the service keep serving while a CLI command writes. The busy
	// timeout makes the two wait for each other instead of failing.
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	if err := migrate(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("prepare the database: %w", err)
	}

	return db, nil
}

// createPrivateFile makes sure the database file exists before SQLite opens
// it, so it is created with mode 0600 whatever the process umask is.
func createPrivateFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("create the database: %w", err)
	}

	return file.Close()
}

// migrate applies every embedded migration newer than the database's
// user_version, in file-name order, each in its own transaction.
func migrate(ctx context.Context, db *sql.DB) error {
	names, err := migrationNames()
	if err != nil {
		return err
	}

	for index, name := range names {
		version := index + 1
		if err := applyMigration(ctx, db, version, name); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}

	return nil
}

func migrationNames() ([]string, error) {
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		return nil, err
	}

	var names []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	return names, nil
}

// applyMigration runs one migration unless the database already has it. The
// version check happens inside the write transaction, so the service and a
// CLI command starting at the same time can't both apply it.
func applyMigration(ctx context.Context, db *sql.DB, version int, name string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var currentVersion int
	if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&currentVersion); err != nil {
		return err
	}
	if currentVersion >= version {
		return nil
	}

	statements, err := migrations.ReadFile("migrations/" + name)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, string(statements)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return err
	}

	return tx.Commit()
}
