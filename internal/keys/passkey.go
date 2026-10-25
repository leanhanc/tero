package keys

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const (
	// maxPasskeys bounds the passkeys one admin can add.
	maxPasskeys = 10
	// maxPasskeyName is in characters.
	maxPasskeyName = 64
	// userHandleSize is the WebAuthn user handle's length, the most the
	// standard allows.
	userHandleSize = 64
)

// PasskeyOptions is what the browser passes to navigator.credentials: the
// public-key options as JSON, and the id that finishes this ceremony.
type PasskeyOptions struct {
	ChallengeID string          `json:"challengeId"`
	Options     json.RawMessage `json:"options"`
}

// PasskeyInfo is one entry of the passkey list.
type PasskeyInfo struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt,omitzero"`
}

// newRelyingParty binds passkeys to the dashboard's domain. The domain comes
// from root's configuration, never from a request, so a compromised
// dashboard can't make this service accept a passkey made for another site.
// Without a domain, passkeys are off.
func newRelyingParty(domain string) (*webauthn.WebAuthn, error) {
	if domain == "" {
		return nil, nil
	}

	timeout := webauthn.TimeoutConfig{Enforce: true, Timeout: challengeLifetime, TimeoutUVD: challengeLifetime}
	return webauthn.New(&webauthn.Config{
		RPID:                  domain,
		RPDisplayName:         "Tero",
		RPOrigins:             []string{"https://" + domain},
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		},
		Timeouts: webauthn.TimeoutsConfig{Login: timeout, Registration: timeout},
	})
}

// admin is the single account as WebAuthn sees it.
type admin struct {
	userHandle  []byte
	credentials []webauthn.Credential
}

func (a admin) WebAuthnID() []byte                         { return a.userHandle }
func (a admin) WebAuthnName() string                       { return "admin" }
func (a admin) WebAuthnDisplayName() string                { return "Tero admin" }
func (a admin) WebAuthnCredentials() []webauthn.Credential { return a.credentials }

// loadAdmin returns the admin's user handle and passkeys.
func (s *Service) loadAdmin(ctx context.Context) (admin, error) {
	var account admin
	err := s.db.QueryRowContext(ctx, "SELECT webauthn_user_id FROM admin WHERE id = 1").Scan(&account.userHandle)
	if errors.Is(err, sql.ErrNoRows) {
		return admin{}, ErrNotSetUp
	}
	if err != nil {
		return admin{}, err
	}

	rows, err := s.db.QueryContext(ctx, "SELECT credential FROM passkeys ORDER BY created_at")
	if err != nil {
		return admin{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var encoded string
		if err := rows.Scan(&encoded); err != nil {
			return admin{}, err
		}
		var credential webauthn.Credential
		if err := json.Unmarshal([]byte(encoded), &credential); err != nil {
			return admin{}, err
		}
		account.credentials = append(account.credentials, credential)
	}

	return account, rows.Err()
}

// BeginPasskeyRegistration starts adding a passkey on a confirmed session.
// The authenticator is asked for a discoverable credential with user
// verification, and passkeys the admin already has are excluded.
func (s *Service) BeginPasskeyRegistration(ctx context.Context, token string) (PasskeyOptions, error) {
	if s.passkeys == nil {
		return PasskeyOptions{}, ErrRefused
	}
	session, err := s.confirmedSession(ctx, token)
	if err != nil {
		return PasskeyOptions{}, err
	}
	account, err := s.loadAdmin(ctx)
	if err != nil {
		return PasskeyOptions{}, err
	}
	if len(account.credentials) >= maxPasskeys {
		return PasskeyOptions{}, ErrPasskeyLimit
	}

	exclusions := webauthn.Credentials(account.credentials).CredentialDescriptors()
	creation, data, err := s.passkeys.BeginRegistration(account, webauthn.WithExclusions(exclusions))
	if err != nil {
		return PasskeyOptions{}, err
	}

	entry := challenge{kind: challengeRegister, sessionID: session.id, data: *data, expiresAt: s.now().Add(challengeLifetime)}
	return s.issueChallenge(entry, creation.Response)
}

// FinishPasskeyRegistration checks the browser's answer and stores the new
// passkey under name, then ends every other session when isEndingOthers is
// set (OWASP ASVS v5.0.0-7.4.3).
func (s *Service) FinishPasskeyRegistration(ctx context.Context, token, ip, challengeID, name string, answer json.RawMessage, isEndingOthers bool) (PasskeyInfo, error) {
	if s.passkeys == nil {
		return PasskeyInfo{}, ErrRefused
	}
	session, err := s.confirmedSession(ctx, token)
	if err != nil {
		return PasskeyInfo{}, err
	}
	name, err = passkeyName(name)
	if err != nil {
		return PasskeyInfo{}, err
	}
	entry, isUsable := s.challenges.take(challengeID, challengeRegister, session.id, s.now())
	if !isUsable {
		return PasskeyInfo{}, ErrInvalidPasskey
	}
	account, err := s.loadAdmin(ctx)
	if err != nil {
		return PasskeyInfo{}, err
	}

	parsed, err := protocol.ParseCredentialCreationResponseBytes(answer)
	if err != nil {
		return PasskeyInfo{}, ErrInvalidPasskey
	}
	credential, err := s.passkeys.CreateCredential(account, entry.data, parsed)
	if err != nil || !credential.Flags.UserVerified {
		return PasskeyInfo{}, ErrInvalidPasskey
	}

	info, err := s.storePasskey(ctx, name, credential)
	if err != nil {
		return PasskeyInfo{}, err
	}
	if err := s.recordOwn(ctx, EventPasskeyAdded, ip); err != nil {
		return PasskeyInfo{}, err
	}
	if !isEndingOthers {
		return info, nil
	}

	return info, s.endOtherSessions(ctx, session.id, ip)
}

// storePasskey inserts credential, re-checking the limit in the same
// transaction so two registrations can't both pass it.
func (s *Service) storePasskey(ctx context.Context, name string, credential *webauthn.Credential) (PasskeyInfo, error) {
	encoded, err := json.Marshal(credential)
	if err != nil {
		return PasskeyInfo{}, err
	}
	id, err := newID()
	if err != nil {
		return PasskeyInfo{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return PasskeyInfo{}, err
	}
	defer tx.Rollback()

	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM passkeys").Scan(&count); err != nil {
		return PasskeyInfo{}, err
	}
	if count >= maxPasskeys {
		return PasskeyInfo{}, ErrPasskeyLimit
	}
	var isKnown bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM passkeys WHERE credential_id = ?)", credential.ID).Scan(&isKnown); err != nil {
		return PasskeyInfo{}, err
	}
	// An authenticator that ignores the exclusion list registers a passkey
	// that is already here.
	if isKnown {
		return PasskeyInfo{}, ErrInvalidPasskey
	}

	now := s.now()
	_, err = tx.ExecContext(ctx, "INSERT INTO passkeys (id, credential_id, credential, name, created_at) VALUES (?, ?, ?, ?, ?)",
		id, credential.ID, string(encoded), name, now.Unix())
	if err != nil {
		return PasskeyInfo{}, err
	}

	return PasskeyInfo{ID: id, Name: name, CreatedAt: now.UTC()}, tx.Commit()
}

