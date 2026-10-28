// Package store opens Tero's SQLite database, which holds apps and deploys.
// Sessions and everything else about the login belong to the key service.
// The database is shared by the service (user tero) and the agent (user
// tero-agent) through the group tero-shared, and is the only channel between
// them.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "modernc.org/sqlite"
)

// Path is the shared database. Its directory is root's, group tero-shared,
// mode 2770, so every file in it gets that group.
const Path = "/var/lib/tero-shared/tero.db"

//go:embed migrations/*.sql
var migrations embed.FS

// Open opens the shared database at path, creating it with mode 0660 when
// missing, and applies any pending migrations. SQLite gives its WAL and
// shared-memory files the database file's mode.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if err := refuseLinks(path, path+"-wal", path+"-shm"); err != nil {
		return nil, err
	}
	if err := createFile(path, sharedFileMode); err != nil {
		return nil, err
	}

	return open(ctx, path, migrations)
}

// OpenWith opens a database the same way with another set of migrations, read
// from the "migrations" directory of source. The key service keeps its own
// database this way.
func OpenWith(ctx context.Context, path string, source fs.FS) (*sql.DB, error) {
	if err := createFile(path, privateFileMode); err != nil {
		return nil, err
	}

	return open(ctx, path, source)
}

func open(ctx context.Context, path string, source fs.FS) (*sql.DB, error) {
	// WAL lets the service keep serving while a CLI command writes. The busy
	// timeout makes the two wait for each other instead of failing.
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	if err := migrate(ctx, db, source); err != nil {
		db.Close()
		return nil, fmt.Errorf("prepare the database: %w", err)
	}

	return db, nil
}

// refuseLinks refuses to open the shared database when one of its files is
// a link. Both users can create files in its directory, and SQLite follows
// links, so a link would make the other process write where it points.
func refuseLinks(paths ...string) error {
	for _, path := range paths {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is a link; the database's files must be regular files", path)
		}
	}

	return nil
}

const (
	privateFileMode os.FileMode = 0o600
	sharedFileMode  os.FileMode = 0o660
)

// createFile makes sure the database file exists before SQLite opens it, so
// it is created with mode whatever the process umask is. A file another
// process created is left as it is.
func createFile(path string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE|os.O_EXCL, mode)
	if errors.Is(err, os.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("create the database: %w", err)
	}
	defer file.Close()

	// The umask may have taken bits off the mode OpenFile asked for.
	return file.Chmod(mode)
}

// migrate applies every embedded migration newer than the database's
// user_version, in file-name order, each in its own transaction.
func migrate(ctx context.Context, db *sql.DB, source fs.FS) error {
	names, err := migrationNames(source)
	if err != nil {
		return err
	}

	for index, name := range names {
		version := index + 1
		if err := applyMigration(ctx, db, source, version, name); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}

	return nil
}

func migrationNames(source fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(source, "migrations")
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
func applyMigration(ctx context.Context, db *sql.DB, source fs.FS, version int, name string) error {
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

	statements, err := fs.ReadFile(source, "migrations/"+name)
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
