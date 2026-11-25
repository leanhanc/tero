// Package backup is `sudo tero backup` and `sudo tero restore`: one file,
// encrypted to the recovery key, that moves a server's apps and their
// variables to a new server. The apps' settings come from the shared
// database; their variables and approved sources come from tero-keys already
// sealed to the recovery key, so root never sees a value. The login, sessions,
// the security log and the GitHub connection stay behind: the new server gets
// its own setup link, and the admin connects GitHub again there.
package backup

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/leanhanc/tero/internal/apps"
	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/recovery"
	"github.com/leanhanc/tero/internal/source"
)

// format versions the backup file's contents.
const format = 1

// maxRootDirectoryBytes matches what the key service accepts for a source.
const maxRootDirectoryBytes = 1024

var (
	// ErrNotEmpty refuses a restore onto a server that already has apps.
	ErrNotEmpty = errors.New("this server already has apps. Restore a backup only onto a freshly set up server")
	// ErrInvalidBackup means the file opened but isn't a backup Tero made.
	ErrInvalidBackup = errors.New("this file isn't a Tero backup this version can restore")
)

// App is an app's settings as a backup holds them.
type App struct {
	Name          string `json:"name"`
	DisplayName   string `json:"displayName"`
	Repository    string `json:"repository"`
	Branch        string `json:"branch"`
	RootDirectory string `json:"rootDirectory"`
	RunTogether   bool   `json:"runTogether"`
	DeployOnPush  bool   `json:"deployOnPush"`
	// PinnedRepositoryID is GitHub's id for the repository the app first
	// deployed, so the new server refuses another repository at the URL.
	PinnedRepositoryID int64 `json:"pinnedRepositoryId"`
}

// Contents is what a backup file holds once opened with the recovery key.
type Contents struct {
	Format    int       `json:"format"`
	CreatedAt time.Time `json:"createdAt"`
	// Domain is the dashboard domain of the server it was made on.
	Domain string `json:"domain"`
	Apps   []App  `json:"apps"`
	// Variables is tero-keys' export, sealed to the same recovery key, which
	// only tero-keys opens on the new server.
	Variables []byte `json:"variables"`
}

// Keys is what backups need from the key service.
type Keys interface {
	ExportBackup(ctx context.Context, recipient string) (keys.ExportedVariables, error)
	ImportBackup(ctx context.Context, secret string, sealed []byte) (keys.RestoredVariables, error)
}

// Created is a new backup file and what it holds.
type Created struct {
	File []byte
	Apps []App
	// Skipped names variables left out because the master key that sealed
	// them was lost.
	Skipped []string
}

// Create makes a backup of every app and its variables, encrypted to
// recipient.
func Create(ctx context.Context, db *sql.DB, keyService Keys, recipient, domain string, now time.Time) (Created, error) {
	stored, err := loadApps(ctx, db)
	if err != nil {
		return Created{}, err
	}
	exported, err := keyService.ExportBackup(ctx, recipient)
	if err != nil {
		return Created{}, fmt.Errorf("export the apps' variables: %w", err)
	}

	contents := Contents{Format: format, CreatedAt: now.UTC(), Domain: domain, Apps: stored, Variables: exported.Sealed}
	plaintext, err := json.Marshal(contents)
	if err != nil {
		return Created{}, err
	}
	file, err := recovery.Seal(recipient, plaintext)
	if err != nil {
		return Created{}, err
	}

	return Created{File: file, Apps: stored, Skipped: exported.Skipped}, nil
}

func loadApps(ctx context.Context, db *sql.DB) ([]App, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT name, display_name, repository, branch, root_directory, run_together, deploy_on_push, pinned_repository_id
		FROM apps ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("read the apps: %w", err)
	}
	defer rows.Close()

	stored := []App{}
	for rows.Next() {
		var app App
		err := rows.Scan(&app.Name, &app.DisplayName, &app.Repository, &app.Branch, &app.RootDirectory,
			&app.RunTogether, &app.DeployOnPush, &app.PinnedRepositoryID)
		if err != nil {
			return nil, err
		}
		stored = append(stored, app)
	}

	return stored, rows.Err()
}

// Open decrypts a backup file with the recovery key and checks what it
// holds, so the admin can see it before restoring.
func Open(secret string, file []byte) (Contents, error) {
	plaintext, err := recovery.Open(secret, file)
	if err != nil {
		return Contents{}, err
	}

	var contents Contents
	if err := json.Unmarshal(plaintext, &contents); err != nil || contents.Format != format {
		return Contents{}, ErrInvalidBackup
	}
	if err := checkApps(contents.Apps); err != nil {
		return Contents{}, err
	}

	return contents, nil
}

