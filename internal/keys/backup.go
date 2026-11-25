package keys

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/leanhanc/tero/internal/recovery"
)

// Backups move apps' variables to a new server. The key service encrypts
// them to the recovery key's recipient itself, so their values never leave it
// in the clear: root gets a sealed blob it can't open, and on the new server
// hands the blob back with the recovery key, which only the admin holds.

var (
	// ErrNotEmpty refuses a restore on a server that already has variables
	// or approved sources.
	ErrNotEmpty = errors.New("this server already has app variables, so a backup can't be restored onto it")
	// ErrInvalidBackup means a backup opened but holds something Tero
	// wouldn't have stored.
	ErrInvalidBackup = errors.New("the backup holds variables Tero doesn't accept")
)

// backupFormat versions what ExportBackup seals.
const backupFormat = 1

// BackupVariable is a variable as a backup holds it, value included.
type BackupVariable struct {
	Name     string        `json:"name"`
	Value    string        `json:"value"`
	Scope    VariableScope `json:"scope"`
	IsSealed bool          `json:"isSealed"`
}

// BackupApp is an app's approved source and variables.
type BackupApp struct {
	App       string           `json:"app"`
	Source    AppSource        `json:"source"`
	Variables []BackupVariable `json:"variables"`
}

type backupContents struct {
	Format int         `json:"format"`
	Apps   []BackupApp `json:"apps"`
}

// ExportedVariables is what root gets from an export: the sealed variables,
// and the names it left out because their master key was lost.
type ExportedVariables struct {
	Sealed  []byte   `json:"sealed"`
	Skipped []string `json:"skipped"`
}

// RestoredVariables names what an import stored, without values.
type RestoredVariables struct {
	Apps []AppVariablesSummary `json:"apps"`
}

type (
	backupExportArgs struct {
		Recipient string `json:"recipient"`
	}
	backupImportArgs struct {
		Secret string `json:"secret"`
		Sealed []byte `json:"sealed"`
	}
)

// ExportBackup seals every app's approved source and variables to the
// recovery key's recipient. Root only.
func (s *Service) ExportBackup(ctx context.Context, recipient string) (ExportedVariables, error) {
	apps, skipped, err := s.backupApps(ctx)
	if err != nil {
		return ExportedVariables{}, err
	}

	plaintext, err := json.Marshal(backupContents{Format: backupFormat, Apps: apps})
	if err != nil {
		return ExportedVariables{}, err
	}
	defer clear(plaintext)

	sealed, err := recovery.Seal(recipient, plaintext)
	if err != nil {
		return ExportedVariables{}, err
	}
	if err := s.recordOwn(ctx, EventBackupExported, ""); err != nil {
		return ExportedVariables{}, err
	}

	return ExportedVariables{Sealed: sealed, Skipped: skipped}, nil
}

func (s *Service) backupApps(ctx context.Context) ([]BackupApp, []string, error) {
	summary, err := s.VariablesSummary(ctx)
	if err != nil {
		return nil, nil, err
	}

	apps := []BackupApp{}
	skipped := []string{}
	for _, listed := range summary {
		approved, err := loadApproval(ctx, s.db, listed.App)
		if err != nil {
			return nil, nil, err
		}
		stored, err := loadVariables(ctx, s.db, listed.App)
		if err != nil {
			return nil, nil, err
		}

		app := BackupApp{App: listed.App, Source: approved.source, Variables: []BackupVariable{}}
		for _, variable := range listed.Variables {
			value, err := s.keys.open(variablePurpose(listed.App, variable.Name), stored[variable.Name].value)
			if errors.Is(err, errNoKey) {
				skipped = append(skipped, variableSubject(listed.App, variable.Name))
				continue
			}
			if err != nil {
				return nil, nil, err
			}

			current := stored[variable.Name]
			app.Variables = append(app.Variables, BackupVariable{
				Name: variable.Name, Value: string(value), Scope: current.scope, IsSealed: current.isSealed,
			})
		}
		apps = append(apps, app)
	}

	return apps, skipped, nil
}

