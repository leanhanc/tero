// Package auth is the dashboard's single-admin login: one-time setup links,
// password plus required TOTP, sessions, brute-force throttling and security
// events. Everything lives in the SQLite store, so the service and root CLI
// commands see the same state and nothing is lost on restart.
package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/leanhanc/tero/internal/password"
)

// Errors shown to the person at the dashboard. Login failures never say which
// factor was wrong.
var (
	ErrNotSetUp           = errors.New("The dashboard login isn't set up yet. Use the setup link printed on the server, or run `sudo tero reset-login` there for a new one")
	ErrInvalidLink        = errors.New("This setup link is invalid, already used or expired. Run `sudo tero reset-login` on the server for a new one")
	ErrInvalidCredentials = errors.New("Wrong password or code")
	ErrInvalidCode        = errors.New("That code didn't match. Check the time on your phone and try the newest code")
	ErrSessionExpired     = errors.New("Your session has ended. Log in again")
	ErrAlreadySetUp       = errors.New("The dashboard login is already set up")
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
	domain    string
	now       func() time.Time
	passwords password.Policy
	journal   JournalWriter
}

// Options configure a Service. Now and Journal default to the real clock and
// no journal; tests replace them.
type Options struct {
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

	return &Service{db: db, domain: opts.Domain, now: now, passwords: opts.Passwords, journal: opts.Journal}
}

// IsSetUp reports whether the admin account exists.
func (s *Service) IsSetUp(ctx context.Context) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM admin").Scan(&count)
	return count == 1, err
}
