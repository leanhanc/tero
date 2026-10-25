package keys

import (
	"context"
	"errors"

	"github.com/leanhanc/tero/internal/password"
)

// Confirm checks the password and a TOTP code on a live session and, when
// both pass, lets that session end sessions and manage passkeys for
// confirmationLifetime (OWASP ASVS v5.0.0-7.5.1). Wrong attempts are charged
// to the same throttles as logins and count toward the code guessing limit,
// so a stolen session can't guess the password faster than the login page.
func (s *Service) Confirm(ctx context.Context, token string, browser Browser, givenPassword, code string) error {
	session, err := s.liveSession(ctx, token)
	if err != nil {
		return err
	}

	if err := s.checkOnSession(ctx, browser, givenPassword, code); err != nil {
		return err
	}
	if err := s.markConfirmed(ctx, session.id); err != nil {
		return err
	}

	return s.recordOwn(ctx, EventAccountConfirmed, browser.IP)
}

// ChangePassword replaces the admin's password after checking the current
// one and a TOTP code in the same request (OWASP ASVS v5.0.0-6.2.3), and
// ends every other session when isEndingOthers is set (v5.0.0-7.4.3). The
// dashboard has already checked newPassword against the password policy.
// The TOTP secret and passkeys are kept.
func (s *Service) ChangePassword(ctx context.Context, token string, browser Browser, currentPassword, code, newPassword string, isEndingOthers bool) error {
	session, err := s.liveSession(ctx, token)
	if err != nil {
		return err
	}
	if len(newPassword) == 0 || len(newPassword) > maxPasswordBytes {
		return ErrRefused
	}

	if err := s.checkOnSession(ctx, browser, currentPassword, code); err != nil {
		return err
	}

	passwordHash, err := password.Hash(ctx, newPassword)
	if errors.Is(err, password.ErrBusy) {
		return ErrHashingIsBusy
	}
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE admin SET password_hash = ? WHERE id = 1", passwordHash); err != nil {
		return err
	}
	if err := s.recordOwn(ctx, EventPasswordChanged, browser.IP); err != nil {
		return err
	}
	if err := s.markConfirmed(ctx, session.id); err != nil {
		return err
	}
	if !isEndingOthers {
		return nil
	}

	return s.endOtherSessions(ctx, session.id, browser.IP)
}

// checkOnSession checks the password and code for a logged-in admin, through
// the same throttles as a login from browser.
func (s *Service) checkOnSession(ctx context.Context, browser Browser, givenPassword, code string) error {
	throttles, _, err := s.throttleKeys(ctx, browser)
	if err != nil {
		return err
	}

	return s.checkThrottled(ctx, throttles, browser.IP, func(ctx context.Context) error {
		return s.checkCredentials(ctx, browser.IP, givenPassword, code, confirmCheck)
	})
}

func (s *Service) markConfirmed(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE sessions SET confirmed_at = ? WHERE id = ?", s.now().Unix(), sessionID)
	return err
}
