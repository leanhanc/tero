// Package auth is the dashboard's side of the single-admin login: sessions,
// trusted browsers and brute-force throttling, kept in the SQLite store. The
// admin's password hash, TOTP secret and setup links belong to the key
// service (internal/keys), which checks every setup and login; the dashboard
// never stores or reads them. Security events are recorded there too.
package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/password"
)

// Errors shown to the person at the dashboard. Login failures never say which
// factor was wrong.
var (
	ErrNotSetUp           = errors.New("The dashboard login isn't set up yet. Use the setup link printed on the server, or run `sudo tero reset-login` there for a new one")
	ErrInvalidLink        = errors.New("This setup link is invalid, already used or expired. Run `sudo tero reset-login` on the server for a new one")
	ErrInvalidCredentials = errors.New("Wrong password or code")
	ErrInvalidCode        = errors.New("That code didn't match. Please check your authenticator app and try again.")
	ErrSessionExpired     = errors.New("Your session has ended. Log in again")
	ErrAlreadySetUp       = errors.New("The dashboard login is already set up")
	ErrKeysUnavailable    = errors.New("The server's key service isn't running, so logins can't be checked. Run `sudo tero reset-login` on the server")
	ErrLoginLocked        = errors.New("Login is locked for 24 hours after too many wrong codes. Run `sudo tero reset-login` on the server to unlock it")
)

// RateLimitedError means too many failures came from this IP or for the
// account. It is returned before credentials are checked, so it says nothing
// about whether they were right.
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e RateLimitedError) Error() string {
	return "Too many attempts. Try again later"
}

// Service carries the login state for one server.
type Service struct {
	db        *sql.DB
	keys      *keys.Client
	domain    string
	now       func() time.Time
	passwords password.Policy
	journal   JournalWriter
}

// Options configure a Service. Now and Journal default to the real clock and
// no journal; tests replace them. Keys is the key service.
type Options struct {
	Keys      *keys.Client
	Domain    string
	Passwords password.Policy
	Now       func() time.Time
	Journal   JournalWriter
}

// New returns a Service backed by db, which must already be migrated.
func New(db *sql.DB, opts Options) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	return &Service{db: db, keys: opts.Keys, domain: opts.Domain, now: now, passwords: opts.Passwords, journal: opts.Journal}
}

// IsSetUp reports whether the admin account exists.
func (s *Service) IsSetUp(ctx context.Context) (bool, error) {
	status, err := s.keys.Status(ctx)
	return status.IsSetUp, fromKeys(err)
}

// fromKeys turns key service errors into the ones the dashboard shows.
func fromKeys(err error) error {
	translations := map[error]error{
		keys.ErrUnavailable:   ErrKeysUnavailable,
		keys.ErrInvalidLink:   ErrInvalidLink,
		keys.ErrAlreadySetUp:  ErrInvalidLink,
		keys.ErrNotSetUp:      ErrNotSetUp,
		keys.ErrInvalidCode:   ErrInvalidCode,
		keys.ErrWrongLogin:    ErrInvalidCredentials,
		keys.ErrLocked:        ErrLoginLocked,
		keys.ErrHashingIsBusy: password.ErrBusy,
	}
	for from, to := range translations {
		if errors.Is(err, from) {
			return to
		}
	}

	return err
}
