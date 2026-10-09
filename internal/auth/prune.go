package auth

import (
	"context"
	"time"
)

// eventRetention is how long security events are kept.
const eventRetention = 365 * 24 * time.Hour

// Prune deletes what can no longer be used: expired sessions, unused setup
// links past their expiry, stale trusted devices, throttle counters that are
// both forgotten and unlocked, and events older than a year. Expired rows
// are already refused when presented; pruning only keeps the tables small.
func (s *Service) Prune(ctx context.Context) error {
	now := s.now()
	statements := []struct {
		query string
		args  []any
	}{
		{"DELETE FROM sessions WHERE last_seen_at <= ? OR created_at <= ?",
			[]any{now.Add(-idleTimeout).Unix(), now.Add(-absoluteTimeout).Unix()}},
		{"DELETE FROM setup_links WHERE used_at IS NULL AND expires_at <= ?",
			[]any{now.Unix()}},
		{"DELETE FROM trusted_devices WHERE last_used_at < ?",
			[]any{now.Add(-DeviceLifetime).Unix()}},
		{"DELETE FROM login_throttles WHERE last_failure < ? AND locked_until <= ?",
			[]any{now.Add(-failureMemory).Unix(), now.Unix()}},
		{"DELETE FROM security_events WHERE at < ?",
			[]any{now.Add(-eventRetention).Unix()}},
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement.query, statement.args...); err != nil {
			return err
		}
	}

	return nil
}
