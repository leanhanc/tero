package auth

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/leanhanc/tero/internal/keys"
)

// SessionCookie is the session cookie's name. The __Host- prefix makes
// browsers require Secure and Path=/ and refuse a Domain attribute.
const SessionCookie = "__Host-tero_session"

// DeviceCookie marks a browser the admin has logged in from. It grants
// nothing by itself: it only moves that browser's failed logins from the
// account-wide throttle to a per-device one.
const DeviceCookie = "__Host-tero_device"

// DeviceLifetime is how long a device stays trusted without a login.
const DeviceLifetime = keys.DeviceLifetime

// Session is a live session, as the dashboard needs it.
type Session = keys.SessionState

// PasskeyOptions are what the browser passes to the WebAuthn API.
type PasskeyOptions = keys.PasskeyOptions

// Login has the key service check the password and TOTP code and start a
// session.
func (s *Service) Login(ctx context.Context, browser Browser, givenPassword, code string) (LoginResult, error) {
	result, err := s.keys.LoginPassword(ctx, browser, givenPassword, code)
	return result, fromKeys(err)
}

// BeginPasskeyLogin returns the options for a usernameless passkey login.
func (s *Service) BeginPasskeyLogin(ctx context.Context, ip string) (PasskeyOptions, error) {
	options, err := s.keys.BeginPasskeyLogin(ctx, ip)
	return options, fromKeys(err)
}

// LoginPasskey has the key service check a passkey answer and start a
// session. Any failure reads the same, so it doesn't say whether the login
// was set up or which check failed.
func (s *Service) LoginPasskey(ctx context.Context, browser Browser, challengeID string, answer json.RawMessage) (LoginResult, error) {
	result, err := s.keys.LoginPasskey(ctx, browser, challengeID, answer)
	err = fromKeys(err)
	if errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrNotSetUp) {
		return LoginResult{}, ErrPasskeyFailed
	}

	return result, err
}

// Authenticate checks a session token, marking the session as used from ip.
// It returns ErrSessionExpired for unknown, ended, idle or too old sessions.
func (s *Service) Authenticate(ctx context.Context, token, ip string) (Session, error) {
	if token == "" {
		return Session{}, ErrSessionExpired
	}

	session, err := s.keys.CheckSession(ctx, token, ip)
	return session, fromKeys(err)
}

// Logout ends the session for token.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}

	return fromKeys(s.keys.EndSession(ctx, token))
}
