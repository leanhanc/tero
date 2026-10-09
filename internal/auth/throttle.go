package auth

import (
	"context"
	"database/sql"
	"errors"
	"net/netip"
	"time"
)

// Failed logins are throttled per source IP and, for browsers that have never
// logged in, for the account as a whole (OWASP ASVS v5.0.0-6.3.1). A browser
// holding a trusted-device cookie is throttled on that device instead of the
// account, so strangers failing logins can't lock the admin out. Past the
// threshold, each further failure doubles the lockout, up to maxLockout.
const (
	ipThreshold      = 5
	accountThreshold = 10
	deviceThreshold  = 5
	baseLockout      = 30 * time.Second
	maxLockout       = 15 * time.Minute
	// failureMemory is how long a quiet key keeps its failure count.
	failureMemory = 24 * time.Hour
)

// throttleKey is one counter an attempt is charged to.
type throttleKey struct {
	name      string
	threshold int
}

var accountKey = throttleKey{name: "account", threshold: accountThreshold}

// ipKey groups IPv6 clients by /64, since one host usually gets a whole /64
// and could otherwise rotate addresses past the limit.
func ipKey(ip string) throttleKey {
	address, err := netip.ParseAddr(ip)
	if err == nil && address.Is6() && !address.Is4In6() {
		prefix, _ := address.Prefix(64)
		return throttleKey{name: "ip:" + prefix.String(), threshold: ipThreshold}
	}

	return throttleKey{name: "ip:" + ip, threshold: ipThreshold}
}

func deviceKey(deviceHash string) throttleKey {
	return throttleKey{name: "device:" + deviceHash, threshold: deviceThreshold}
}

// attempt is a login or setup attempt that was charged up front.
type attempt struct {
	keys              []throttleKey
	hasStartedLockout bool
}

// checkThrottle is a cheap read-only check for requests that are already
// locked out, so they don't wait for a password-hashing slot.
func (s *Service) checkThrottle(ctx context.Context, keys []throttleKey) error {
	now := s.now()
	for _, key := range keys {
		lockedUntil, err := s.lockedUntil(ctx, s.db, key)
		if err != nil {
			return err
		}

		remaining := lockedUntil.Sub(now)
		if remaining > 0 {
			return RateLimitedError{RetryAfter: remaining.Round(time.Second)}
		}
	}

	return nil
}

// chargeAttempt counts the attempt as a failure on every key before the
// credentials are checked, in one write transaction. Concurrent attempts are
// serialised on that transaction, so no more than the threshold can get past
// the check; a successful attempt is refunded with forgive. It returns a
// RateLimitedError when any key is locked.
func (s *Service) chargeAttempt(ctx context.Context, keys []throttleKey) (attempt, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return attempt{}, err
	}
	defer tx.Rollback()

	now := s.now()
	for _, key := range keys {
		lockedUntil, err := s.lockedUntil(ctx, tx, key)
		if err != nil {
			return attempt{}, err
		}

		remaining := lockedUntil.Sub(now)
		if remaining > 0 {
			return attempt{}, RateLimitedError{RetryAfter: remaining.Round(time.Second)}
		}
	}

	charged := attempt{keys: keys}
	for _, key := range keys {
		hasStartedLockout, err := s.countFailure(ctx, tx, key)
		if err != nil {
			return attempt{}, err
		}
		charged.hasStartedLockout = charged.hasStartedLockout || hasStartedLockout
	}

	return charged, tx.Commit()
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *Service) lockedUntil(ctx context.Context, db queryer, key throttleKey) (time.Time, error) {
	var lockedUntil int64
	err := db.QueryRowContext(ctx, "SELECT locked_until FROM login_throttles WHERE key = ?", key.name).Scan(&lockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, nil
	}

	return time.Unix(lockedUntil, 0), err
}

// countFailure adds one failure to key and reports whether that started a
// lockout. A lockout is only ever extended, never shortened.
func (s *Service) countFailure(ctx context.Context, tx *sql.Tx, key throttleKey) (bool, error) {
	now := s.now()

	var failures int
	var lastFailure, storedLockedUntil int64
	err := tx.QueryRowContext(ctx, "SELECT failures, last_failure, locked_until FROM login_throttles WHERE key = ?", key.name).
		Scan(&failures, &lastFailure, &storedLockedUntil)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	isForgotten := now.Sub(time.Unix(lastFailure, 0)) > failureMemory
	if isForgotten {
		failures = 0
	}
	failures++

	lockout := lockoutAfter(failures, key.threshold)
	lockedUntil := max(storedLockedUntil, now.Add(lockout).Unix())

	_, err = tx.ExecContext(ctx, `
		INSERT INTO login_throttles (key, failures, last_failure, locked_until) VALUES (?, ?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET failures = excluded.failures, last_failure = excluded.last_failure, locked_until = excluded.locked_until`,
		key.name, failures, now.Unix(), lockedUntil)

	return lockout > 0, err
}

// lockoutAfter is 0 below threshold, then baseLockout doubling with every
// further failure, capped at maxLockout.
func lockoutAfter(failures, threshold int) time.Duration {
	if failures < threshold {
		return 0
	}

	lockout := baseLockout
	for extra := failures - threshold; extra > 0 && lockout < maxLockout; extra-- {
		lockout *= 2
	}

	return min(lockout, maxLockout)
}

// forgive clears the counters an attempt was charged to, after it succeeded.
func (s *Service) forgive(ctx context.Context, charged attempt) error {
	for _, key := range charged.keys {
		if _, err := s.db.ExecContext(ctx, "DELETE FROM login_throttles WHERE key = ?", key.name); err != nil {
			return err
		}
	}

	return nil
}

// recordFailedAttempt records the events for a charged attempt that failed.
func (s *Service) recordFailedAttempt(ctx context.Context, ip string, charged attempt, eventType EventType) error {
	if err := s.record(ctx, Event{At: s.now(), Type: eventType, IP: ip}); err != nil {
		return err
	}
	if !charged.hasStartedLockout {
		return nil
	}

	return s.record(ctx, Event{At: s.now(), Type: EventRateLimited, IP: ip})
}
