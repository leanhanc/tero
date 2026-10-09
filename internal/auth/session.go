package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Session limits (OWASP ASVS V7).
const (
	idleTimeout     = 30 * time.Minute
	absoluteTimeout = 12 * time.Hour
)

// SessionCookie is the cookie name. The __Host- prefix makes browsers require
// Secure and Path=/ and refuse a Domain attribute.
const SessionCookie = "__Host-tero_session"

func (s *Service) createSession(ctx context.Context, tx *sql.Tx) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}

	now := s.now().Unix()
	_, err = tx.ExecContext(ctx, "INSERT INTO sessions (token_hash, created_at, last_seen_at) VALUES (?, ?, ?)", hashToken(token), now, now)
	return token, err
}

// Authenticate checks a session token and marks the session as active. It
// returns ErrSessionExpired for unknown, idle or too old sessions.
func (s *Service) Authenticate(ctx context.Context, token string) error {
	if token == "" {
		return ErrSessionExpired
	}

	tokenHash := hashToken(token)
	var createdAt, lastSeenAt int64
	err := s.db.QueryRowContext(ctx, "SELECT created_at, last_seen_at FROM sessions WHERE token_hash = ?", tokenHash).Scan(&createdAt, &lastSeenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSessionExpired
	}
	if err != nil {
		return err
	}

	now := s.now()
	isIdle := now.Sub(time.Unix(lastSeenAt, 0)) >= idleTimeout
	isTooOld := now.Sub(time.Unix(createdAt, 0)) >= absoluteTimeout
	if isIdle || isTooOld {
		_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash)
		return errors.Join(ErrSessionExpired, err)
	}

	_, err = s.db.ExecContext(ctx, "UPDATE sessions SET last_seen_at = ? WHERE token_hash = ?", now.Unix(), tokenHash)
	return err
}

// Logout ends the session for token.
func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", hashToken(token))
	return err
}
