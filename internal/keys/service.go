// Package keys is the key service, `tero keys`: the only process that holds
// Tero's master key. It runs as its own system user, tero-keys, with no
// network, and answers the dashboard and root over a Unix socket. It keeps
// the admin's password hash, the TOTP secret (sealed with the master key) and
// the setup links, checks every login, and keeps the security log, so
// nothing running as tero can read the key, read or replace the admin's
// credentials, or change a recorded event.
package keys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"slices"
	"time"

	"github.com/leanhanc/tero/internal/password"
	"github.com/leanhanc/tero/internal/totp"
)

// DatabasePath is the key service's database inside its StateDirectory.
const DatabasePath = "/var/lib/tero-keys/keys.db"

//go:embed migrations/*.sql
var Migrations embed.FS

// Errors a request can end with. They cross the socket as codes; see
// protocol.go.
var (
	ErrRefused       = errors.New("the key service refused this request")
	ErrUnavailable   = errors.New("the key service isn't running")
	ErrInvalidLink   = errors.New("the setup link is invalid, already used or expired")
	ErrAlreadySetUp  = errors.New("the dashboard login is already set up")
	ErrNotSetUp      = errors.New("the dashboard login isn't set up")
	ErrInvalidCode   = errors.New("the TOTP code didn't match")
	ErrWrongLogin    = errors.New("wrong password or code")
	ErrLocked        = errors.New("login is locked after too many wrong codes")
	ErrKeyNotLoaded  = errors.New("a value is sealed with a master key that isn't loaded")
	ErrHashingIsBusy = errors.New("the key service is busy")
)

const (
	// setupLinkLifetime bounds how long a printed link works (OWASP ASVS
	// v5.0.0-6.4.1).
	setupLinkLifetime = 24 * time.Hour
	// codeFailureLimit wrong codes with the right password within
	// codeFailureWindow lock login for codeLockout. Wrong passwords don't
	// count, so strangers can't lock the admin out; the dashboard throttles
	// those.
	codeFailureLimit  = 20
	codeFailureWindow = 24 * time.Hour
	codeLockout       = 24 * time.Hour
	// maxPasswordBytes is a backstop: the dashboard's policy allows 1,024
	// characters, at most four bytes each.
	maxPasswordBytes = 4096
)

// Service is the key service's state for one server.
type Service struct {
	db      *sql.DB
	keys    keyring
	now     func() time.Time
	journal JournalWriter
	limits  limits
}

// Options configure a Service. Now and Journal default to the real clock and
// no journal; tests replace them.
type Options struct {
	Now     func() time.Time
	Journal JournalWriter
}

// NewService returns a Service backed by db, which must already be migrated,
// holding the master key versions in keys.
func NewService(db *sql.DB, keys map[int][]byte, opts Options) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	return &Service{db: db, keys: keys, now: now, journal: opts.Journal}
}

// Enrolment is the new TOTP secret, returned once so the dashboard can show
// it as a QR code.
type Enrolment struct {
	Secret string `json:"secret"`
}

// IssueSetupLink replaces any unused setup link with a new one and returns
// its token. Only root can ask for one.
func (s *Service) IssueSetupLink(ctx context.Context) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	now := s.now()
	if _, err := tx.ExecContext(ctx, "DELETE FROM setup_links WHERE used_at IS NULL"); err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO setup_links (token_hash, issued_at, expires_at) VALUES (?, ?, ?)",
		hashToken(token), now.Unix(), now.Add(setupLinkLifetime).Unix())
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}

	return token, s.recordOwn(ctx, EventSetupLinkIssued, "")
}

// BeginSetup hashes the chosen password and creates a TOTP secret, both kept
// pending on the link until FinishSetup confirms a code. The dashboard has
// already checked the password against the length, blocklist and breach
// rules, which need the network this service doesn't have.
func (s *Service) BeginSetup(ctx context.Context, token, newPassword string) (Enrolment, error) {
	if _, err := s.usableLink(ctx, token); err != nil {
		return Enrolment{}, err
	}
	if len(newPassword) == 0 || len(newPassword) > maxPasswordBytes {
		return Enrolment{}, ErrRefused
	}

	passwordHash, err := password.Hash(ctx, newPassword)
	if errors.Is(err, password.ErrBusy) {
		return Enrolment{}, ErrHashingIsBusy
	}
	if err != nil {
		return Enrolment{}, err
	}
	secret, err := totp.NewSecret()
	if err != nil {
		return Enrolment{}, err
	}

	tokenHash := hashToken(token)
	sealedSecret, err := s.keys.seal(pendingSecretPurpose(tokenHash), []byte(secret))
	if err != nil {
		return Enrolment{}, err
	}

	updated, err := s.db.ExecContext(ctx, `
		UPDATE setup_links SET pending_password_hash = ?, pending_totp_key_version = ?, pending_totp_secret = ?
		WHERE token_hash = ? AND used_at IS NULL`,
		passwordHash, sealedSecret.version, sealedSecret.ciphertext, tokenHash)
	if err != nil {
		return Enrolment{}, err
	}
	// The link was used up while the password hashed.
	if count, _ := updated.RowsAffected(); count != 1 {
		return Enrolment{}, ErrInvalidLink
	}

	return Enrolment{Secret: secret}, nil
}