// ListPasskeys returns the admin's passkeys, oldest first.
func (s *Service) ListPasskeys(ctx context.Context, token string) ([]PasskeyInfo, error) {
	if _, err := s.liveSession(ctx, token); err != nil {
		return nil, err
	}

	rows, err := s.db.QueryContext(ctx, "SELECT id, name, created_at, last_used_at FROM passkeys ORDER BY created_at, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	passkeys := []PasskeyInfo{}
	for rows.Next() {
		var info PasskeyInfo
		var createdAt, lastUsedAt int64
		if err := rows.Scan(&info.ID, &info.Name, &createdAt, &lastUsedAt); err != nil {
			return nil, err
		}
		info.CreatedAt = time.Unix(createdAt, 0).UTC()
		if lastUsedAt > 0 {
			info.LastUsedAt = time.Unix(lastUsedAt, 0).UTC()
		}
		passkeys = append(passkeys, info)
	}

	return passkeys, rows.Err()
}

// RenamePasskey renames a passkey on a confirmed session.
func (s *Service) RenamePasskey(ctx context.Context, token, id, name string) error {
	if _, err := s.confirmedSession(ctx, token); err != nil {
		return err
	}
	name, err := passkeyName(name)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, "UPDATE passkeys SET name = ? WHERE id = ?", name, id)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrNotFound
	}

	return nil
}

// RemovePasskey removes a passkey on a confirmed session, then ends every
// other session when isEndingOthers is set (OWASP ASVS v5.0.0-7.4.3).
// Removing the last one is allowed: password and TOTP always remain.
func (s *Service) RemovePasskey(ctx context.Context, token, ip, id string, isEndingOthers bool) error {
	session, err := s.confirmedSession(ctx, token)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, "DELETE FROM passkeys WHERE id = ?", id)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrNotFound
	}
	if err := s.recordOwn(ctx, EventPasskeyRemoved, ip); err != nil {
		return err
	}
	if !isEndingOthers {
		return nil
	}

	return s.endOtherSessions(ctx, session.id, ip)
}

// BeginPasskeyLogin starts a usernameless passkey login. Anyone can ask, so
// only an IP that is already locked out is refused here, and the challenges
// kept are capped.
func (s *Service) BeginPasskeyLogin(ctx context.Context, ip string) (PasskeyOptions, error) {
	if s.passkeys == nil {
		return PasskeyOptions{}, ErrRefused
	}
	if err := s.checkThrottle(ctx, []throttleKey{ipKey(ip)}); err != nil {
		return PasskeyOptions{}, err
	}

	assertion, data, err := s.passkeys.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return PasskeyOptions{}, err
	}

	entry := challenge{kind: challengeLogin, requester: ipKey(ip).name, data: *data, expiresAt: s.now().Add(challengeLifetime)}
	return s.issueChallenge(entry, assertion.Response)
}

