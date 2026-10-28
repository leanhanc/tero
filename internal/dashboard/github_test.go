package dashboard

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leanhanc/tero/internal/githubapp"
	"github.com/leanhanc/tero/internal/keys"
)

const testWebhookSecret = "webhook secret"

var testAppKey = func() string {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}()

// connectGitHub goes through the dashboard's connect routes, standing in for
// the agent's part with its own key service client.
func (d testDashboard) connectGitHub(t *testing.T, session *http.Cookie) {
	t.Helper()

	begin := d.post("/api/github/connect/begin", map[string]string{"organization": ""}, session)
	if begin.Code != http.StatusOK {
		t.Fatalf("connect/begin = %d %s", begin.Code, begin.Body)
	}
	var start githubapp.Start
	json.Unmarshal(begin.Body.Bytes(), &start)

	finish := d.post("/api/github/connect", map[string]string{"code": "abc123", "state": start.State}, session)
	if finish.Code != http.StatusAccepted {
		t.Fatalf("connect = %d %s", finish.Code, finish.Body)
	}

	err := d.keysAgent.ConnectGitHub(context.Background(), keys.GitHubAppSecrets{
		State: start.State, AppID: 7, Slug: "tero-dash", Owner: "leanhanc", PrivateKey: testAppKey, WebhookSecret: testWebhookSecret,
	})
	if err != nil {
		t.Fatal(err)
	}
}

// github-connection: Connect GitHub
func TestConnectGitHubFromTheDashboard(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)

	begin := d.post("/api/github/connect/begin", map[string]string{"organization": "acme"}, session)
	var start githubapp.Start
	json.Unmarshal(begin.Body.Bytes(), &start)
	if !strings.HasPrefix(start.Action, "https://github.com/organizations/acme/settings/apps/new?state=") {
		t.Errorf("action = %q", start.Action)
	}
	for _, want := range []string{`"url":"https://dash.example.com/api/github/webhook"`, `"public":false`, `"contents":"read"`, `"default_events":["push"]`} {
		if !strings.Contains(start.Manifest, want) {
			t.Errorf("manifest is missing %s: %s", want, start.Manifest)
		}
	}

	finish := d.post("/api/github/connect", map[string]string{"code": "abc123", "state": start.State}, session)
	var queued gitHubRequestResponse
	json.Unmarshal(finish.Body.Bytes(), &queued)
	answer := d.get(fmt.Sprintf("/api/github/requests/%d", queued.RequestID), session)
	if answer.Code != http.StatusOK || !strings.Contains(answer.Body.String(), `"status":"pending"`) {
		t.Errorf("request = %d %s", answer.Code, answer.Body)
	}
}

// github-connection: Unconfirmed session
func TestConnectGitHubNeedsAConfirmedSession(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)
	*d.now = d.now.Add(6 * time.Minute)

	begin := d.post("/api/github/connect/begin", map[string]string{"organization": ""}, session)
	if begin.Code != http.StatusForbidden || !strings.Contains(begin.Body.String(), notConfirmedCode) {
		t.Errorf("begin on an unconfirmed session = %d %s", begin.Code, begin.Body)
	}
}

// github-connection: Callback with a foreign state
func TestConnectGitHubRefusesAForeignState(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)

	finish := d.post("/api/github/connect", map[string]string{"code": "abc123", "state": "made-up"}, session)
	if finish.Code != http.StatusForbidden {
		t.Errorf("connect with a made-up state = %d %s", finish.Code, finish.Body)
	}
}

func TestConnectGitHubRefusesABadOrganizationName(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)

	begin := d.post("/api/github/connect/begin", map[string]string{"organization": "../evil"}, session)
	if begin.Code != http.StatusUnprocessableEntity {
		t.Errorf("begin with a bad organization = %d %s", begin.Code, begin.Body)
	}
}

