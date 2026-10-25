// Package auth is the dashboard's side of the single-admin login and account
// settings. The key service (internal/keys) checks every setup, login and
// confirmation, holds the sessions, trusted browsers, throttles, passkeys
// and the security log, and creates every session; the dashboard never
// stores or reads any of them. What stays here is what needs the network or
// the browser: the password policy and turning key service errors into the
// messages the dashboard shows.
package auth

import (
	"context"
	"errors"

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
	ErrNotConfirmed       = errors.New("Confirm it's you with your password and a code first")
	ErrPasskeyFailed      = errors.New("That passkey didn't work. Try again, or log in with your password and a code")
	ErrPasskeyLimit       = errors.New("You already have 10 passkeys. Remove one first")
	ErrInvalidPasskeyName = errors.New("Give the passkey a name of 1 to 64 characters")
	ErrNotFound           = errors.New("Not found")
)

// RateLimitedError means too many failures came from this IP or browser, or
// for the account.
type RateLimitedError = keys.RateLimitedError

// Browser is what the dashboard saw of the browser making a request.
type Browser = keys.Browser

// LoginResult is what a successful login or setup hands back to the browser.
type LoginResult = keys.LoginResult

// Service carries the login state for one server.
type Service struct {
	keys      *keys.Client
	domain    string
	passwords password.Policy
	journal   JournalWriter
}

// Options configure a Service. Keys is the key service. Passwords' context
// words are filled from Domain and PublicIP. Journal defaults to no journal.
type Options struct {
	Keys      *keys.Client
	Domain    string
	PublicIP  string
	Passwords password.Policy
	Journal   JournalWriter
}

// New returns a Service that asks the key service in opts.Keys.
func New(opts Options) *Service {
	passwords := opts.Passwords
	passwords.ContextWords = password.ContextWords(opts.Domain, opts.PublicIP)

	return &Service{keys: opts.Keys, domain: opts.Domain, passwords: passwords, journal: opts.Journal}
}

// IsSetUp reports whether the admin account exists.
func (s *Service) IsSetUp(ctx context.Context) (bool, error) {
	status, err := s.keys.Status(ctx)
	return status.IsSetUp, fromKeys(err)
}

// checkNewPassword runs the password policy, recording when the online
// breach check couldn't run.
func (s *Service) checkNewPassword(ctx context.Context, ip, newPassword string) error {
	result, err := s.passwords.Check(ctx, newPassword)
	if err != nil {
		return err
	}
	if result.IsOnlineCheckSkipped {
		s.record(ctx, keys.EventBreachCheckSkipped, ip)
	}

	return nil
}

// fromKeys turns key service errors into the ones the dashboard shows.
func fromKeys(err error) error {
	if err == nil {
		return nil
	}

	var limited keys.RateLimitedError
	if errors.As(err, &limited) {
		return limited
	}

	translations := map[error]error{
		keys.ErrUnavailable:    ErrKeysUnavailable,
		keys.ErrInvalidLink:    ErrInvalidLink,
		keys.ErrAlreadySetUp:   ErrInvalidLink,
		keys.ErrNotSetUp:       ErrNotSetUp,
		keys.ErrInvalidCode:    ErrInvalidCode,
		keys.ErrWrongLogin:     ErrInvalidCredentials,
		keys.ErrLocked:         ErrLoginLocked,
		keys.ErrHashingIsBusy:  password.ErrBusy,
		keys.ErrSessionEnded:   ErrSessionExpired,
		keys.ErrNotConfirmed:   ErrNotConfirmed,
		keys.ErrInvalidPasskey: ErrPasskeyFailed,
		keys.ErrPasskeyLimit:   ErrPasskeyLimit,
		keys.ErrInvalidName:    ErrInvalidPasskeyName,
		keys.ErrNotFound:       ErrNotFound,
	}
	for from, to := range translations {
		if errors.Is(err, from) {
			return to
		}
	}

	return err
}
