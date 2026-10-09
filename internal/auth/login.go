package auth

import (
	"context"
	"database/sql"
	"errors"

	"github.com/leanhanc/tero/internal/password"
	"github.com/leanhanc/tero/internal/totp"
)

// LoginResult is what a successful login or setup hands back to the browser.
type LoginResult struct {
	SessionToken string
	// DeviceToken marks the browser as trusted; see DeviceCookie.
	DeviceToken string
}

// Login checks the password and TOTP code and returns a new session.
// deviceToken is the browser's trusted-device cookie, if any. The attempt is
// charged to the throttle before credentials are checked, so a rate-limited
// client learns nothing about its credentials.
func (s *Service) Login(ctx context.Context, ip, deviceToken, givenPassword, code string) (LoginResult, error) {
	isSetUp, err := s.IsSetUp(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	if !isSetUp {
		return LoginResult{}, ErrNotSetUp
	}

	keys, isTrustedDevice, err := s.throttleKeys(ctx, ip, deviceToken)
	if err != nil {
		return LoginResult{}, err
	}
	if err := s.checkThrottle(ctx, keys); err != nil {
		return LoginResult{}, err
	}

	charged, err := s.chargeAttempt(ctx, keys)
	if err != nil {
		return LoginResult{}, err
	}

	step, isValid, err := s.checkCredentials(ctx, givenPassword, code)
	if err != nil {
		return LoginResult{}, err
	}
	if !isValid {
		return LoginResult{}, s.failLogin(ctx, ip, charged)
	}

	return s.succeedLogin(ctx, ip, charged, step, deviceToken, isTrustedDevice)
}

// checkCredentials verifies both factors. It always checks both, so timing
// doesn't tell which one was wrong.
func (s *Service) checkCredentials(ctx context.Context, givenPassword, code string) (int64, bool, error) {
	var passwordHash, secret string
	var lastStep int64
	err := s.db.QueryRowContext(ctx, "SELECT password_hash, totp_secret, last_totp_step FROM admin WHERE id = 1").Scan(&passwordHash, &secret, &lastStep)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, ErrNotSetUp
	}
	if err != nil {
		return 0, false, err
	}

	isPasswordValid, err := password.Verify(ctx, givenPassword, passwordHash)
	if err != nil {
		return 0, false, err
	}
	step, isCodeValid := totp.Verify(secret, code, s.now(), lastStep)

	return step, isPasswordValid && isCodeValid, nil
}

func (s *Service) failLogin(ctx context.Context, ip string, charged attempt) error {
	if err := s.recordFailedAttempt(ctx, ip, charged, EventLoginFailed); err != nil {
		return err
	}

	return ErrInvalidCredentials
}

func (s *Service) succeedLogin(ctx context.Context, ip string, charged attempt, step int64, deviceToken string, isTrustedDevice bool) (LoginResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LoginResult{}, err
	}
	defer tx.Rollback()

	// The step only moves forward, so two requests racing with the same code
	// can't both log in (OWASP ASVS v5.0.0-6.5.1).
	result, err := tx.ExecContext(ctx, "UPDATE admin SET last_totp_step = ? WHERE id = 1 AND last_totp_step < ?", step, step)
	if err != nil {
		return LoginResult{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		tx.Rollback()
		return LoginResult{}, s.failLogin(ctx, ip, charged)
	}

	sessionToken, err := s.createSession(ctx, tx)
	if err != nil {
		return LoginResult{}, err
	}
	newDeviceToken, err := s.trustDevice(ctx, tx, deviceToken, isTrustedDevice)
	if err != nil {
		return LoginResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return LoginResult{}, err
	}

	if err := s.forgive(ctx, charged); err != nil {
		return LoginResult{}, err
	}
	if err := s.record(ctx, Event{At: s.now(), Type: EventLoginSucceeded, IP: ip}); err != nil {
		return LoginResult{}, err
	}

	return LoginResult{SessionToken: sessionToken, DeviceToken: newDeviceToken}, nil
}