// github-connection: One account per server, Disconnect
func TestGitHubStatusAndDisconnect(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)
	d.connectGitHub(t, session)

	status := d.get("/api/github", session)
	for _, want := range []string{`"isConnected":true`, `"settingsUrl":"https://github.com/settings/apps/tero-dash"`, `"installUrl":"https://github.com/apps/tero-dash/installations/new"`} {
		if !strings.Contains(status.Body.String(), want) {
			t.Errorf("status is missing %s: %s", want, status.Body)
		}
	}
	if again := d.post("/api/github/connect/begin", map[string]string{"organization": ""}, session); again.Code != http.StatusConflict {
		t.Errorf("a second connection = %d", again.Code)
	}

	if disconnect := d.post("/api/github/disconnect", map[string]string{}, session); disconnect.Code != http.StatusNoContent {
		t.Fatalf("disconnect = %d %s", disconnect.Code, disconnect.Body)
	}
	if status := d.get("/api/github", session); strings.Contains(status.Body.String(), `"isConnected":true`) {
		t.Errorf("still connected after disconnecting: %s", status.Body)
	}
}

func signWebhook(body []byte) string {
	mac := hmac.New(sha256.New, []byte(testWebhookSecret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (d testDashboard) sendWebhook(event, guid, signature string, body []byte, ip string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, "https://dash.example.com/api/github/webhook", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Event", event)
	request.Header.Set("X-GitHub-Delivery", guid)
	request.Header.Set("X-Hub-Signature-256", signature)
	request.RemoteAddr = ip + ":443"

	recorder := httptest.NewRecorder()
	d.handler.ServeHTTP(recorder, request)
	return recorder
}

func pushBody(branch string) []byte {
	return []byte(`{"ref":"refs/heads/` + branch + `","after":"0123456789abcdef0123456789abcdef01234567","deleted":false,` +
		`"repository":{"id":555,"name":"api","owner":{"login":"leanhanc"}},"sender":{"login":"ana"}}`)
}

func (d testDashboard) insertCoveredApp(t *testing.T, name, branch string) {
	t.Helper()
	_, err := d.db.Exec("INSERT INTO apps (name, repository, branch, github_repository_id, created_at) VALUES (?, 'https://github.com/leanhanc/old-name', ?, 555, 1)", name, branch)
	if err != nil {
		t.Fatal(err)
	}
}

func (d testDashboard) queuedPushes(t *testing.T) []string {
	t.Helper()
	rows, err := d.db.Query("SELECT apps.name || ':' || pushed_by FROM deploys JOIN apps ON apps.id = deploys.app_id WHERE state = 'queued' AND trigger_type = 'push' ORDER BY apps.name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	var queued []string
	for rows.Next() {
		var entry string
		rows.Scan(&entry)
		queued = append(queued, entry)
	}
	return queued
}

// github-connection: Valid push webhook, Replayed delivery, Push to another
// branch; deployments: Push deploys, Deploy on push turned off; apps:
// Repository renamed
func TestPushWebhookQueuesDeploysOnce(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)
	d.connectGitHub(t, session)
	d.insertCoveredApp(t, "api", "main")
	d.insertCoveredApp(t, "staging", "staging")
	d.insertCoveredApp(t, "paused", "main")
	d.db.Exec("UPDATE apps SET deploy_on_push = 0 WHERE name = 'paused'")

	body := pushBody("main")
	first := d.sendWebhook("push", "11111111-2222-3333-4444-555555555555", signWebhook(body), body, "140.82.112.1")
	if first.Code != http.StatusAccepted {
		t.Fatalf("push = %d %s", first.Code, first.Body)
	}
	replay := d.sendWebhook("push", "11111111-2222-3333-4444-555555555555", signWebhook(body), body, "140.82.112.1")
	if replay.Code != http.StatusOK {
		t.Errorf("replayed push = %d", replay.Code)
	}

	if queued := d.queuedPushes(t); fmt.Sprint(queued) != "[api:ana]" {
		t.Errorf("queued pushes = %v, want only api, pushed by ana", queued)
	}
	var repository string
	d.db.QueryRow("SELECT repository FROM apps WHERE name = 'api'").Scan(&repository)
	if repository != "https://github.com/leanhanc/api" {
		t.Errorf("repository after the push = %q, want the current name", repository)
	}
}

// github-connection: Push to another branch, a tag or a deleted branch
func TestPushesThatQueueNothing(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)
	d.connectGitHub(t, session)
	d.insertCoveredApp(t, "api", "main")

	bodies := map[string][]byte{
		"tag":     []byte(`{"ref":"refs/tags/main","repository":{"id":555,"name":"api","owner":{"login":"leanhanc"}}}`),
		"deleted": []byte(`{"ref":"refs/heads/main","deleted":true,"repository":{"id":555,"name":"api","owner":{"login":"leanhanc"}}}`),
		"other":   []byte(`{"ref":"refs/heads/main","repository":{"id":999,"name":"other","owner":{"login":"leanhanc"}}}`),
	}
	index := 0
	for name, body := range bodies {
		index++
		response := d.sendWebhook("push", fmt.Sprintf("0000000%d-0000-0000-0000-000000000000", index), signWebhook(body), body, "140.82.112.1")
		if response.Code != http.StatusAccepted {
			t.Errorf("%s push = %d %s", name, response.Code, response.Body)
		}
	}

	if queued := d.queuedPushes(t); len(queued) != 0 {
		t.Errorf("queued pushes = %v, want none", queued)
	}
}

// github-connection: Unsigned or forged webhook, Flood of bad signatures
func TestForgedWebhooksAreRefusedAndThrottled(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)
	d.connectGitHub(t, session)
	d.insertCoveredApp(t, "api", "main")
	body := pushBody("main")

	if missing := d.sendWebhook("push", "aaaa", "", body, "203.0.113.9"); missing.Code != http.StatusBadRequest {
		t.Errorf("unsigned webhook = %d", missing.Code)
	}
	for attempt := range badSignatureLimit {
		forged := d.sendWebhook("push", fmt.Sprintf("bbbb-%d", attempt), "sha256=00", body, "203.0.113.9")
		if forged.Code != http.StatusUnauthorized {
			t.Fatalf("forged webhook %d = %d", attempt, forged.Code)
		}
	}
	if limited := d.sendWebhook("push", "cccc", signWebhook(body), body, "203.0.113.9"); limited.Code != http.StatusTooManyRequests {
		t.Errorf("after the limit = %d, want 429", limited.Code)
	}

	if queued := d.queuedPushes(t); len(queued) != 0 {
		t.Errorf("forged webhooks queued %v", queued)
	}
	if fromGitHub := d.sendWebhook("push", "dddd", signWebhook(body), body, "140.82.112.1"); fromGitHub.Code != http.StatusAccepted {
		t.Errorf("another address was throttled too: %d", fromGitHub.Code)
	}
}

func TestUnhandledEventsAreAcknowledgedUnchecked(t *testing.T) {
	d := newTestDashboard(t)

	response := d.sendWebhook("issues", "eeee", "sha256=00", []byte(`{}`), "140.82.112.1")
	if response.Code != http.StatusNoContent {
		t.Errorf("issues webhook = %d", response.Code)
	}
}

// github-connection: Installation removed on GitHub
func TestInstallationWebhookAsksTheAgentToRefresh(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)
	d.connectGitHub(t, session)
	body := []byte(`{"action":"deleted"}`)

	response := d.sendWebhook("installation", "ffff", signWebhook(body), body, "140.82.112.1")
	if response.Code != http.StatusAccepted {
		t.Fatalf("installation webhook = %d %s", response.Code, response.Body)
	}

	var pending int
	d.db.QueryRow("SELECT count(*) FROM github_requests WHERE kind = 'refresh_installation' AND status = 'pending'").Scan(&pending)
	if pending != 1 {
		t.Errorf("pending refreshes = %d, want 1", pending)
	}
}

