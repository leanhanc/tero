package keys

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Apps' environment variables are sealed here. The dashboard reads unsealed
// values for a live session and never a sealed one. Only the agent gets
// sealed values, and only for the source the admin approved, so a compromised
// dashboard can't obtain them by pointing an app at code it controls.

var (
	ErrInvalidVariable   = errors.New("a variable's name or value isn't valid")
	ErrVariableSealed    = errors.New("a sealed variable can only be replaced or deleted")
	ErrTooManyVariables  = errors.New("this app has reached its limit of variables")
	ErrSourceNotApproved = errors.New("this app's source isn't the one approved for its variables")
)

const (
	maxVariableNameBytes  = 256
	maxVariableValueBytes = 32 << 10
	maxVariablesPerApp    = 200
	maxAppValueBytes      = 256 << 10
	maxSourceFieldBytes   = 1024
	maxListedReads        = 100
	// lostKeyVersion marks a value whose master key is gone. No key has this
	// version, so opening it says the key isn't loaded.
	lostKeyVersion = 0
	// reservedVariable is set by Tero for every app.
	reservedVariable = "PORT"
	// railpackPrefix names variables Railpack reads as build configuration
	// and can copy into its plan, so their values can't be kept sealed.
	railpackPrefix = "RAILPACK_"
	// misePrefix names variables Railpack passes to mise in its plan.
	misePrefix = "MISE_"
)

// copiedByRailpack are other variables whose values Railpack can write into
// its plan, so they can't be kept sealed either.
var copiedByRailpack = map[string]bool{"TURBO_CACHE_DIR": true}

