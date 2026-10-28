package keys

import (
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

const testWebhookSecret = "a webhook secret from GitHub"

// testAppKey is generated once: RSA key generation is slow.
var testAppKey = func() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return key
}()

func testAppKeyPEM() string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(testAppKey)}))
}

func (env testEnv) agent() *Client {
	return NewLocalClient(env.service, CallerAgent)
}

// connectGitHub walks the whole connection: begin on the setup's confirmed
// session, approve on return from GitHub, and store from the agent.
func (env testEnv) connectGitHub(t *testing.T) {
	t.Helper()
	ctx := context.Background()

	state, err := env.dashboard.BeginGitHubConnection(ctx, *env.session)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.dashboard.ApproveGitHubConnection(ctx, *env.session, state); err != nil {
		t.Fatal(err)
	}
	if err := env.agent().ConnectGitHub(ctx, testAppSecrets(state)); err != nil {
		t.Fatal(err)
	}
}

func testAppSecrets(state string) GitHubAppSecrets {
	return GitHubAppSecrets{
		State: state, AppID: 4242, Slug: "tero-dash-example-com", Owner: "leanhanc",
		PrivateKey: testAppKeyPEM(), WebhookSecret: testWebhookSecret,
	}
}

func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// github-connection: Connect GitHub; security-log: Connection recorded
func TestConnectGitHub(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	env.connectGitHub(t)

	app, err := env.dashboard.GitHubStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !app.IsConnected || app.AppID != 4242 || app.Slug != "tero-dash-example-com" || app.Owner != "leanhanc" {
		t.Errorf("status = %+v", app)
	}
	if !slices.Contains(env.eventTypes(t), EventGitHubConnected) {
		t.Error("github_connected wasn't recorded")
	}
}

// key-service: Secrets never leave
func TestGitHubSecretsNeverLeave(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	env.connectGitHub(t)
	ctx := context.Background()

	var answers []string
	for _, caller := range []Caller{CallerDashboard, CallerAgent, CallerRoot} {
		for verb := range verbs[caller] {
			answer := env.service.handle(ctx, caller, request{Verb: verb, Args: json.RawMessage(`{}`)})
			answers = append(answers, string(answer.Result))
		}
	}

	privateKeyBody := base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PrivateKey(testAppKey))[:64]
	for _, answer := range answers {
		if strings.Contains(answer, privateKeyBody) || strings.Contains(answer, testWebhookSecret) {
			t.Fatalf("an answer holds a secret: %s", answer)
		}
	}

	var stored []byte
	env.service.db.QueryRow("SELECT private_key FROM github_app").Scan(&stored)
	if strings.Contains(string(stored), "PRIVATE KEY") {
		t.Error("the private key is stored in clear")
	}
}

// key-service: Short-lived JWT, Agent asks for a JWT
func TestAppTokenIsSignedAndShortLived(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	env.connectGitHub(t)

	token, err := env.agent().SignAppToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	claims := verifyTestJWT(t, token.Token)
	if claims.Issuer != "4242" {
		t.Errorf("iss = %q, want the App id", claims.Issuer)
	}
	lifetime := time.Unix(claims.ExpiresAt, 0).Sub(env.clock.Now())
	if lifetime <= 0 || lifetime > 5*time.Minute {
		t.Errorf("the token lives %s, want at most 5 minutes", lifetime)
	}
	if claims.IssuedAt >= env.clock.Now().Unix() {
		t.Error("the token isn't backdated for clock skew")
	}
}