func TestOversizedWebhookIsRefused(t *testing.T) {
	d := newTestDashboard(t)
	body := bytes.Repeat([]byte("x"), keys.MaxWebhookBody+1)

	response := d.sendWebhook("push", "abcd", signWebhook(body), body, "140.82.112.1")
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized webhook = %d", response.Code)
	}
}

// answerSourceChecks stands in for the agent: it answers every pending
// source check with status and result.
func (d testDashboard) answerSourceChecks(t *testing.T, status, result string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	go func() {
		for ctx.Err() == nil {
			d.db.Exec("UPDATE github_requests SET status = ?, result = ? WHERE kind = 'check_source' AND status = 'pending'", status, result)
			time.Sleep(20 * time.Millisecond)
		}
	}()
}

// apps: Create from a private repository
func TestCreateAppFromACoveredPrivateRepository(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)
	d.db.Exec("INSERT INTO github_repositories (id, owner, name, is_private, default_branch) VALUES (555, 'leanhanc', 'secret-api', 1, 'main')")
	d.answerSourceChecks(t, "done", `{"rootDirectory":"apps/api"}`)

	response := d.post("/api/apps", map[string]any{
		"githubRepositoryId": 555, "branch": "main", "displayName": "Secret API", "name": "secret-api", "rootDirectory": "apps/api/",
	}, session)
	if response.Code != http.StatusCreated && response.Code != http.StatusOK {
		t.Fatalf("create = %d %s", response.Code, response.Body)
	}

	var repository, rootDirectory string
	var repositoryID int64
	d.db.QueryRow("SELECT repository, root_directory, github_repository_id FROM apps WHERE name = 'secret-api'").
		Scan(&repository, &rootDirectory, &repositoryID)
	if repository != "https://github.com/leanhanc/secret-api" || rootDirectory != "apps/api" || repositoryID != 555 {
		t.Errorf("stored %q %q %d", repository, rootDirectory, repositoryID)
	}
	var trigger string
	d.db.QueryRow("SELECT trigger_type FROM deploys").Scan(&trigger)
	if trigger != "create" {
		t.Errorf("first deploy trigger = %q", trigger)
	}
}