// checkApps applies the rules the dashboard does when it creates an app.
// The agent checks every field again before it uses one.
func checkApps(stored []App) error {
	seen := map[string]bool{}
	for _, app := range stored {
		_, repositoryErr := source.ParseRepository(app.Repository)
		isValid := apps.CheckName(app.Name) == nil && !seen[app.Name] && repositoryErr == nil &&
			source.CheckBranchName(app.Branch) == nil && len(app.RootDirectory) <= maxRootDirectoryBytes &&
			app.PinnedRepositoryID >= 0 && isValidDisplayName(app.DisplayName)
		if !isValid {
			return fmt.Errorf("%w: app %q isn't valid", ErrInvalidBackup, app.Name)
		}
		seen[app.Name] = true
	}

	return nil
}

func isValidDisplayName(displayName string) bool {
	if displayName == "" {
		return true
	}

	cleaned, err := apps.CleanDisplayName(displayName)
	return err == nil && cleaned == displayName
}

// Restored is what a restore stored.
type Restored struct {
	Apps      []App
	Variables keys.RestoredVariables
}

// Restore hands a backup's variables to tero-keys, then stores its apps and
// queues a deploy of every app. It refuses unless the server has no apps.
// tero-keys takes the same backup again, so a restore that stopped between
// the two steps can be run again.
func Restore(ctx context.Context, db *sql.DB, keyService Keys, secret string, contents Contents, now time.Time) (Restored, error) {
	if err := requireNoApps(ctx, db); err != nil {
		return Restored{}, err
	}
	variables, err := keyService.ImportBackup(ctx, secret, contents.Variables)
	if err != nil {
		return Restored{}, fmt.Errorf("restore the apps' variables: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Restored{}, err
	}
	defer tx.Rollback()

	if err := requireNoApps(ctx, tx); err != nil {
		return Restored{}, err
	}
	appIDs, err := insertApps(ctx, tx, contents.Apps, now)
	if err != nil {
		return Restored{}, err
	}
	if err := markVariables(ctx, tx, appIDs, variables); err != nil {
		return Restored{}, err
	}
	if err := queueDeploys(ctx, tx, appIDs, now); err != nil {
		return Restored{}, err
	}
	if err := tx.Commit(); err != nil {
		return Restored{}, fmt.Errorf("the variables were restored, but saving the apps failed: %w. Run the restore again", err)
	}

	return Restored{Apps: contents.Apps, Variables: variables}, nil
}

// querier is a database or a transaction.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func requireNoApps(ctx context.Context, db querier) error {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM apps").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return ErrNotEmpty
	}

	return nil
}

// insertApps stores each app with polling and GitHub's installation state
// cleared: the new server has neither until GitHub is connected there.
func insertApps(ctx context.Context, tx *sql.Tx, stored []App, now time.Time) (map[string]int64, error) {
	appIDs := map[string]int64{}
	for _, app := range stored {
		result, err := tx.ExecContext(ctx, `
			INSERT INTO apps (name, display_name, repository, branch, root_directory, run_together, deploy_on_push,
				pinned_repository_id, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			app.Name, app.DisplayName, app.Repository, app.Branch, app.RootDirectory, app.RunTogether, app.DeployOnPush,
			app.PinnedRepositoryID, now.Unix())
		if err != nil {
			return nil, fmt.Errorf("save app %s: %w", app.Name, err)
		}
		appID, err := result.LastInsertId()
		if err != nil {
			return nil, err
		}
		appIDs[app.Name] = appID
	}

	return appIDs, nil
}

func markVariables(ctx context.Context, tx *sql.Tx, appIDs map[string]int64, variables keys.RestoredVariables) error {
	for _, app := range variables.Apps {
		appID, hasApp := appIDs[app.App]
		if !hasApp || len(app.Variables) == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, "UPDATE apps SET has_variables = 1 WHERE id = ?", appID); err != nil {
			return err
		}
	}

	return nil
}

// queueDeploys inserts the deploys directly: deploys.Enqueue takes a
// transaction of its own, which would wait on this one.
func queueDeploys(ctx context.Context, tx *sql.Tx, appIDs map[string]int64, now time.Time) error {
	for _, appID := range appIDs {
		_, err := tx.ExecContext(ctx, "INSERT INTO deploys (app_id, trigger_type, queued_at) VALUES (?, 'manual', ?)", appID, now.Unix())
		if err != nil {
			return fmt.Errorf("queue a deploy: %w", err)
		}
	}

	return nil
}