// ImportBackup opens a backup with the recovery key and stores its sources
// and variables, sealed with this server's master key. It refuses unless the
// server has no variables and no approved sources yet, and stores everything
// or nothing. When the server already holds exactly what the backup holds,
// from a restore that stopped after this step, it succeeds without storing
// anything, so the restore can be run again. Root only.
func (s *Service) ImportBackup(ctx context.Context, secret string, sealed []byte) (RestoredVariables, error) {
	plaintext, err := recovery.Open(secret, sealed)
	if err != nil {
		return RestoredVariables{}, err
	}
	defer clear(plaintext)

	var contents backupContents
	if err := json.Unmarshal(plaintext, &contents); err != nil || contents.Format != backupFormat {
		return RestoredVariables{}, ErrInvalidBackup
	}
	if err := checkBackup(contents.Apps); err != nil {
		return RestoredVariables{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RestoredVariables{}, err
	}
	defer tx.Rollback()

	isEmpty, err := hasNoVariables(ctx, tx)
	if err != nil {
		return RestoredVariables{}, err
	}
	if !isEmpty {
		tx.Rollback()
		return s.matchRestored(ctx, contents.Apps)
	}

	restored, err := s.storeBackup(ctx, tx, contents.Apps)
	if err != nil {
		return RestoredVariables{}, err
	}
	event := normalizeEvent(Event{At: s.now(), Type: EventBackupRestored, Source: SourceKeys, Detail: restoredNames(restored)})
	if err := insertEvent(ctx, tx, event); err != nil {
		return RestoredVariables{}, err
	}
	if err := tx.Commit(); err != nil {
		return RestoredVariables{}, err
	}

	s.writeJournal(event)
	return restored, nil
}

// matchRestored succeeds when the stored sources and variables are exactly
// the backup's, and refuses otherwise.
func (s *Service) matchRestored(ctx context.Context, apps []BackupApp) (RestoredVariables, error) {
	stored, skipped, err := s.backupApps(ctx)
	if err != nil {
		return RestoredVariables{}, err
	}
	if len(skipped) > 0 || !sameApps(stored, apps) {
		return RestoredVariables{}, ErrNotEmpty
	}

	return summarize(apps), nil
}

func sameApps(stored, backedUp []BackupApp) bool {
	if len(stored) != len(backedUp) {
		return false
	}

	byName := map[string]BackupApp{}
	for _, app := range stored {
		byName[app.App] = app
	}
	for _, app := range backedUp {
		current, exists := byName[app.App]
		if !exists || current.Source != app.Source || !sameVariables(current.Variables, app.Variables) {
			return false
		}
	}

	return true
}

func sameVariables(stored, backedUp []BackupVariable) bool {
	if len(stored) != len(backedUp) {
		return false
	}

	byName := map[string]BackupVariable{}
	for _, variable := range stored {
		byName[variable.Name] = variable
	}
	for _, variable := range backedUp {
		if byName[variable.Name] != variable {
			return false
		}
	}

	return true
}

func summarize(apps []BackupApp) RestoredVariables {
	restored := RestoredVariables{Apps: []AppVariablesSummary{}}
	for _, app := range apps {
		summary := AppVariablesSummary{App: app.App, Source: app.Source, Variables: []SummarizedVariable{}}
		for _, variable := range app.Variables {
			summary.Variables = append(summary.Variables, SummarizedVariable{Name: variable.Name, IsSealed: variable.IsSealed})
		}
		restored.Apps = append(restored.Apps, summary)
	}

	return restored
}

// checkBackup applies the rules a save does, so a crafted backup stores
// nothing the dashboard couldn't have.
func checkBackup(apps []BackupApp) error {
	seenApps := map[string]bool{}
	for _, app := range apps {
		if !appNamePattern.MatchString(app.App) || seenApps[app.App] || !app.Source.isValid() || app.Source.RepositoryID < 0 {
			return ErrInvalidBackup
		}
		seenApps[app.App] = true

		stored := map[string]storedVariable{}
		for _, variable := range app.Variables {
			isValidName := variableNamePattern.MatchString(variable.Name) && len(variable.Name) <= maxVariableNameBytes &&
				variable.Name != reservedVariable
			_, isDuplicate := stored[variable.Name]
			change := VariableChange{Name: variable.Name, Value: &variable.Value, Scope: variable.Scope, IsSealed: variable.IsSealed}
			if !isValidName || isDuplicate || checkVariableChange(false, change) != nil {
				return ErrInvalidBackup
			}
			stored[variable.Name] = storedVariable{valueBytes: len(variable.Value)}
		}
		if checkAppLimits(stored) != nil {
			return ErrInvalidBackup
		}
	}

	return nil
}

func hasNoVariables(ctx context.Context, tx *sql.Tx) (bool, error) {
	var count int
	err := tx.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM app_variables) + (SELECT count(*) FROM app_sources)").Scan(&count)
	return count == 0, err
}

// storeBackup keeps each source's pinned repository id, so the first deploy
// on the new server is refused if GitHub has another repository at the URL.
func (s *Service) storeBackup(ctx context.Context, tx *sql.Tx, apps []BackupApp) (RestoredVariables, error) {
	now := s.now().Unix()
	restored := RestoredVariables{Apps: []AppVariablesSummary{}}
	for _, app := range apps {
		_, err := tx.ExecContext(ctx,
			"INSERT INTO app_sources (app, repository, branch, root_directory, repository_id, approved_at) VALUES (?, ?, ?, ?, ?, ?)",
			app.App, app.Source.Repository, app.Source.Branch, app.Source.RootDirectory, app.Source.RepositoryID, now)
		if err != nil {
			return RestoredVariables{}, err
		}

		summary := AppVariablesSummary{App: app.App, Source: app.Source, Variables: []SummarizedVariable{}}
		for _, variable := range app.Variables {
			value, err := s.keys.seal(variablePurpose(app.App, variable.Name), []byte(variable.Value))
			if err != nil {
				return RestoredVariables{}, err
			}
			_, err = tx.ExecContext(ctx, `
				INSERT INTO app_variables (app, name, key_version, value, value_bytes, scope, is_sealed, replaced_at)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
				app.App, variable.Name, value.version, value.ciphertext, len(variable.Value), string(variable.Scope), variable.IsSealed, now)
			if err != nil {
				return RestoredVariables{}, err
			}
			summary.Variables = append(summary.Variables, SummarizedVariable{Name: variable.Name, IsSealed: variable.IsSealed})
		}
		restored.Apps = append(restored.Apps, summary)
	}

	return restored, nil
}

// restoredNames lists a restore's apps for the security log.
func restoredNames(restored RestoredVariables) string {
	names := make([]string, 0, len(restored.Apps))
	for _, app := range restored.Apps {
		names = append(names, app.App)
	}

	return strings.Join(names, ", ")
}