// FinishSetup checks a code against the pending secret and, in one
// transaction, creates the admin and uses up the link.
func (s *Service) FinishSetup(ctx context.Context, ip, token, code string) error {
	link, err := s.usableLink(ctx, token)
	if err != nil {
		return err
	}
	if !link.passwordHash.Valid || link.secret == nil {
		return ErrInvalidLink
	}

	tokenHash := hashToken(token)
	secret, err := s.keys.open(pendingSecretPurpose(tokenHash), *link.secret)
	if err != nil {
		return err
	}

	step, isValid := totp.Verify(string(secret), code, s.now(), 0)
	if !isValid {
		if err := s.recordOwn(ctx, EventSetupCodeFailed, ip); err != nil {
			return err
		}
		return ErrInvalidCode
	}

	// The admin row gets its own seal, tied to the admin row.
	sealedSecret, err := s.keys.seal(adminSecretPurpose, secret)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := s.now().Unix()
	inserted, err := tx.ExecContext(ctx, `
		INSERT INTO admin (id, password_hash, totp_key_version, totp_secret, last_totp_step, created_at)
		VALUES (1, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO NOTHING`,
		link.passwordHash.String, sealedSecret.version, sealedSecret.ciphertext, step, now)
	if err != nil {
		return err
	}
	if count, _ := inserted.RowsAffected(); count != 1 {
		return ErrAlreadySetUp
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE setup_links SET used_at = ?, pending_password_hash = NULL, pending_totp_key_version = NULL, pending_totp_secret = NULL
		WHERE token_hash = ? AND used_at IS NULL`, now, tokenHash)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrInvalidLink
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	return s.recordOwn(ctx, EventSetupCompleted, ip)
}

type setupLink struct {
	passwordHash sql.NullString
	secret       *sealed
}

// usableLink loads the link for token when it is unused, unexpired, and no
// admin exists yet. A link can never replace an existing admin.
func (s *Service) usableLink(ctx context.Context, token string) (setupLink, error) {
	isSetUp, err := s.isSetUp(ctx)
	if err != nil {
		return setupLink{}, err
	}
	if isSetUp {
		return setupLink{}, ErrAlreadySetUp
	}
	if token == "" {
		return setupLink{}, ErrInvalidLink
	}

	var link setupLink
	var expiresAt int64
	var usedAt, keyVersion sql.NullInt64
	var ciphertext []byte
	err = s.db.QueryRowContext(ctx, `
		SELECT expires_at, used_at, pending_password_hash, pending_totp_key_version, pending_totp_secret
		FROM setup_links WHERE token_hash = ?`, hashToken(token)).
		Scan(&expiresAt, &usedAt, &link.passwordHash, &keyVersion, &ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return setupLink{}, ErrInvalidLink
	}
	if err != nil {
		return setupLink{}, err
	}

	isExpired := !s.now().Before(time.Unix(expiresAt, 0))
	if usedAt.Valid || isExpired {
		return setupLink{}, ErrInvalidLink
	}
	if keyVersion.Valid {
		link.secret = &sealed{version: int(keyVersion.Int64), ciphertext: ciphertext}
	}

	return link, nil
}

func (s *Service) isSetUp(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM admin").Scan(&count)
	return count == 1, err
}

// CheckLogin checks the password and the code. It always runs the password
// hash, so timing doesn't say which factor was wrong, and answers only pass
// or fail. A code passes once: the last used step only moves forward
// (OWASP ASVS v5.0.0-6.5.1).
func (s *Service) CheckLogin(ctx context.Context, ip, givenPassword, code string) error {
	if !s.limits.allowCheck(s.now()) {
		return ErrHashingIsBusy
	}

	locked, err := s.isLocked(ctx)
	if err != nil {
		return err
	}
	if locked {
		return ErrLocked
	}

	var passwordHash string
	var secretValue sealed
	var lastStep int64
	err = s.db.QueryRowContext(ctx, "SELECT password_hash, totp_key_version, totp_secret, last_totp_step FROM admin WHERE id = 1").
		Scan(&passwordHash, &secretValue.version, &secretValue.ciphertext, &lastStep)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotSetUp
	}
	if err != nil {
		return err
	}

	isPasswordValid, err := password.Verify(ctx, givenPassword, passwordHash)
	if errors.Is(err, password.ErrBusy) {
		return ErrHashingIsBusy
	}
	if err != nil {
		return err
	}
	// Checks running when the limit was reached must not get their codes
	// tried.
	if locked, err := s.isLocked(ctx); err != nil {
		return err
	} else if locked {
		return ErrLocked
	}
	secret, err := s.keys.open(adminSecretPurpose, secretValue)
	if err != nil {
		return err
	}
	step, isCodeValid := totp.Verify(string(secret), code, s.now(), lastStep)

	switch {
	case isPasswordValid && isCodeValid:
		return s.succeedLogin(ctx, ip, step)
	case isPasswordValid:
		return s.failCode(ctx, ip)
	default:
		// The same write a wrong code makes, so the time an answer takes
		// doesn't say which factor was wrong.
		if _, err := s.db.ExecContext(ctx, "DELETE FROM code_failures WHERE at <= ?", s.now().Add(-codeFailureWindow).Unix()); err != nil {
			return err
		}
		if err := s.recordOwn(ctx, EventLoginFailed, ip); err != nil {
			return err
		}
		return ErrWrongLogin
	}
}

func (s *Service) succeedLogin(ctx context.Context, ip string, step int64) error {
	// The lock is checked in the same statement, so a check that was
	// hashing when the limit was reached can't pass.
	result, err := s.db.ExecContext(ctx, `
		UPDATE admin SET last_totp_step = ?
		WHERE id = 1 AND last_totp_step < ?
			AND NOT EXISTS (SELECT 1 FROM login_lock WHERE id = 1 AND locked_until > ?)`,
		step, step, s.now().Unix())
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		locked, err := s.isLocked(ctx)
		if err != nil {
			return err
		}
		if locked {
			return ErrLocked
		}
		// Another request used this code first.
		if err := s.recordOwn(ctx, EventLoginFailed, ip); err != nil {
			return err
		}
		return ErrWrongLogin
	}

	return s.recordOwn(ctx, EventLoginSucceeded, ip)
}

// failCode counts a wrong code that came with the right password, which
// means someone has the password, and locks login once there are too many.
func (s *Service) failCode(ctx context.Context, ip string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := s.now()
	if _, err := tx.ExecContext(ctx, "DELETE FROM code_failures WHERE at <= ?", now.Add(-codeFailureWindow).Unix()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO code_failures (at) VALUES (?)", now.Unix()); err != nil {
		return err
	}

	var failures int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM code_failures").Scan(&failures); err != nil {
		return err
	}
	isNowLocked := failures >= codeFailureLimit
	if isNowLocked {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO login_lock (id, locked_until) VALUES (1, ?)
			ON CONFLICT (id) DO UPDATE SET locked_until = excluded.locked_until`, now.Add(codeLockout).Unix())
		if err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	if err := s.recordOwn(ctx, EventCodeFailed, ip); err != nil {
		return err
	}
	if isNowLocked {
		if err := s.recordOwn(ctx, EventCodeLocked, ip); err != nil {
			return err
		}
	}

	return ErrWrongLogin
}

