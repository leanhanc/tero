package keys

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"strconv"
	"strings"
	"time"
)

// The GitHub App this server created holds two secrets: its private key,
// which signs App tokens, and its webhook secret, which signs every webhook.
// Both are sealed here and never leave the key service. The agent, which
// alone talks to GitHub with them, gets short-lived App tokens; the
// dashboard, which receives webhooks, gets only whether a signature is
// valid.

var (
	ErrGitHubConnected    = errors.New("GitHub is already connected")
	ErrGitHubNotConnected = errors.New("GitHub isn't connected")
	ErrInvalidConnection  = errors.New("this GitHub connection wasn't started here, expired or was already used")
	ErrInvalidAppKey      = errors.New("the GitHub App's private key isn't an RSA key")
)

const (
	// connectionLifetime bounds how long the admin has to create and
	// install the App on GitHub after starting.
	connectionLifetime = 10 * time.Minute
	// appTokenLifetime is how long an App token is valid. GitHub accepts up
	// to 10 minutes; the agent asks for one per use.
	appTokenLifetime = 5 * time.Minute
	// appTokenClockSkew backdates a token's issue time, as GitHub
	// recommends, so a server clock slightly ahead of GitHub's still works.
	appTokenClockSkew = 60 * time.Second
	// MaxWebhookBody is the largest webhook body the key service checks.
	// A push lists at most 20 commits, so a real one is far smaller.
	MaxWebhookBody = 1 << 20
	maxAppKeyBytes = 16 << 10
	maxSecretBytes = 256
	maxSlugBytes   = 128
)

// The purposes the App's two secrets are sealed under.
const (
	gitHubKeyPurpose    = "github_app.private_key.1"
	gitHubSecretPurpose = "github_app.webhook_secret.1"
)

// GitHubApp is the connected App, without its secrets.
type GitHubApp struct {
	IsConnected bool      `json:"isConnected"`
	AppID       int64     `json:"appId,omitempty"`
	Slug        string    `json:"slug,omitempty"`
	Owner       string    `json:"owner,omitempty"`
	IsOrgOwned  bool      `json:"isOrgOwned,omitempty"`
	ConnectedAt time.Time `json:"connectedAt,omitzero"`
}

// GitHubAppSecrets is what the agent hands over from GitHub's manifest
// conversion, with the state the admin approved.
type GitHubAppSecrets struct {
	State         string `json:"state"`
	AppID         int64  `json:"appId"`
	Slug          string `json:"slug"`
	Owner         string `json:"owner"`
	IsOrgOwned    bool   `json:"isOrgOwned"`
	PrivateKey    string `json:"privateKey"`
	WebhookSecret string `json:"webhookSecret"`
}

// AppToken is a signed App token (JWT) for GitHub's API.
type AppToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// BeginGitHubConnection starts connecting GitHub on a confirmed session and
// returns the state to send through GitHub's manifest flow. Only one App per
// server: it refuses while one is connected.
func (s *Service) BeginGitHubConnection(ctx context.Context, token string) (string, error) {
	session, err := s.confirmedSession(ctx, token)
	if err != nil {
		return "", err
	}

	app, err := s.GitHubStatus(ctx)
	if err != nil {
		return "", err
	}
	if app.IsConnected {
		return "", ErrGitHubConnected
	}

	state, err := newToken()
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx,
		"INSERT INTO github_connections (state_hash, session_id, expires_at) VALUES (?, ?, ?)",
		hashToken(state), session.id, s.now().Add(connectionLifetime).Unix())

	return state, err
}

// ApproveGitHubConnection marks a state the same live session began as the
// admin's own, when GitHub sends the admin back. The 5-minute confirmation
// window doesn't apply: creating and installing the App can take longer.
func (s *Service) ApproveGitHubConnection(ctx context.Context, token, state string) error {
	session, err := s.liveSession(ctx, token)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx,
		"UPDATE github_connections SET approved_at = ? WHERE state_hash = ? AND session_id = ? AND expires_at > ? AND approved_at = 0",
		s.now().Unix(), hashToken(state), session.id, s.now().Unix())
	if err != nil {
		return err
	}

	return requireOneRow(result, ErrInvalidConnection)
}