// LoginPasskey checks a passkey answer and starts a session. A failed answer
// is charged to the IP only: a signature can't be guessed, and charging the
// account would let strangers lock the admin out. The code guessing lock
// doesn't apply, since it is about someone holding the password.
func (s *Service) LoginPasskey(ctx context.Context, browser Browser, challengeID string, answer json.RawMessage) (LoginResult, error) {
	if s.passkeys == nil {
		return LoginResult{}, ErrRefused
	}
	_, isTrustedDevice, err := s.trustedDevice(ctx, browser.DeviceToken)
	if err != nil {
		return LoginResult{}, err
	}

	throttles := []throttleKey{ipKey(browser.IP)}
	err = s.checkThrottled(ctx, throttles, browser.IP, func(ctx context.Context) error {
		return s.checkPasskey(ctx, browser.IP, challengeID, answer)
	})
	if err != nil {
		return LoginResult{}, err
	}

	return s.startSession(ctx, MethodPasskey, browser, isTrustedDevice)
}

// checkPasskey verifies an assertion against the open challenge and the
// admin's passkeys, and stores the passkey's new counter and last use. A
// challenge that is gone, expired or of another kind says nothing about the
// passkey, so it isn't charged: otherwise a flood of challenges that pushes
// out the admin's would lock out the admin's IP.
func (s *Service) checkPasskey(ctx context.Context, ip, challengeID string, answer json.RawMessage) error {
	account, err := s.loadAdmin(ctx)
	if err != nil {
		return err
	}
	entry, isUsable := s.challenges.take(challengeID, challengeLogin, "", s.now())
	if !isUsable {
		return ErrInvalidPasskey
	}
	if !s.limits.allowPasskeyCheck(s.now()) {
		return ErrHashingIsBusy
	}

	parsed, err := protocol.ParseCredentialRequestResponseBytes(answer)
	if err != nil {
		return s.failPasskey(ctx, ip, EventPasskeyLoginFailed)
	}
	findAdmin := func(_, userHandle []byte) (webauthn.User, error) {
		if subtle.ConstantTimeCompare(userHandle, account.userHandle) != 1 {
			return nil, errors.New("unknown user handle")
		}
		return account, nil
	}
	_, credential, err := s.passkeys.ValidatePasskeyLogin(findAdmin, entry.data, parsed)
	if err != nil {
		return s.failPasskey(ctx, ip, EventPasskeyLoginFailed)
	}
	// A counter that went backwards means the passkey may have been copied.
	if credential.Authenticator.CloneWarning {
		return s.failPasskey(ctx, ip, EventPasskeyCounterMismatch)
	}

	if err := s.updatePasskey(ctx, credential); err != nil {
		return err
	}

	return s.recordOwn(ctx, EventPasskeyLoginSucceeded, ip)
}

func (s *Service) failPasskey(ctx context.Context, ip string, eventType EventType) error {
	if err := s.recordOwn(ctx, eventType, ip); err != nil {
		return err
	}

	return ErrWrongLogin
}

func (s *Service) updatePasskey(ctx context.Context, credential *webauthn.Credential) error {
	encoded, err := json.Marshal(credential)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx, "UPDATE passkeys SET credential = ?, last_used_at = ? WHERE credential_id = ?",
		string(encoded), s.now().Unix(), credential.ID)
	return err
}

// issueChallenge keeps entry and returns the options with its id.
func (s *Service) issueChallenge(entry challenge, options any) (PasskeyOptions, error) {
	encoded, err := json.Marshal(options)
	if err != nil {
		return PasskeyOptions{}, err
	}
	id, err := s.challenges.add(entry, s.now())
	if err != nil {
		return PasskeyOptions{}, err
	}

	return PasskeyOptions{ChallengeID: id, Options: encoded}, nil
}

// passkeyName trims name and checks its length.
func passkeyName(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	length := utf8.RuneCountInString(trimmed)
	isValid := utf8.ValidString(trimmed) && length >= 1 && length <= maxPasskeyName
	if !isValid || strings.ContainsFunc(trimmed, isControl) {
		return "", ErrInvalidName
	}

	return trimmed, nil
}

func isControl(r rune) bool {
	return r < 0x20 || r == 0x7f
}

// newUserHandle returns the random WebAuthn user handle for a new admin.
// Passkeys are made for it, so a reset, which creates a new admin, leaves
// old passkeys pointing at a handle that no longer exists.
func newUserHandle() ([]byte, error) {
	handle := make([]byte, userHandleSize)
	if _, err := rand.Read(handle); err != nil {
		return nil, err
	}

	return handle, nil
}
