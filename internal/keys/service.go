// Package keys is the key service, `tero keys`: the only process that holds
// Tero's master key. It runs as its own system user, tero-keys, with no
// network, and answers the dashboard and root over a Unix socket. It keeps
// the admin's password hash, the TOTP secret (sealed with the master key),
// passkeys and setup links, checks every login, holds the dashboard's
// sessions, and keeps the security log, so nothing running as tero can read
// the key, read or replace the admin's credentials, create a session, or
// change a recorded event.
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

	"github.com/go-webauthn/webauthn/webauthn"

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
	ErrRefused        = errors.New("the key service refused this request")
	ErrUnavailable    = errors.New("the key service isn't running")
	ErrInvalidLink    = errors.New("the setup link is invalid, already used or expired")
	ErrAlreadySetUp   = errors.New("the dashboard login is already set up")
	ErrNotSetUp       = errors.New("the dashboard login isn't set up")
	ErrInvalidCode    = errors.New("the TOTP code didn't match")
	ErrWrongLogin     = errors.New("wrong password or code")
	ErrLocked         = errors.New("login is locked after too many wrong codes")
	ErrKeyNotLoaded   = errors.New("a value is sealed with a master key that isn't loaded")
	ErrHashingIsBusy  = errors.New("the key service is busy")
	ErrSessionEnded   = errors.New("the session has ended")
	ErrNotConfirmed   = errors.New("confirm it's you first")
	ErrPasskeyLimit   = errors.New("there are already as many passkeys as allowed")
	ErrInvalidName    = errors.New("a passkey name must be 1 to 64 characters")
	ErrNotFound       = errors.New("not found")
	ErrInvalidPasskey = errors.New("the passkey answer is invalid or expired")
)

// RateLimitedError means too many failures came from this IP, browser or for
// the account. It is returned before credentials are checked, so it says
// nothing about whether they were right.
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e RateLimitedError) Error() string {
	return "Too many attempts. Try again later"
}

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
	db         *sql.DB
	keys       keyring
	now        func() time.Time
	journal    JournalWriter
	limits     limits
	passkeys   *webauthn.WebAuthn
	challenges challenges
}

// Options configure a Service. Domain is the dashboard's domain, which
// passkeys are bound to. Now and Journal default to the real clock and no
// journal; tests replace them.
type Options struct {
	Domain  string
	Now     func() time.Time
	Journal JournalWriter
}

// NewService returns a Service backed by db, which must already be migrated,
// holding the master key versions in keys.
func NewService(db *sql.DB, keys map[int][]byte, opts Options) (*Service, error) {
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	passkeys, err := newRelyingParty(opts.Domain)
	if err != nil {
		return nil, err
	}

	return &Service{db: db, keys: keys, now: now, journal: opts.Journal, passkeys: passkeys}, nil
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
func (s *Service) BeginSetup(ctx context.Context, ip, token, newPassword string) (Enrolment, error) {
	if err := s.checkThrottle(ctx, []throttleKey{ipKey(ip)}); err != nil {
		return Enrolment{}, err
	}
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

// FinishSetup checks a code against the pending secret, creates the admin,
// uses up the link and logs the admin in, trusting this browser. A wrong
// code is charged to the throttle like a failed login, so the six-digit code
// can't be guessed at speed.
func (s *Service) FinishSetup(ctx context.Context, browser Browser, token, code string) (LoginResult, error) {
	throttles := []throttleKey{ipKey(browser.IP), accountKey}
	err := s.checkThrottled(ctx, throttles, browser.IP, func(ctx context.Context) error {
		return s.createAdmin(ctx, browser.IP, token, code)
	})
	if err != nil {
		return LoginResult{}, err
	}

	return s.startSession(ctx, MethodSetup, Browser{IP: browser.IP, UserAgent: browser.UserAgent}, false)
}

// createAdmin checks a code against the pending secret and, in one
// transaction, creates the admin and uses up the link.
func (s *Service) createAdmin(ctx context.Context, ip, token, code string) error {
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

	userHandle, err := newUserHandle()
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
		INSERT INTO admin (id, password_hash, totp_key_version, totp_secret, last_totp_step, webauthn_user_id, created_at)
		VALUES (1, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO NOTHING`,
		link.passwordHash.String, sealedSecret.version, sealedSecret.ciphertext, step, userHandle, now)
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

// ResetLogin deletes the admin, every passkey, setup link, session, trusted
// browser and throttle, and the code guessing state. Only root can ask for
// it; it then issues a new link. Whoever runs it has proven root access to
// the server, which is why the throttles go too.
func (s *Service) ResetLogin(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, statement := range []string{
		"DELETE FROM admin",
		"DELETE FROM passkeys",
		"DELETE FROM setup_links",
		"DELETE FROM sessions",
		"DELETE FROM trusted_devices",
		"DELETE FROM login_throttles",
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
	s.challenges.clear()

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

// Prune deletes what can no longer be used: expired setup links and
// sessions, stale trusted browsers, throttle counters that are both
// forgotten and unlocked, old code failures, events older than a year, and
// the oldest events past maxEvents. Expired rows are already refused when
// presented; pruning only keeps the tables small.
func (s *Service) Prune(ctx context.Context) error {
	now := s.now()
	statements := []struct {
		query string
		args  []any
	}{
		{"DELETE FROM setup_links WHERE used_at IS NULL AND expires_at <= ?", []any{now.Unix()}},
		{"DELETE FROM sessions WHERE last_seen_at <= ? OR created_at <= ?",
			[]any{now.Add(-sessionIdleTimeout).Unix(), now.Add(-sessionAbsoluteTimeout).Unix()}},
		{"DELETE FROM trusted_devices WHERE last_used_at < ?", []any{now.Add(-DeviceLifetime).Unix()}},
		{"DELETE FROM login_throttles WHERE last_failure < ? AND locked_until <= ?",
			[]any{now.Add(-failureMemory).Unix(), now.Unix()}},
		{"DELETE FROM code_failures WHERE at <= ?", []any{now.Add(-codeFailureWindow).Unix()}},
		{"DELETE FROM security_events WHERE at < ?", []any{now.Add(-eventRetention).Unix()}},
		// A flood of one kind of event must not push out the record of a
		// reset, a rotation or a successful login.
		{`DELETE FROM security_events WHERE id <= (SELECT max(id) FROM security_events) - ?
			AND type NOT IN ('login_succeeded', 'passkey_login_succeeded', 'setup_completed', 'login_reset', 'key_rotated',
				'breach_reset', 'totp_locked', 'password_changed', 'passkey_added', 'passkey_removed')`,
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

// newToken returns a random 256-bit token for setup links, sessions and
// trusted browsers.
func newToken() (string, error) {
	token := make([]byte, 32)
	if _, err := rand.Read(token); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(token), nil
}

// newID returns a random 128-bit id for rows the dashboard may name, such as
// sessions and passkeys. An id grants nothing by itself.
func newID() (string, error) {
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return "", err
	}

	return hex.EncodeToString(id), nil
}

// hashToken is what the database stores in place of a token, so a copy of
// the database can't be used to set up the login or as a session.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
