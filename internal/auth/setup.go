package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/leanhanc/tero/internal/password"
	"github.com/leanhanc/tero/internal/totp"
)

// setupLinkLifetime bounds how long a printed link works (OWASP ASVS
// v5.0.0-6.4.1). `sudo tero reset-login` prints a fresh one.
const setupLinkLifetime = 24 * time.Hour

// Enrolment is what the setup page shows so the admin can add Tero to their
// authenticator app.
type Enrolment struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

// IssueSetupLink replaces any unused setup link with a new one and returns
// its URL. The token is in the URL fragment, so browsers never send it in
// requests, logs or Referer headers; the dashboard reads it from there.
func (s *Service) IssueSetupLink(ctx context.Context, ip string) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	now := s.now()
	if _, err := tx.ExecContext(ctx, "DELETE FROM setup_links WHERE used_at IS NULL"); err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO setup_links (token_hash, issued_at, expires_at) VALUES (?, ?, ?)",
		hashToken(token), now.Unix(), now.Add(setupLinkLifetime).Unix())
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}

	if err := s.record(ctx, Event{At: now, Type: EventSetupLinkIssued, IP: ip}); err != nil {
		return "", err
	}

	return "https://" + s.domain + "/setup#" + token, nil
}

// BeginSetup checks the link and the chosen password, then returns a new
// TOTP secret to enrol. Nothing is created until FinishSetup confirms a code
// from that secret.
func (s *Service) BeginSetup(ctx context.Context, ip, token, newPassword string) (Enrolment, error) {
	if err := s.checkThrottle(ctx, []throttleKey{ipKey(ip)}); err != nil {
		return Enrolment{}, err
	}
	if _, err := s.usableLink(ctx, token); err != nil {
		return Enrolment{}, err
	}

	result, err := s.passwords.Check(ctx, newPassword)
	if err != nil {
		return Enrolment{}, err
	}
	if result.IsOnlineCheckSkipped {
		if err := s.record(ctx, Event{At: s.now(), Type: EventBreachCheckSkipped, IP: ip}); err != nil {
			return Enrolment{}, err
		}
	}

	passwordHash, err := password.Hash(ctx, newPassword)
	if err != nil {
		return Enrolment{}, err
	}
	secret, err := totp.NewSecret()
	if err != nil {
		return Enrolment{}, err
	}

	_, err = s.db.ExecContext(ctx,
		"UPDATE setup_links SET pending_password_hash = ?, pending_totp_secret = ? WHERE token_hash = ? AND used_at IS NULL",
		passwordHash, secret, hashToken(token))
	if err != nil {
		return Enrolment{}, err
	}

	return Enrolment{Secret: secret, URI: totp.URI(secret, "Tero", "admin@"+s.domain)}, nil
}

// FinishSetup confirms a TOTP code from the enrolled secret, creates the admin
// account, uses up the link and logs the admin in, trusting this browser.
// A wrong code is charged to the throttle like a failed login, so the
// six-digit code can't be guessed at speed.
func (s *Service) FinishSetup(ctx context.Context, ip, token, code string) (LoginResult, error) {
	keys := []throttleKey{ipKey(ip), accountKey}
	if err := s.checkThrottle(ctx, keys); err != nil {
		return LoginResult{}, err
	}

	link, err := s.usableLink(ctx, token)
	if err != nil {
		return LoginResult{}, err
	}
	hasPendingCredentials := link.passwordHash.Valid && link.totpSecret.Valid
	if !hasPendingCredentials {
		return LoginResult{}, ErrInvalidLink
	}

	charged, err := s.chargeAttempt(ctx, keys)
	if err != nil {
		return LoginResult{}, err
	}

	step, isValid := totp.Verify(link.totpSecret.String, code, s.now(), 0)
	if !isValid {
		if err := s.recordFailedAttempt(ctx, ip, charged, EventSetupCodeFailed); err != nil {
			return LoginResult{}, err
		}
		return LoginResult{}, ErrInvalidCode
	}

	result, err := s.createAdmin(ctx, token, link, step)
	if err != nil {
		return LoginResult{}, err
	}

	if err := s.forgive(ctx, charged); err != nil {
		return LoginResult{}, err
	}
	if err := s.record(ctx, Event{At: s.now(), Type: EventSetupCompleted, IP: ip}); err != nil {
		return LoginResult{}, err
	}

	return result, nil
}

func (s *Service) createAdmin(ctx context.Context, token string, link setupLink, step int64) (LoginResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LoginResult{}, err
	}
	defer tx.Rollback()

	now := s.now().Unix()
	_, err = tx.ExecContext(ctx,
		"INSERT INTO admin (id, password_hash, totp_secret, last_totp_step, created_at) VALUES (1, ?, ?, ?, ?)",
		link.passwordHash.String, link.totpSecret.String, step, now)
	if err != nil {
		return LoginResult{}, ErrAlreadySetUp
	}

	result, err := tx.ExecContext(ctx,
		"UPDATE setup_links SET used_at = ?, pending_password_hash = NULL, pending_totp_secret = NULL WHERE token_hash = ? AND used_at IS NULL",
		now, hashToken(token))
	if err != nil {
		return LoginResult{}, err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return LoginResult{}, ErrInvalidLink
	}

	sessionToken, err := s.createSession(ctx, tx)
	if err != nil {
		return LoginResult{}, err
	}
	deviceToken, err := s.trustDevice(ctx, tx, "", false)
	if err != nil {
		return LoginResult{}, err
	}

	return LoginResult{SessionToken: sessionToken, DeviceToken: deviceToken}, tx.Commit()
}

type setupLink struct {
	passwordHash sql.NullString
	totpSecret   sql.NullString
}

// usableLink loads the link for token when it is unused, unexpired, and no
// admin exists yet.
func (s *Service) usableLink(ctx context.Context, token string) (setupLink, error) {
	isSetUp, err := s.IsSetUp(ctx)
	if err != nil {
		return setupLink{}, err
	}
	if isSetUp || token == "" {
		return setupLink{}, ErrInvalidLink
	}

	var link setupLink
	var expiresAt int64
	var usedAt sql.NullInt64
	err = s.db.QueryRowContext(ctx,
		"SELECT expires_at, used_at, pending_password_hash, pending_totp_secret FROM setup_links WHERE token_hash = ?",
		hashToken(token)).Scan(&expiresAt, &usedAt, &link.passwordHash, &link.totpSecret)
	if errors.Is(err, sql.ErrNoRows) {
		return setupLink{}, ErrInvalidLink
	}
	if err != nil {
		return setupLink{}, err
	}

	isExpired := !s.now().Before(time.Unix(expiresAt, 0))
	if usedAt.Valid || isExpired {
		return setupLink{}, ErrInvalidLink
	}

	return link, nil
}