var (
	variableNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	// appNamePattern is the app naming rule, so a name can't forge a purpose
	// or an event's subject.
	appNamePattern = regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,28}[a-z0-9])?$`)
)

// VariableScope says which phases of a deploy get a variable.
type VariableScope string

const (
	ScopeRuntime VariableScope = "runtime"
	ScopeBuild   VariableScope = "build"
	ScopeBoth    VariableScope = "both"
)

func (scope VariableScope) isValid() bool {
	return scope == ScopeRuntime || scope == ScopeBuild || scope == ScopeBoth
}

func (scope VariableScope) reachesBuild() bool {
	return scope == ScopeBuild || scope == ScopeBoth
}

// DeployPhase is the part of a deploy values are released for.
type DeployPhase string

const (
	PhaseBuild DeployPhase = "build"
	PhaseRun   DeployPhase = "run"
)

func (phase DeployPhase) includes(scope VariableScope) bool {
	if phase == PhaseBuild {
		return scope.reachesBuild()
	}

	return scope == ScopeRuntime || scope == ScopeBoth
}

// AppSource is what an app builds from.
type AppSource struct {
	Repository    string `json:"repository"`
	Branch        string `json:"branch"`
	RootDirectory string `json:"rootDirectory"`
}

func (source AppSource) isValid() bool {
	return source.Repository != "" && len(source.Repository) <= maxSourceFieldBytes &&
		source.Branch != "" && len(source.Branch) <= maxSourceFieldBytes &&
		len(source.RootDirectory) <= maxSourceFieldBytes
}

// Variable is a variable as the dashboard sees it. Value is empty for a
// sealed variable, and for one sealed with a master key that was lost, which
// the admin has to set again.
type Variable struct {
	Name         string        `json:"name"`
	Value        string        `json:"value,omitempty"`
	Scope        VariableScope `json:"scope"`
	IsSealed     bool          `json:"isSealed"`
	IsUnreadable bool          `json:"isUnreadable,omitempty"`
	ReplacedAt   int64         `json:"replacedAt"`
}

// AppVariables is an app's list and approved source, which is empty until
// its first variable is saved.
type AppVariables struct {
	Variables []Variable `json:"variables"`
	Source    AppSource  `json:"source"`
}

// VariableChange is one change in a save: removing the variable, or setting
// it. A nil Value keeps the stored one.
type VariableChange struct {
	Name      string        `json:"name"`
	IsRemoved bool          `json:"isRemoved,omitempty"`
	Value     *string       `json:"value,omitempty"`
	Scope     VariableScope `json:"scope,omitempty"`
	IsSealed  bool          `json:"isSealed,omitempty"`
}

// ChangedVariable says what a redeploy needs for one changed variable.
type ChangedVariable struct {
	Name         string `json:"name"`
	TouchesBuild bool   `json:"touchesBuild"`
}

// SavedVariables is what a save changed and how many variables the app has
// after it.
type SavedVariables struct {
	Changed []ChangedVariable `json:"changed"`
	Count   int               `json:"count"`
}

// VariableRead is one release of a sealed variable.
type VariableRead struct {
	At       int64       `json:"at"`
	Phase    DeployPhase `json:"phase"`
	DeployID int64       `json:"deployId"`
}

// ReleaseRequest asks for an app's values for one phase of a deploy. Source
// is what the agent is about to fetch or run.
type ReleaseRequest struct {
	App      string      `json:"app"`
	Source   AppSource   `json:"source"`
	Phase    DeployPhase `json:"phase"`
	DeployID int64       `json:"deployId"`
}

// ReleasedVariable is a value on its way to a build or a container.
type ReleasedVariable struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	IsSealed bool   `json:"isSealed"`
}

// SummarizedVariable names a variable without its value.
type SummarizedVariable struct {
	Name     string `json:"name"`
	IsSealed bool   `json:"isSealed"`
}

// AppVariablesSummary is what root sees of an app's variables.
type AppVariablesSummary struct {
	App       string               `json:"app"`
	Source    AppSource            `json:"source"`
	Variables []SummarizedVariable `json:"variables"`
}

// storedVariable is a row of app_variables.
type storedVariable struct {
	name       string
	value      sealed
	valueBytes int
	scope      VariableScope
	isSealed   bool
	replacedAt int64
}

func variablePurpose(app, name string) string {
	return "app_variables.value." + app + "." + name
}

func variableSubject(app, name string) string {
	return app + "/" + name
}

// ListVariables returns an app's variables for a live session, with the
// values of unsealed ones only (OWASP ASVS v5.0.0-13.3.2).
func (s *Service) ListVariables(ctx context.Context, token, app string) (AppVariables, error) {
	if _, err := s.liveSession(ctx, token); err != nil {
		return AppVariables{}, err
	}
	if !appNamePattern.MatchString(app) {
		return AppVariables{}, ErrRefused
	}

	stored, err := loadVariables(ctx, s.db, app)
	if err != nil {
		return AppVariables{}, err
	}

	result := AppVariables{Variables: make([]Variable, 0, len(stored))}
	for _, name := range slices.Sorted(maps.Keys(stored)) {
		variable := stored[name]
		shown := Variable{Name: name, Scope: variable.scope, IsSealed: variable.isSealed, ReplacedAt: variable.replacedAt}
		_, isKeyLoaded := s.keys[variable.value.version]
		if variable.isSealed {
			shown.IsUnreadable = !isKeyLoaded
		} else {
			plaintext, err := s.openSealed(variablePurpose(app, name), variable.value)
			switch {
			case errors.Is(err, ErrKeyNotLoaded):
				shown.IsUnreadable = true
			case err != nil:
				return AppVariables{}, err
			}
			shown.Value = string(plaintext)
		}
		result.Variables = append(result.Variables, shown)
	}

	source, _, err := loadSource(ctx, s.db, app)
	if err != nil {
		return AppVariables{}, err
	}
	result.Source = source

	return result, nil
}

// SaveVariables applies changes to an app's variables, all or none. On an app
// that has or would gain a sealed variable, any change needs a confirmed
// session: an unsealed NODE_OPTIONS or API base URL can make the app hand over
// the sealed values. The first save records source as the app's approved one.
func (s *Service) SaveVariables(ctx context.Context, token, ip, app string, source AppSource, changes []VariableChange) (SavedVariables, error) {
	session, err := s.liveSession(ctx, token)
	if err != nil {
		return SavedVariables{}, err
	}
	if !appNamePattern.MatchString(app) || !source.isValid() || len(changes) > 2*maxVariablesPerApp {
		return SavedVariables{}, ErrRefused
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SavedVariables{}, err
	}
	defer tx.Rollback()

	stored, err := loadVariables(ctx, tx, app)
	if err != nil {
		return SavedVariables{}, err
	}
	if needsConfirmation(stored, changes) && !s.isConfirmed(session) {
		return SavedVariables{}, ErrNotConfirmed
	}

	hadSealed := hasSealed(stored)
	saved, events, err := s.applyChanges(ctx, tx, app, stored, changes)
	if err != nil {
		return SavedVariables{}, err
	}

	sourceEvents, err := s.keepSource(ctx, tx, app, source, saved.Count, !hadSealed && hasSealed(stored))
	if err != nil {
		return SavedVariables{}, err
	}
	events = append(events, sourceEvents...)

	// Recorded in the same transaction, so a change is never stored without its
	// event.
	for i := range events {
		events[i].At, events[i].Source, events[i].IP = s.now(), SourceKeys, ip
		events[i] = normalizeEvent(events[i])
		if err := insertEvent(ctx, tx, events[i]); err != nil {
			return SavedVariables{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return SavedVariables{}, err
	}
	for _, event := range events {
		s.writeJournal(event)
	}

	return saved, nil
}

func hasSealed(stored map[string]storedVariable) bool {
	for _, variable := range stored {
		if variable.isSealed {
			return true
		}
	}

	return false
}

// keepSource maintains the app's approved source after a save. It goes with
// the app's last variable, so an old approval can't wait for later values. The
// first seal pins the current source: sealing needs a confirmed session, so an
// approval recorded earlier on a live session alone doesn't carry over to
// sealed values.
func (s *Service) keepSource(ctx context.Context, tx *sql.Tx, app string, source AppSource, count int, isFirstSeal bool) ([]Event, error) {
	if count == 0 {
		_, err := tx.ExecContext(ctx, "DELETE FROM app_sources WHERE app = ?", app)
		return nil, err
	}
	if !isFirstSeal {
		return nil, insertSourceIfMissing(ctx, tx, app, source, s.now().Unix())
	}

	if err := upsertSource(ctx, tx, app, source, s.now().Unix()); err != nil {
		return nil, err
	}
	return []Event{{Type: EventSourceApproved, Subject: app}}, nil
}

func needsConfirmation(stored map[string]storedVariable, changes []VariableChange) bool {
	if hasSealed(stored) {
		return true
	}
	for _, change := range changes {
		if change.IsSealed {
			return true
		}
	}

	return false
}

// applyChanges writes changes in tx, and returns what changed with the events
// to record once it commits. The app's limits are checked on the result.
func (s *Service) applyChanges(ctx context.Context, tx *sql.Tx, app string, stored map[string]storedVariable, changes []VariableChange) (SavedVariables, []Event, error) {
	saved := SavedVariables{Changed: []ChangedVariable{}}
	events := []Event{}

	for _, change := range changes {
		if !variableNamePattern.MatchString(change.Name) || len(change.Name) > maxVariableNameBytes || change.Name == reservedVariable {
			return SavedVariables{}, nil, ErrInvalidVariable
		}

		current, exists := stored[change.Name]
		subject := variableSubject(app, change.Name)
		if change.IsRemoved {
			if !exists {
				continue
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM app_variables WHERE app = ? AND name = ?", app, change.Name); err != nil {
				return SavedVariables{}, nil, err
			}
			delete(stored, change.Name)
			saved.Changed = append(saved.Changed, ChangedVariable{Name: change.Name, TouchesBuild: current.scope.reachesBuild()})
			events = append(events, Event{Type: EventVariablesChanged, Subject: subject})
			continue
		}

		next, changed, changeEvents, err := s.applySet(ctx, tx, app, current, exists, change)
		if err != nil {
			return SavedVariables{}, nil, err
		}
		stored[change.Name] = next
		if changed {
			touchesBuild := next.scope.reachesBuild() || (exists && current.scope.reachesBuild())
			saved.Changed = append(saved.Changed, ChangedVariable{Name: change.Name, TouchesBuild: touchesBuild})
		}
		events = append(events, changeEvents...)
	}

	if err := checkAppLimits(stored); err != nil {
		return SavedVariables{}, nil, err
	}
	saved.Count = len(stored)

	return saved, events, nil
}

// applySet creates or updates one variable. Changing nothing but the sealed
// flag doesn't change what the app runs with, so it isn't reported as changed.
func (s *Service) applySet(ctx context.Context, tx *sql.Tx, app string, current storedVariable, exists bool, change VariableChange) (storedVariable, bool, []Event, error) {
	isUnsealing := exists && current.isSealed && !change.IsSealed
	if isUnsealing {
		return storedVariable{}, false, nil, ErrVariableSealed
	}
	if err := checkVariableChange(exists, change); err != nil {
		return storedVariable{}, false, nil, err
	}

	subject := variableSubject(app, change.Name)
	next := current
	next.name = change.Name
	next.scope = change.Scope
	next.isSealed = change.IsSealed
	events := []Event{}

	if change.Value != nil {
		value, err := s.keys.seal(variablePurpose(app, change.Name), []byte(*change.Value))
		if err != nil {
			return storedVariable{}, false, nil, err
		}
		next.value = value
		next.valueBytes = len(*change.Value)
		next.replacedAt = s.now().Unix()
	}

	_, err := tx.ExecContext(ctx, `
		INSERT INTO app_variables (app, name, key_version, value, value_bytes, scope, is_sealed, replaced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (app, name) DO UPDATE SET key_version = excluded.key_version, value = excluded.value,
			value_bytes = excluded.value_bytes, scope = excluded.scope, is_sealed = excluded.is_sealed,
			replaced_at = excluded.replaced_at`,
		app, next.name, next.value.version, next.value.ciphertext, next.valueBytes, string(next.scope), next.isSealed, next.replacedAt)
	if err != nil {
		return storedVariable{}, false, nil, err
	}

	hasNewValue := change.Value != nil
	hasNewScope := exists && current.scope != change.Scope
	isNewlySealed := change.IsSealed && (!exists || !current.isSealed)
	isReplacingSealed := exists && current.isSealed && hasNewValue
	if isNewlySealed {
		events = append(events, Event{Type: EventVariableSealed, Subject: subject})
	}
	if isReplacingSealed {
		events = append(events, Event{Type: EventVariableReplaced, Subject: subject})
	}
	changed := hasNewValue || hasNewScope
	if changed && !isReplacingSealed && !isNewlySealed {
		events = append(events, Event{Type: EventVariablesChanged, Subject: subject})
	}

	return next, changed, events, nil
}

func checkVariableChange(exists bool, change VariableChange) error {
	isCopiedIntoPlan := strings.HasPrefix(change.Name, railpackPrefix) || strings.HasPrefix(change.Name, misePrefix) ||
		copiedByRailpack[change.Name]
	isSealingRailpack := change.IsSealed && isCopiedIntoPlan
	hasNoValue := !exists && change.Value == nil
	if !change.Scope.isValid() || isSealingRailpack || hasNoValue {
		return ErrInvalidVariable
	}
	if change.Value == nil {
		return nil
	}

	if len(*change.Value) > maxVariableValueBytes || strings.ContainsRune(*change.Value, 0) {
		return ErrInvalidVariable
	}

	return nil
}

func checkAppLimits(stored map[string]storedVariable) error {
	total := 0
	for _, variable := range stored {
		total += variable.valueBytes
	}
	if len(stored) > maxVariablesPerApp || total > maxAppValueBytes {
		return ErrTooManyVariables
	}

	return nil
}

// ReleaseVariables returns an app's values for one phase of a deploy, only
// when the source matches the approved one exactly. GitHub's repository id
// isn't compared: a row can pair an approved id with another repository's
// URL, and the URL is what gets cloned. Every released sealed value is
// recorded.
func (s *Service) ReleaseVariables(ctx context.Context, release ReleaseRequest) ([]ReleasedVariable, error) {
	isValidPhase := release.Phase == PhaseBuild || release.Phase == PhaseRun
	if !appNamePattern.MatchString(release.App) || !isValidPhase || release.DeployID <= 0 {
		return nil, ErrRefused
	}

	stored, err := loadVariables(ctx, s.db, release.App)
	if err != nil {
		return nil, err
	}
	released := []ReleasedVariable{}
	if len(stored) == 0 {
		return released, nil
	}

	approved, hasApproved, err := loadSource(ctx, s.db, release.App)
	if err != nil {
		return nil, err
	}
	if !hasApproved || approved != release.Source {
		event := Event{At: s.now(), Type: EventVariablesRefused, Source: SourceKeys, Subject: release.App,
			Detail: "deploy " + strconv.FormatInt(release.DeployID, 10)}
		return nil, errors.Join(ErrSourceNotApproved, s.record(ctx, event))
	}

	detail := fmt.Sprintf("%s:%d", release.Phase, release.DeployID)
	events := []Event{}
	for _, name := range slices.Sorted(maps.Keys(stored)) {
		variable := stored[name]
		if !release.Phase.includes(variable.scope) {
			continue
		}

		plaintext, err := s.openSealed(variablePurpose(release.App, name), variable.value)
		if err != nil {
			return nil, err
		}
		released = append(released, ReleasedVariable{Name: name, Value: string(plaintext), IsSealed: variable.isSealed})
		if variable.isSealed {
			events = append(events, Event{At: s.now(), Type: EventSealedVariableReleased, Source: SourceKeys,
				Subject: variableSubject(release.App, name), Detail: detail})
		}
	}

	// The record comes before the values leave: an unrecorded release doesn't
	// happen.
	for _, event := range events {
		if err := s.record(ctx, event); err != nil {
			return nil, err
		}
	}

	return released, nil
}

// VariableReads lists the latest releases of one variable, newest first.
func (s *Service) VariableReads(ctx context.Context, token, app, name string) ([]VariableRead, error) {
	if _, err := s.liveSession(ctx, token); err != nil {
		return nil, err
	}
	if !appNamePattern.MatchString(app) || !variableNamePattern.MatchString(name) {
		return nil, ErrRefused
	}

	rows, err := s.db.QueryContext(ctx,
		"SELECT at, detail FROM security_events WHERE type = ? AND subject = ? ORDER BY id DESC LIMIT ?",
		string(EventSealedVariableReleased), variableSubject(app, name), maxListedReads)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	reads := []VariableRead{}
	for rows.Next() {
		var read VariableRead
		var detail string
		if err := rows.Scan(&read.At, &detail); err != nil {
			return nil, err
		}
		phase, deployID, _ := strings.Cut(detail, ":")
		read.Phase = DeployPhase(phase)
		read.DeployID, _ = strconv.ParseInt(deployID, 10, 64)
		reads = append(reads, read)
	}

	return reads, rows.Err()
}

// ApproveSource makes source the one an app's values are released for, on a
// confirmed session.
func (s *Service) ApproveSource(ctx context.Context, token, ip, app string, source AppSource) error {
	if _, err := s.confirmedSession(ctx, token); err != nil {
		return err
	}
	if !appNamePattern.MatchString(app) || !source.isValid() {
		return ErrRefused
	}

	// An app without variables has nothing to approve, and an approval mustn't
	// wait for values saved later.
	stored, err := loadVariables(ctx, s.db, app)
	if err != nil {
		return err
	}
	if len(stored) == 0 {
		return ErrRefused
	}

	if err := upsertSource(ctx, s.db, app, source, s.now().Unix()); err != nil {
		return err
	}

	return s.record(ctx, Event{At: s.now(), Type: EventSourceApproved, Source: SourceKeys, IP: ip, Subject: app})
}

// VariablesSummary lists every app with variables, for root: names and
// approved sources, never values.
func (s *Service) VariablesSummary(ctx context.Context) ([]AppVariablesSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT v.app, v.name, v.is_sealed, coalesce(a.repository, ''), coalesce(a.branch, ''), coalesce(a.root_directory, '')
		FROM app_variables v LEFT JOIN app_sources a ON a.app = v.app ORDER BY v.app, v.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summary := []AppVariablesSummary{}
	for rows.Next() {
		var app string
		var source AppSource
		var variable SummarizedVariable
		if err := rows.Scan(&app, &variable.Name, &variable.IsSealed, &source.Repository, &source.Branch, &source.RootDirectory); err != nil {
			return nil, err
		}
		if len(summary) == 0 || summary[len(summary)-1].App != app {
			summary = append(summary, AppVariablesSummary{App: app, Source: source})
		}
		last := &summary[len(summary)-1]
		last.Variables = append(last.Variables, variable)
	}

	return summary, rows.Err()
}

// MarkLostVariables records that values sealed with a master key that isn't
// loaded are gone, so `sudo tero assume-breach` can replace the key. Their rows
// stay: the dashboard lists them as needing to be set again, and a release fails
// rather than starting an app without them. Root only.
func (s *Service) MarkLostVariables(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, "SELECT DISTINCT key_version FROM app_variables WHERE key_version != ?", lostKeyVersion)
	if err != nil {
		return err
	}
	defer rows.Close()

	var versions []int
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return err
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, version := range versions {
		if _, isLoaded := s.keys[version]; isLoaded {
			continue
		}
		_, err := s.db.ExecContext(ctx, "UPDATE app_variables SET key_version = ?, value = x'' WHERE key_version = ?", lostKeyVersion, version)
		if err != nil {
			return err
		}
	}

	return nil
}

// resealVariables re-seals every value under the newest key, except those
// already marked as lost.
func (s *Service) resealVariables(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT app, name, key_version, value FROM app_variables WHERE key_version != ?", lostKeyVersion)
	if err != nil {
		return err
	}

	type row struct {
		app, name string
		value     sealed
	}
	var variables []row
	for rows.Next() {
		var variable row
		if err := rows.Scan(&variable.app, &variable.name, &variable.value.version, &variable.value.ciphertext); err != nil {
			rows.Close()
			return err
		}
		variables = append(variables, variable)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, variable := range variables {
		resealed, err := s.reseal(variablePurpose(variable.app, variable.name), variable.value)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE app_variables SET key_version = ?, value = ? WHERE app = ? AND name = ?",
			resealed.version, resealed.ciphertext, variable.app, variable.name)
		if err != nil {
			return err
		}
	}

	return nil
}

// variableReader is what reading variables needs from a database or a
// transaction.
type variableReader interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func loadVariables(ctx context.Context, db variableReader, app string) (map[string]storedVariable, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT name, key_version, value, value_bytes, scope, is_sealed, replaced_at FROM app_variables WHERE app = ?", app)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	variables := map[string]storedVariable{}
	for rows.Next() {
		var variable storedVariable
		err := rows.Scan(&variable.name, &variable.value.version, &variable.value.ciphertext, &variable.valueBytes,
			&variable.scope, &variable.isSealed, &variable.replacedAt)
		if err != nil {
			return nil, err
		}
		variables[variable.name] = variable
	}

	return variables, rows.Err()
}

func loadSource(ctx context.Context, db variableReader, app string) (AppSource, bool, error) {
	var source AppSource
	err := db.QueryRowContext(ctx, "SELECT repository, branch, root_directory FROM app_sources WHERE app = ?", app).
		Scan(&source.Repository, &source.Branch, &source.RootDirectory)
	if errors.Is(err, sql.ErrNoRows) {
		return AppSource{}, false, nil
	}

	return source, err == nil, err
}

func upsertSource(ctx context.Context, db eventWriter, app string, source AppSource, now int64) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO app_sources (app, repository, branch, root_directory, approved_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (app) DO UPDATE SET repository = excluded.repository, branch = excluded.branch,
			root_directory = excluded.root_directory, approved_at = excluded.approved_at`,
		app, source.Repository, source.Branch, source.RootDirectory, now)
	return err
}

func insertSourceIfMissing(ctx context.Context, tx *sql.Tx, app string, source AppSource, now int64) error {
	_, err := tx.ExecContext(ctx,
		"INSERT OR IGNORE INTO app_sources (app, repository, branch, root_directory, approved_at) VALUES (?, ?, ?, ?, ?)",
		app, source.Repository, source.Branch, source.RootDirectory, now)
	return err
}