// ConnectGitHub stores the App the agent created from an approved state.
// The state works once, and only within its lifetime.
func (s *Service) ConnectGitHub(ctx context.Context, secrets GitHubAppSecrets) error {
	if err := checkAppSecrets(secrets); err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx,
		"DELETE FROM github_connections WHERE state_hash = ? AND approved_at > 0 AND expires_at > ?",
		hashToken(secrets.State), s.now().Unix())
	if err != nil {
		return err
	}
	if err := requireOneRow(result, ErrInvalidConnection); err != nil {
		return err
	}

	if err := s.insertGitHubApp(ctx, tx, secrets); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	return s.recordOwn(ctx, EventGitHubConnected, "")
}

func (s *Service) insertGitHubApp(ctx context.Context, tx *sql.Tx, secrets GitHubAppSecrets) error {
	privateKey, err := s.keys.seal(gitHubKeyPurpose, []byte(secrets.PrivateKey))
	if err != nil {
		return err
	}
	webhookSecret, err := s.keys.seal(gitHubSecretPurpose, []byte(secrets.WebhookSecret))
	if err != nil {
		return err
	}
	if privateKey.version != webhookSecret.version {
		return errors.New("the master key changed while sealing the App's secrets")
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO github_app (id, app_id, slug, owner, is_org_owned, key_version, private_key, webhook_secret, connected_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)`,
		secrets.AppID, secrets.Slug, secrets.Owner, secrets.IsOrgOwned, privateKey.version,
		privateKey.ciphertext, webhookSecret.ciphertext, s.now().Unix())
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return ErrGitHubConnected
	}

	return err
}

// checkAppSecrets refuses values GitHub never sends, before anything is
// stored. The private key must be an RSA key, since App tokens are RS256.
func checkAppSecrets(secrets GitHubAppSecrets) error {
	isValid := secrets.AppID > 0 &&
		secrets.Slug != "" && len(secrets.Slug) <= maxSlugBytes &&
		secrets.Owner != "" && len(secrets.Owner) <= maxSlugBytes &&
		secrets.WebhookSecret != "" && len(secrets.WebhookSecret) <= maxSecretBytes &&
		len(secrets.PrivateKey) <= maxAppKeyBytes
	if !isValid {
		return ErrRefused
	}

	_, err := parseAppKey([]byte(secrets.PrivateKey))
	return err
}

// DisconnectGitHub deletes the App's secrets on a confirmed session.
func (s *Service) DisconnectGitHub(ctx context.Context, token string) error {
	if _, err := s.confirmedSession(ctx, token); err != nil {
		return err
	}

	return s.ForgetGitHub(ctx)
}

// ForgetGitHub deletes the App's secrets and every unfinished connection.
// Root uses it in assume-breach.
func (s *Service) ForgetGitHub(ctx context.Context) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM github_app")
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM github_connections"); err != nil {
		return err
	}

	deleted, err := result.RowsAffected()
	if err != nil || deleted == 0 {
		return err
	}

	return s.recordOwn(ctx, EventGitHubDisconnected, "")
}

// GitHubStatus describes the connected App without its secrets.
func (s *Service) GitHubStatus(ctx context.Context) (GitHubApp, error) {
	app := GitHubApp{IsConnected: true}
	var connectedAt int64
	err := s.db.QueryRowContext(ctx, "SELECT app_id, slug, owner, is_org_owned, connected_at FROM github_app WHERE id = 1").
		Scan(&app.AppID, &app.Slug, &app.Owner, &app.IsOrgOwned, &connectedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return GitHubApp{}, nil
	}
	if err != nil {
		return GitHubApp{}, err
	}

	app.ConnectedAt = time.Unix(connectedAt, 0).UTC()
	return app, nil
}

// SignAppToken returns a new App token, signed with the App's private key.
func (s *Service) SignAppToken(ctx context.Context) (AppToken, error) {
	var appID int64
	var value sealed
	err := s.db.QueryRowContext(ctx, "SELECT app_id, key_version, private_key FROM github_app WHERE id = 1").
		Scan(&appID, &value.version, &value.ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return AppToken{}, ErrGitHubNotConnected
	}
	if err != nil {
		return AppToken{}, err
	}

	pemKey, err := s.openSealed(gitHubKeyPurpose, value)
	if err != nil {
		return AppToken{}, err
	}
	key, err := parseAppKey(pemKey)
	if err != nil {
		return AppToken{}, err
	}

	issuedAt := s.now().Add(-appTokenClockSkew)
	expiresAt := s.now().Add(appTokenLifetime)
	token, err := signJWT(key, appID, issuedAt, expiresAt)
	return AppToken{Token: token, ExpiresAt: expiresAt.UTC()}, err
}

// VerifyWebhook reports whether signature, GitHub's X-Hub-Signature-256
// header, is the HMAC-SHA256 of body with the App's webhook secret. Without
// a connected App nothing is valid. A failure is recorded with the sender's
// IP.
func (s *Service) VerifyWebhook(ctx context.Context, body []byte, signature, ip string) (bool, error) {
	if len(body) > MaxWebhookBody {
		return false, ErrRefused
	}

	isValid, err := s.checkWebhookSignature(ctx, body, signature)
	if err != nil || isValid {
		return isValid, err
	}

	return false, s.recordOwn(ctx, EventWebhookSignatureFailed, ip)
}

func (s *Service) checkWebhookSignature(ctx context.Context, body []byte, signature string) (bool, error) {
	var value sealed
	err := s.db.QueryRowContext(ctx, "SELECT key_version, webhook_secret FROM github_app WHERE id = 1").
		Scan(&value.version, &value.ciphertext)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	secret, err := s.openSealed(gitHubSecretPurpose, value)
	if err != nil {
		return false, err
	}

	given, hasPrefix := strings.CutPrefix(signature, "sha256=")
	givenMAC, decodeErr := hex.DecodeString(given)
	if !hasPrefix || decodeErr != nil {
		return false, nil
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hmac.Equal(givenMAC, mac.Sum(nil)), nil
}

func (s *Service) openSealed(purpose string, value sealed) ([]byte, error) {
	plaintext, err := s.keys.open(purpose, value)
	if errors.Is(err, errNoKey) {
		return nil, ErrKeyNotLoaded
	}

	return plaintext, err
}

// resealGitHubApp re-seals the App's secrets under the newest key.
func (s *Service) resealGitHubApp(ctx context.Context, tx *sql.Tx) error {
	var version int
	var privateKey, webhookSecret []byte
	err := tx.QueryRowContext(ctx, "SELECT key_version, private_key, webhook_secret FROM github_app WHERE id = 1").
		Scan(&version, &privateKey, &webhookSecret)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}

	resealedKey, err := s.reseal(gitHubKeyPurpose, sealed{version: version, ciphertext: privateKey})
	if err != nil {
		return err
	}
	resealedSecret, err := s.reseal(gitHubSecretPurpose, sealed{version: version, ciphertext: webhookSecret})
	if err != nil {
		return err
	}

	_, err = tx.ExecContext(ctx, "UPDATE github_app SET key_version = ?, private_key = ?, webhook_secret = ? WHERE id = 1",
		resealedKey.version, resealedKey.ciphertext, resealedSecret.ciphertext)
	return err
}

// parseAppKey reads the PEM private key GitHub issues: PKCS #1, or PKCS #8
// for keys downloaded later.
func parseAppKey(pemKey []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemKey)
	if block == nil {
		return nil, ErrInvalidAppKey
	}

	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, ErrInvalidAppKey
	}
	key, isRSA := parsed.(*rsa.PrivateKey)
	if !isRSA {
		return nil, ErrInvalidAppKey
	}

	return key, nil
}

// jwtHeader is the only header Tero signs: RS256, as GitHub requires.
var jwtHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))

// signJWT signs a GitHub App token: the App id as issuer, and its issue and
// expiry times.
func signJWT(key *rsa.PrivateKey, appID int64, issuedAt, expiresAt time.Time) (string, error) {
	claims, err := json.Marshal(map[string]any{
		"iss": strconv.FormatInt(appID, 10),
		"iat": issuedAt.Unix(),
		"exp": expiresAt.Unix(),
	})
	if err != nil {
		return "", err
	}

	signingInput := jwtHeader + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

// requireOneRow returns notFound unless the statement changed exactly one
// row.
func requireOneRow(result sql.Result, notFound error) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return notFound
	}

	return nil
}
