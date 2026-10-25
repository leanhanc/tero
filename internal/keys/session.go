package keys

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Session limits (OWASP ASVS V7). A session is created only here, after a
// login this service checked, so nothing running as tero can make one.
const (
	sessionIdleTimeout     = 30 * time.Minute
	sessionAbsoluteTimeout = 12 * time.Hour
	// confirmationLifetime is how long confirming it's you lets a session
	// end sessions and manage passkeys (OWASP ASVS v5.0.0-7.5.1).
	confirmationLifetime = 5 * time.Minute
	// lastSeenResolution spaces out the last-used writes, so most session
	// checks only read.
	lastSeenResolution = time.Minute
	// maxUserAgentBytes bounds what the session list stores per session.
	maxUserAgentBytes = 256
)

// DeviceLifetime is how long a browser stays trusted without a login.
const DeviceLifetime = 180 * 24 * time.Hour

// Login methods, as the session list shows them.
const (
	MethodPassword = "password"
	MethodPasskey  = "passkey"
	MethodSetup    = "setup"
)

// Browser is what the dashboard saw of the browser making a request.
type Browser struct {
	IP string `json:"ip"`
	// DeviceToken is the browser's trusted-device cookie, if any.
	DeviceToken string `json:"deviceToken"`
	UserAgent   string `json:"userAgent"`
}

// LoginResult is what a successful login or setup hands back for the
// browser's cookies.
type LoginResult struct {
	SessionToken string `json:"sessionToken"`
	// DeviceToken marks the browser as trusted.
	DeviceToken string `json:"deviceToken"`
}

// SessionState is what a session check tells the dashboard.
type SessionState struct {
	ID          string `json:"id"`
	IsConfirmed bool   `json:"isConfirmed"`
}

// SessionInfo is one entry of the session list. It never holds the token or
// its hash.
type SessionInfo struct {
	ID         string    `json:"id"`
	Method     string    `json:"method"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	LoginIP    string    `json:"loginIP"`
	LastIP     string    `json:"lastIP"`
	UserAgent  string    `json:"userAgent"`
	IsCurrent  bool      `json:"isCurrent"`
}

type sessionRow struct {
	id          string
	createdAt   time.Time
	lastSeenAt  time.Time
	lastIP      string
	confirmedAt time.Time
}

func (s *Service) isConfirmed(session sessionRow) bool {
	return s.now().Sub(session.confirmedAt) < confirmationLifetime
}

// startSession creates a session and trusts the browser in one transaction.
func (s *Service) startSession(ctx context.Context, method string, browser Browser, isTrustedDevice bool) (LoginResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LoginResult{}, err
	}
	defer tx.Rollback()

	sessionToken, err := s.createSession(ctx, tx, method, browser)
	if err != nil {
		return LoginResult{}, err
	}
	deviceToken, err := s.trustDevice(ctx, tx, browser.DeviceToken, isTrustedDevice)
	if err != nil {
		return LoginResult{}, err
	}

	return LoginResult{SessionToken: sessionToken, DeviceToken: deviceToken}, tx.Commit()
}

func (s *Service) createSession(ctx context.Context, tx *sql.Tx, method string, browser Browser) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	id, err := newID()
	if err != nil {
		return "", err
	}

	now := s.now().Unix()
	ip := cutIP(browser.IP)
	// A login or setup with the password and a code is the same check
	// confirming asks for, so the new session starts confirmed. Any other
	// method, a passkey login among them, starts unconfirmed: adding
	// passkeys needs the password and a code.
	var confirmedAt int64
	isPasswordAndCode := method == MethodPassword || method == MethodSetup
	if isPasswordAndCode {
		confirmedAt = now
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO sessions (id, token_hash, method, created_at, last_seen_at, login_ip, last_ip, user_agent, confirmed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, hashToken(token), method, now, now, ip, ip, cutUserAgent(browser.UserAgent), confirmedAt)
	return token, err
}

// CheckSession checks a session token for a dashboard request and marks the
// session as used from ip. Unknown, idle and too old sessions are
// ErrSessionEnded.
func (s *Service) CheckSession(ctx context.Context, token, ip string) (SessionState, error) {
	session, err := s.liveSession(ctx, token)
	if err != nil {
		return SessionState{}, err
	}

	now := s.now()
	ip = cutIP(ip)
	isStale := now.Sub(session.lastSeenAt) >= lastSeenResolution
	if isStale || ip != session.lastIP {
		_, err := s.db.ExecContext(ctx, "UPDATE sessions SET last_seen_at = ?, last_ip = ? WHERE id = ?", now.Unix(), ip, session.id)
		if err != nil {
			return SessionState{}, err
		}
	}

	return SessionState{ID: session.id, IsConfirmed: s.isConfirmed(session)}, nil
}

// liveSession loads the session for token without marking it used, and ends
// it when it has expired.
func (s *Service) liveSession(ctx context.Context, token string) (sessionRow, error) {
	if token == "" {
		return sessionRow{}, ErrSessionEnded
	}

	tokenHash := hashToken(token)
	var session sessionRow
	var createdAt, lastSeenAt, confirmedAt int64
	err := s.db.QueryRowContext(ctx, "SELECT id, created_at, last_seen_at, last_ip, confirmed_at FROM sessions WHERE token_hash = ?", tokenHash).
		Scan(&session.id, &createdAt, &lastSeenAt, &session.lastIP, &confirmedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return sessionRow{}, ErrSessionEnded
	}
	if err != nil {
		return sessionRow{}, err
	}
	session.createdAt = time.Unix(createdAt, 0)
	session.lastSeenAt = time.Unix(lastSeenAt, 0)
	session.confirmedAt = time.Unix(confirmedAt, 0)

	now := s.now()
	isIdle := now.Sub(session.lastSeenAt) >= sessionIdleTimeout
	isTooOld := now.Sub(session.createdAt) >= sessionAbsoluteTimeout
	if isIdle || isTooOld {
		_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE id = ?", session.id)
		return sessionRow{}, errors.Join(ErrSessionEnded, err)
	}

	return session, nil
}

// confirmedSession is liveSession for actions that need the admin to have
// confirmed it's them on this session within confirmationLifetime.
func (s *Service) confirmedSession(ctx context.Context, token string) (sessionRow, error) {
	session, err := s.liveSession(ctx, token)
	if err != nil {
		return sessionRow{}, err
	}
	if !s.isConfirmed(session) {
		return sessionRow{}, ErrNotConfirmed
	}

	return session, nil
}

// EndSession ends the session for token: a logout.
func (s *Service) EndSession(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}

	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", hashToken(token))
	return err
}

// ListSessions returns every live session, newest first, marking the one
// token belongs to.
func (s *Service) ListSessions(ctx context.Context, token string) ([]SessionInfo, error) {
	current, err := s.liveSession(ctx, token)
	if err != nil {
		return nil, err
	}

	now := s.now()
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, method, created_at, last_seen_at, login_ip, last_ip, user_agent FROM sessions
		WHERE last_seen_at > ? AND created_at > ?
		ORDER BY created_at DESC, id`,
		now.Add(-sessionIdleTimeout).Unix(), now.Add(-sessionAbsoluteTimeout).Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sessions := []SessionInfo{}
	for rows.Next() {
		var info SessionInfo
		var createdAt, lastSeenAt int64
		if err := rows.Scan(&info.ID, &info.Method, &createdAt, &lastSeenAt, &info.LoginIP, &info.LastIP, &info.UserAgent); err != nil {
			return nil, err
		}
		info.CreatedAt = time.Unix(createdAt, 0).UTC()
		info.LastSeenAt = time.Unix(lastSeenAt, 0).UTC()
		info.IsCurrent = info.ID == current.id
		sessions = append(sessions, info)
	}

	return sessions, rows.Err()
}