// apps: Repository or branch not found (private)
func TestCreateAppFromACoveredRepositoryWithAMissingBranch(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)
	d.db.Exec("INSERT INTO github_repositories (id, owner, name, is_private, default_branch) VALUES (555, 'leanhanc', 'secret-api', 1, 'main')")
	d.answerSourceChecks(t, "failed", "That branch wasn't found in the repository")

	response := d.post("/api/apps", map[string]any{
		"githubRepositoryId": 555, "branch": "nope", "displayName": "Secret API", "name": "secret-api", "rootDirectory": "",
	}, session)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), `"field":"branch"`) {
		t.Errorf("create with a missing branch = %d %s", response.Code, response.Body)
	}
}

func TestCreateAppFromARepositoryNoLongerCovered(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)

	response := d.post("/api/apps", map[string]any{
		"githubRepositoryId": 999, "branch": "main", "displayName": "Gone", "name": "gone", "rootDirectory": "",
	}, session)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), `"field":"repository"`) {
		t.Errorf("create from an uncovered repository = %d %s", response.Code, response.Body)
	}
}

func TestBadSignaturesFromOneIPv6NetworkShareALimit(t *testing.T) {
	var limits badSignatures
	now := time.Now()
	for index := range badSignatureLimit {
		limits.count(fmt.Sprintf("2001:db8:1:2::%x", index+1), now)
	}

	if !limits.isLimited("2001:db8:1:2::ffff", now) {
		t.Error("rotating addresses within one /64 escaped the limit")
	}
	if limits.isLimited("2001:db8:1:3::1", now) {
		t.Error("another /64 was limited")
	}
}
