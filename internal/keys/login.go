package keys

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/leanhanc/tero/internal/password"
	"github.com/leanhanc/tero/internal/totp"
)

// credentialCheck names the events a password and code check records. A
// check made to log in and one made to confirm it's you are told apart in
// the log, and only the login one is recorded as a success here; the others
// record their own outcome once it happened.
type credentialCheck struct {
	succeeded     EventType
	wrongPassword EventType
	wrongCode     EventType
}

var (
	loginCheck   = credentialCheck{succeeded: EventLoginSucceeded, wrongPassword: EventLoginFailed, wrongCode: EventCodeFailed}
	confirmCheck = credentialCheck{wrongPassword: EventConfirmFailed, wrongCode: EventConfirmCodeFailed}
)

// LoginPassword checks the password and TOTP code and starts a session. The
// attempt is charged to the throttle before the credentials are checked, so
// a rate-limited client learns nothing about its credentials.
func (s *Service) LoginPassword(ctx context.Context, browser Browser, givenPassword, code string) (LoginResult, error) {
	throttles, isTrustedDevice, err := s.throttleKeys(ctx, browser)
	if err != nil {
		return LoginResult{}, err
	}

	err = s.checkThrottled(ctx, throttles, browser.IP, func(ctx context.Context) error {
		return s.checkCredentials(ctx, browser.IP, givenPassword, code, loginCheck)
	})
	if err != nil {
		return LoginResult{}, err
	}

	return s.startSession(ctx, MethodPassword, browser, isTrustedDevice)
}

// checkThrottled charges throttles, runs check and settles the charge: a
// success clears the counters, a wrong credential keeps the failure, and any
// other outcome (not set up, locked, busy, the key service failing) says
// nothing about the credentials, so its failure is taken back.
func (s *Service) checkThrottled(ctx context.Context, throttles []throttleKey, ip string, check func(context.Context) error) error {
	if err := s.checkThrottle(ctx, throttles); err != nil {
		return err
	}
	charged, err := s.chargeAttempt(ctx, throttles)
	if err != nil {
		return err
	}

	err = check(ctx)
	switch {
	case err == nil:
		return s.forgive(ctx, charged)
	case errors.Is(err, ErrWrongLogin), errors.Is(err, ErrInvalidCode):
		return errors.Join(err, s.recordLockout(ctx, ip, charged))
	default:
		return errors.Join(err, s.refund(ctx, charged))
	}
}

// checkCredentials checks the password and the code. It always runs the
// password hash, so timing doesn't say which factor was wrong, and answers
// only pass or fail. A code passes once: the last used step only moves
// forward (OWASP ASVS v5.0.0-6.5.1).
func (s *Service) checkCredentials(ctx context.Context, ip, givenPassword, code string, events credentialCheck) error {
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
	// A code that matches but was already used is a replay, such as the
	// admin confirming with the code they just logged in with. It is
	// refused, but it isn't a guess, so it doesn't count toward the lock.
	_, isUsedCode := totp.Verify(string(secret), code, s.now(), 0)

	switch {
	case isPasswordValid && isCodeValid:
		return s.useCode(ctx, ip, step, events)
	case isPasswordValid && !isUsedCode:
		return s.failCode(ctx, ip, events.wrongCode)
	default:
		// The same write a wrong code makes, so the time an answer takes
		// doesn't say which factor was wrong.
		if _, err := s.db.ExecContext(ctx, "DELETE FROM code_failures WHERE at <= ?", s.now().Add(-codeFailureWindow).Unix()); err != nil {
			return err
		}
		if err := s.recordOwn(ctx, events.wrongPassword, ip); err != nil {
			return err
		}
		return ErrWrongLogin
	}
}

// useCode moves the last used step forward to step, so the code can't pass
// again.
func (s *Service) useCode(ctx context.Context, ip string, step int64, events credentialCheck) error {
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
		if err := s.recordOwn(ctx, events.wrongPassword, ip); err != nil {
			return err
		}
		return ErrWrongLogin
	}

	if events.succeeded == "" {
		return nil
	}
	return s.recordOwn(ctx, events.succeeded, ip)
}

// failCode counts a wrong code that came with the right password, which
// means someone has the password, and locks every password and code check
// once there are too many.
func (s *Service) failCode(ctx context.Context, ip string, eventType EventType) error {
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

	if err := s.recordOwn(ctx, eventType, ip); err != nil {
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