type testClaims struct {
	Issuer    string `json:"iss"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

func verifyTestJWT(t *testing.T, token string) testClaims {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts", len(parts))
	}

	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	signature, _ := base64.RawURLEncoding.DecodeString(parts[2])
	if err := rsa.VerifyPKCS1v15(&testAppKey.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		t.Fatalf("signature doesn't verify: %v", err)
	}

	var claims testClaims
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	json.Unmarshal(payload, &claims)
	return claims
}

// key-service: Dashboard asks for a JWT, Agent asks for account verbs
func TestOnlyTheAgentGetsAppTokens(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	env.connectGitHub(t)
	ctx := context.Background()

	for _, client := range []*Client{env.dashboard, env.root} {
		if _, err := client.SignAppToken(ctx); !errors.Is(err, ErrRefused) {
			t.Errorf("a caller other than the agent got an App token: %v", err)
		}
	}
	if _, err := env.agent().ListSessions(ctx, *env.session); !errors.Is(err, ErrRefused) {
		t.Errorf("the agent listed sessions: %v", err)
	}
	if _, err := env.agent().RecentEvents(ctx, "", 10); !errors.Is(err, ErrRefused) {
		t.Errorf("the agent listed events: %v", err)
	}
}

// github-connection: Unconfirmed session; key-service: Dashboard connects
// without a session
func TestConnectingNeedsAConfirmedSession(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()

	if _, err := env.dashboard.BeginGitHubConnection(ctx, ""); !errors.Is(err, ErrSessionEnded) {
		t.Errorf("begin without a session = %v", err)
	}

	env.clock.Advance(6 * time.Minute)
	if _, err := env.dashboard.BeginGitHubConnection(ctx, *env.session); !errors.Is(err, ErrNotConfirmed) {
		t.Errorf("begin on an unconfirmed session = %v", err)
	}
}

// github-connection: Callback with a foreign state; key-service: State from
// another session, Agent connects without approval
func TestConnectionStateIsTheAdminsOwnAndWorksOnce(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	ctx := context.Background()
	state, err := env.dashboard.BeginGitHubConnection(ctx, *env.session)
	if err != nil {
		t.Fatal(err)
	}

	// A code works once, so the second login waits for the next one.
	env.clock.Advance(30 * time.Second)
	other, err := env.loginFrom(Browser{IP: adminIP}, secret, adminPassword)
	if err != nil {
		t.Fatal(err)
	}
	if err := env.dashboard.ApproveGitHubConnection(ctx, other.SessionToken, state); !errors.Is(err, ErrInvalidConnection) {
		t.Errorf("another session approved the state: %v", err)
	}
	if err := env.agent().ConnectGitHub(ctx, testAppSecrets(state)); !errors.Is(err, ErrInvalidConnection) {
		t.Errorf("the agent connected with an unapproved state: %v", err)
	}

	if err := env.dashboard.ApproveGitHubConnection(ctx, *env.session, state); err != nil {
		t.Fatal(err)
	}
	if err := env.agent().ConnectGitHub(ctx, testAppSecrets(state)); err != nil {
		t.Fatal(err)
	}
	env.service.ForgetGitHub(ctx)
	if err := env.agent().ConnectGitHub(ctx, testAppSecrets(state)); !errors.Is(err, ErrInvalidConnection) {
		t.Errorf("the state worked twice: %v", err)
	}
}

func TestConnectionStateExpires(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()
	state, _ := env.dashboard.BeginGitHubConnection(ctx, *env.session)
	env.dashboard.ApproveGitHubConnection(ctx, *env.session, state)

	env.clock.Advance(11 * time.Minute)
	if err := env.agent().ConnectGitHub(ctx, testAppSecrets(state)); !errors.Is(err, ErrInvalidConnection) {
		t.Errorf("an expired state worked: %v", err)
	}
}

// github-connection: One account per server
func TestOneAppPerServer(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	env.connectGitHub(t)

	if _, err := env.dashboard.BeginGitHubConnection(context.Background(), *env.session); !errors.Is(err, ErrGitHubConnected) {
		t.Errorf("a second connection began: %v", err)
	}
}

func TestConnectRefusesAKeyThatIsntRSA(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()
	state, _ := env.dashboard.BeginGitHubConnection(ctx, *env.session)
	env.dashboard.ApproveGitHubConnection(ctx, *env.session, state)

	secrets := testAppSecrets(state)
	secrets.PrivateKey = "-----BEGIN RSA PRIVATE KEY-----\nbm90IGEga2V5\n-----END RSA PRIVATE KEY-----\n"
	if err := env.agent().ConnectGitHub(ctx, secrets); !errors.Is(err, ErrInvalidAppKey) {
		t.Errorf("connect with a bad key = %v", err)
	}
}

// github-connection: Valid push webhook, Unsigned or forged webhook;
// security-log: Forged webhook recorded
func TestVerifyWebhook(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()
	body := []byte(`{"ref":"refs/heads/main"}`)

	if isValid, _ := env.dashboard.VerifyWebhook(ctx, body, signBody(testWebhookSecret, body), adminIP); isValid {
		t.Error("a webhook was valid with no App connected")
	}

	env.connectGitHub(t)
	if isValid, err := env.dashboard.VerifyWebhook(ctx, body, signBody(testWebhookSecret, body), adminIP); err != nil || !isValid {
		t.Errorf("a correctly signed webhook = %v, %v", isValid, err)
	}
	for _, signature := range []string{"", "sha256=00", signBody("another secret", body), "sha1=" + signBody(testWebhookSecret, body)[7:]} {
		if isValid, _ := env.dashboard.VerifyWebhook(ctx, body, signature, adminIP); isValid {
			t.Errorf("signature %q was valid", signature)
		}
	}
	if !slices.Contains(env.eventTypes(t), EventWebhookSignatureFailed) {
		t.Error("webhook_signature_failed wasn't recorded")
	}
}

// key-service: Rotation re-seals the App's secrets
func TestRotationResealsTheAppsSecrets(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	env.connectGitHub(t)
	ctx := context.Background()

	env.service.keys[2] = testKey(2)
	inUse, err := env.root.RotateKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(inUse, []int{2}) {
		t.Errorf("versions in use = %v, want [2]", inUse)
	}

	delete(env.service.keys, 1)
	if _, err := env.agent().SignAppToken(ctx); err != nil {
		t.Errorf("App token after rotation: %v", err)
	}
	body := []byte(`{}`)
	if isValid, _ := env.dashboard.VerifyWebhook(ctx, body, signBody(testWebhookSecret, body), adminIP); !isValid {
		t.Error("webhooks stopped verifying after rotation")
	}
}

// github-connection: Disconnect; breach-response: GitHub connection deleted
func TestDisconnectAndForget(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	env.connectGitHub(t)
	ctx := context.Background()

	env.clock.Advance(6 * time.Minute)
	if err := env.dashboard.DisconnectGitHub(ctx, *env.session); !errors.Is(err, ErrNotConfirmed) {
		t.Errorf("disconnect on an unconfirmed session = %v", err)
	}
	if err := env.root.ForgetGitHub(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := env.agent().SignAppToken(ctx); !errors.Is(err, ErrGitHubNotConnected) {
		t.Errorf("App token after forgetting = %v", err)
	}
	if !slices.Contains(env.eventTypes(t), EventGitHubDisconnected) {
		t.Error("github_disconnected wasn't recorded")
	}
	if err := env.dashboard.AppendEvent(ctx, EventGitHubConnected, ""); !errors.Is(err, ErrRefused) {
		t.Errorf("the dashboard appended github_connected: %v", err)
	}
}