func (s *Service) isLocked(ctx context.Context) (bool, error) {
	var lockedUntil int64
	err := s.db.QueryRowContext(ctx, "SELECT locked_until FROM login_lock WHERE id = 1").Scan(&lockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	return s.now().Before(time.Unix(lockedUntil, 0)), err
}

// ResetLogin deletes the admin, every setup link and the code guessing
// state. Only root can ask for it; it then issues a new link.
func (s *Service) ResetLogin(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, statement := range []string{
		"DELETE FROM admin",
		"DELETE FROM setup_links",
		"DELETE FROM code_failures",
		"DELETE FROM login_lock",
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	return s.recordOwn(ctx, EventLoginReset, "")
}

// Status is what root and the dashboard can learn about the service.
type Status struct {
	IsSetUp bool `json:"isSetUp"`
	// KeyVersions are the master key versions loaded; InUse those some
	// stored value is sealed with.
	KeyVersions []int `json:"keyVersions"`
	InUse       []int `json:"inUse"`
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	isSetUp, err := s.isSetUp(ctx)
	if err != nil {
		return Status{}, err
	}
	inUse, err := s.versionsInUse(ctx)
	if err != nil {
		return Status{}, err
	}

	loaded := make([]int, 0, len(s.keys))
	for version := range s.keys {
		loaded = append(loaded, version)
	}
	slices.Sort(loaded)

	return Status{IsSetUp: isSetUp, KeyVersions: loaded, InUse: inUse}, nil
}

func (s *Service) versionsInUse(ctx context.Context) ([]int, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT totp_key_version FROM admin
		UNION SELECT pending_totp_key_version FROM setup_links WHERE pending_totp_key_version IS NOT NULL
		ORDER BY 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	versions := []int{}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		versions = append(versions, version)
	}

	return versions, rows.Err()
}

// RotateKey re-seals every stored value with the newest loaded key in one
// transaction and returns the versions still in use afterwards: only the
// newest, unless there is nothing sealed at all. It refuses while a value
// names a version that isn't loaded, since that value couldn't be re-sealed.
func (s *Service) RotateKey(ctx context.Context) ([]int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if err := s.resealAdmin(ctx, tx); err != nil {
		return nil, err
	}
	if err := s.resealLinks(ctx, tx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	if err := s.recordOwn(ctx, EventKeyRotated, ""); err != nil {
		return nil, err
	}

	return s.versionsInUse(ctx)
}

func (s *Service) resealAdmin(ctx context.Context, tx *sql.Tx) error {
	var value sealed
	err := tx.QueryRowContext(ctx, "SELECT totp_key_version, totp_secret FROM admin WHERE id = 1").Scan(&value.version, &value.ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	resealed, err := s.reseal(adminSecretPurpose, value)
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, "UPDATE admin SET totp_key_version = ?, totp_secret = ? WHERE id = 1", resealed.version, resealed.ciphertext)
	return err
}

func (s *Service) resealLinks(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, "SELECT token_hash, pending_totp_key_version, pending_totp_secret FROM setup_links WHERE pending_totp_key_version IS NOT NULL")
	if err != nil {
		return err
	}

	type pending struct {
		tokenHash string
		value     sealed
	}
	var links []pending
	for rows.Next() {
		var link pending
		if err := rows.Scan(&link.tokenHash, &link.value.version, &link.value.ciphertext); err != nil {
			rows.Close()
			return err
		}
		links = append(links, link)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, link := range links {
		resealed, err := s.reseal(pendingSecretPurpose(link.tokenHash), link.value)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE setup_links SET pending_totp_key_version = ?, pending_totp_secret = ? WHERE token_hash = ?",
			resealed.version, resealed.ciphertext, link.tokenHash)
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) reseal(purpose string, value sealed) (sealed, error) {
	plaintext, err := s.keys.open(purpose, value)
	if errors.Is(err, errNoKey) {
		return sealed{}, ErrKeyNotLoaded
	}
	if err != nil {
		return sealed{}, err
	}

	return s.keys.seal(purpose, plaintext)
}

// Prune deletes expired setup links, old code failures, events older than a
// year, and the oldest events past maxEvents.
func (s *Service) Prune(ctx context.Context) error {
	now := s.now()
	statements := []struct {
		query string
		args  []any
	}{
		{"DELETE FROM setup_links WHERE used_at IS NULL AND expires_at <= ?", []any{now.Unix()}},
		{"DELETE FROM code_failures WHERE at <= ?", []any{now.Add(-codeFailureWindow).Unix()}},
		{"DELETE FROM security_events WHERE at < ?", []any{now.Add(-eventRetention).Unix()}},
		// A flood of one kind of event must not push out the record of a
		// reset, a rotation or a successful login.
		{`DELETE FROM security_events WHERE id <= (SELECT max(id) FROM security_events) - ?
			AND type NOT IN ('login_succeeded', 'setup_completed', 'login_reset', 'key_rotated', 'breach_reset', 'totp_locked')`,
			[]any{maxEvents}},
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return err
		}
	}

	return nil
}

// adminSecretPurpose and pendingSecretPurpose name the row a sealed TOTP
// secret belongs to (table, column and row key), so a ciphertext copied to
// another row doesn't open.
const adminSecretPurpose = "admin.totp_secret.1"

func pendingSecretPurpose(tokenHash string) string {
	return "setup_links.pending_totp_secret." + tokenHash
}

// newToken returns a random 256-bit token for setup links.
func newToken() (string, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(token), nil
}

// hashToken is what the database stores in place of a token, so a copy of
// the database can't be used to set up the login.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
