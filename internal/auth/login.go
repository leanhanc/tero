package auth

import (
	"context"
	"errors"
)

// LoginResult is what a successful login or setup hands back to the browser.
type LoginResult struct {
	SessionToken string
	// DeviceToken marks the browser as trusted; see DeviceCookie.
	DeviceToken string
}

// Login has the key service check the password and TOTP code and returns a
// new session. deviceToken is the browser's trusted-device cookie, if any.
// The attempt is charged to the throttle before credentials are checked, so
// a rate-limited client learns nothing about its credentials, and the key
// service is only asked about attempts the throttle let through.
func (s *Service) Login(ctx context.Context, ip, deviceToken, givenPassword, code string) (LoginResult, error) {
	throttles, isTrustedDevice, err := s.throttleKeys(ctx, ip, deviceToken)
	if err != nil {
		return LoginResult{}, err
	}
	if err := s.checkThrottle(ctx, throttles); err != nil {
		return LoginResult{}, err
	}

	charged, err := s.chargeAttempt(ctx, throttles)
	if err != nil {
		return LoginResult{}, err
	}

	err = fromKeys(s.keys.CheckLogin(ctx, ip, givenPassword, code))
	switch {
	case err == nil:
	case errors.Is(err, ErrInvalidCredentials):
		s.recordLockout(ctx, ip, charged)
		return LoginResult{}, err
	default:
		// Not set up, locked, or the key service is down: none of these say
		// anything about the credentials, so its failure is taken back.
		if refundErr := s.refund(ctx, charged); refundErr != nil {
			return LoginResult{}, refundErr
		}
		return LoginResult{}, err
	}

	result, err := s.startSession(ctx, deviceToken, isTrustedDevice)
	if err != nil {
		return LoginResult{}, err
	}

	return result, s.forgive(ctx, charged)
}

// startSession creates a session and trusts the browser in one transaction.
func (s *Service) startSession(ctx context.Context, deviceToken string, isTrustedDevice bool) (LoginResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LoginResult{}, err
	}
	defer tx.Rollback()

	sessionToken, err := s.createSession(ctx, tx)
	if err != nil {
		return LoginResult{}, err
	}
	newDeviceToken, err := s.trustDevice(ctx, tx, deviceToken, isTrustedDevice)
	if err != nil {
		return LoginResult{}, err
	}

	return LoginResult{SessionToken: sessionToken, DeviceToken: newDeviceToken}, tx.Commit()
}
