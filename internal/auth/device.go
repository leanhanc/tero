package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// DeviceCookie marks a browser the admin has logged in from. It grants
// nothing by itself: it only moves that browser's failed logins from the
// account-wide throttle to a per-device one (OWASP's device cookie pattern).
const DeviceCookie = "__Host-tero_device"

// DeviceLifetime is how long a device stays trusted without a login.
const DeviceLifetime = 180 * 24 * time.Hour

// trustedDevice returns the throttle key for deviceToken, or false when the
// token is missing, unknown or stale.
func (s *Service) trustedDevice(ctx context.Context, deviceToken string) (throttleKey, bool, error) {
	if deviceToken == "" {
		return throttleKey{}, false, nil
	}

	tokenHash := hashToken(deviceToken)
	var lastUsedAt int64
	err := s.db.QueryRowContext(ctx, "SELECT last_used_at FROM trusted_devices WHERE token_hash = ?", tokenHash).Scan(&lastUsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return throttleKey{}, false, nil
	}
	if err != nil {
		return throttleKey{}, false, err
	}

	isStale := s.now().Sub(time.Unix(lastUsedAt, 0)) > DeviceLifetime
	if isStale {
		return throttleKey{}, false, nil
	}

	return deviceKey(tokenHash), true, nil
}

// trustDevice records a successful login from deviceToken, issuing a new
// token when the browser didn't have a valid one. It returns the token the
// browser should keep.
func (s *Service) trustDevice(ctx context.Context, tx *sql.Tx, deviceToken string, isTrusted bool) (string, error) {
	now := s.now().Unix()
	if isTrusted {
		_, err := tx.ExecContext(ctx, "UPDATE trusted_devices SET last_used_at = ? WHERE token_hash = ?", now, hashToken(deviceToken))
		return deviceToken, err
	}

	newDeviceToken, err := newToken()
	if err != nil {
		return "", err
	}

	_, err = tx.ExecContext(ctx, "INSERT INTO trusted_devices (token_hash, created_at, last_used_at) VALUES (?, ?, ?)", hashToken(newDeviceToken), now, now)
	return newDeviceToken, err
}

// throttleKeys are the counters a login attempt is charged to.
func (s *Service) throttleKeys(ctx context.Context, ip, deviceToken string) ([]throttleKey, bool, error) {
	device, isTrusted, err := s.trustedDevice(ctx, deviceToken)
	if err != nil {
		return nil, false, err
	}
	if isTrusted {
		return []throttleKey{ipKey(ip), device}, true, nil
	}

	return []throttleKey{ipKey(ip), accountKey}, false, nil
}