// RevokeSession ends the session with id, or every session but the caller's
// when isAllOthers is set. The caller's session must be confirmed (OWASP
// ASVS v5.0.0-7.5.2). It returns how many sessions ended.
func (s *Service) RevokeSession(ctx context.Context, token, ip, id string, isAllOthers bool) (int, error) {
	current, err := s.confirmedSession(ctx, token)
	if err != nil {
		return 0, err
	}

	query, args := "DELETE FROM sessions WHERE id = ?", []any{id}
	if isAllOthers {
		query, args = "DELETE FROM sessions WHERE id != ?", []any{current.id}
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}

	ended, _ := result.RowsAffected()
	if ended == 0 && !isAllOthers {
		return 0, ErrNotFound
	}

	return int(ended), s.recordEnded(ctx, ip, int(ended))
}

// endOtherSessions ends every session but keep, after a factor changed
// (OWASP ASVS v5.0.0-7.4.3).
func (s *Service) endOtherSessions(ctx context.Context, keep, ip string) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE id != ?", keep)
	if err != nil {
		return err
	}

	ended, _ := result.RowsAffected()
	return s.recordEnded(ctx, ip, int(ended))
}

func (s *Service) recordEnded(ctx context.Context, ip string, count int) error {
	if count == 0 {
		return nil
	}

	return s.record(ctx, Event{At: s.now(), Type: EventSessionsEnded, Source: SourceKeys, IP: ip, Count: count})
}

// trustedDevice returns the throttle key for deviceToken, or false when the
// token is missing, unknown or stale. A trusted-device cookie grants nothing
// by itself: it only moves that browser's failed logins from the
// account-wide throttle to a per-device one (OWASP's device cookie pattern).
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

// throttleKeys are the counters a password and code check from browser is
// charged to.
func (s *Service) throttleKeys(ctx context.Context, browser Browser) ([]throttleKey, bool, error) {
	device, isTrusted, err := s.trustedDevice(ctx, browser.DeviceToken)
	if err != nil {
		return nil, false, err
	}
	if isTrusted {
		return []throttleKey{ipKey(browser.IP), device}, true, nil
	}

	return []throttleKey{ipKey(browser.IP), accountKey}, false, nil
}

// cutUserAgent keeps at most maxUserAgentBytes of valid UTF-8.
func cutUserAgent(userAgent string) string {
	if len(userAgent) > maxUserAgentBytes {
		userAgent = userAgent[:maxUserAgentBytes]
	}

	return strings.ToValidUTF8(userAgent, "")
}

func cutIP(ip string) string {
	if len(ip) > maxIPLength {
		return ip[:maxIPLength]
	}

	return ip
}
