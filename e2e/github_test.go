//go:build e2e

package e2e

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The fake GitHub (e2e/fakegithub) runs in the VM; e2e builds of Tero read
// its address from fakeGitHubFile.
const (
	fakeGitHubAddress = "http://127.0.0.1:9900"
	fakeGitHubFile    = "/opt/tero-e2e/github-api"
	privateApp        = "gh-npm"
)

// currentAdmin is the admin testDashboardAccess set up, for tests later in
// the run that need to confirm it's them.
var currentAdmin *dashboard

type fakeGitHubState struct {
	WebhookSecret string `json:"webhookSecret"`
	RepositoryID  int64  `json:"repositoryId"`
	Minted        []struct {
		Token         string            `json:"token"`
		RepositoryIDs []int64           `json:"repositoryIds"`
		Permissions   map[string]string `json:"permissions"`
	} `json:"minted"`
	Revoked []string `json:"revoked"`
}

func testGitHub(t *testing.T) {
	startFakeGitHub(t)

	t.Run("github-connection: Connect GitHub / Dashboard never sees the private key", testConnectGitHub)
	if t.Failed() {
		t.FailNow()
	}
	t.Run("apps: Create from a private repository / github-connection: Deploy a private repository / Token kept out of the build", testPrivateDeploy)
	t.Run("github-connection: Valid push webhook / Replayed delivery / Unsigned or forged webhook / deployments: Push deploys", testPushWebhook)
	t.Run("github-connection: No long-lived repository credentials", testEveryTokenRevoked)
	t.Run("key-service: Secrets not in tero's reach", testGitHubSecretsOutOfReach)
}

// startFakeGitHub runs the fake GitHub in the VM, points Tero at it and
// restarts the agent, which reads the address when it starts.
func startFakeGitHub(t *testing.T) {
	must(t, "sudo install -m 0755 /tmp/fake-github /opt/tero-e2e/fake-github")
	must(t, "sudo systemd-run --quiet --unit=tero-e2e-github --property=DynamicUser=yes /opt/tero-e2e/fake-github -listen 127.0.0.1:9900")
	waitUntil(t, 30*time.Second, func() bool { return run(t, "curl -sf "+fakeGitHubAddress+"/e2e/state").exitCode == 0 })

	must(t, "echo "+fakeGitHubAddress+" | sudo tee "+fakeGitHubFile+" >/dev/null")
	must(t, agentSystemctl+" restart tero-agent")
	waitUntil(t, time.Minute, func() bool { return run(t, agentSystemctl+" is-active tero-agent").exitCode == 0 })
}

func fakeGitHub(t *testing.T) fakeGitHubState {
	t.Helper()
	var state fakeGitHubState
	if err := json.Unmarshal([]byte(must(t, "curl -sf "+fakeGitHubAddress+"/e2e/state")), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

// waitForGitHubRequest waits until the agent answers a request and returns
// its status.
func waitForGitHubRequest(t *testing.T, response dashboardResponse) string {
	t.Helper()
	var queued struct {
		RequestID int64 `json:"requestId"`
	}
	if err := json.Unmarshal([]byte(response.body), &queued); err != nil || queued.RequestID == 0 {
		t.Fatalf("no request in %d %s", response.status, response.body)
	}

	var answer struct{ Status, Result string }
	waitUntil(t, time.Minute, func() bool {
		got := getDashboard(t, "admin", "/api/github/requests/"+strconv.FormatInt(queued.RequestID, 10))
		json.Unmarshal([]byte(got.body), &answer)
		return answer.Status != "pending"
	})
	if answer.Status != "done" {
		t.Errorf("the agent answered %s: %s", answer.Status, answer.Result)
	}
	return answer.Status
}

func testConnectGitHub(t *testing.T) {
	currentAdmin.confirm(t, "admin")

	begin := postDashboard(t, "admin", "/api/github/connect/begin", map[string]string{"organization": ""})
	if begin.status != 200 {
		t.Fatalf("connect/begin = %d %s", begin.status, begin.body)
	}
	var start struct{ Action, Manifest, State string }
	json.Unmarshal([]byte(begin.body), &start)
	assertContains(t, start.Manifest, `"url":"https://`+skippedDomain+`/api/github/webhook"`, `"public":false`)

	// GitHub would now show the manifest to the admin and send them back
	// with a code; the fake accepts any.
	finish := postDashboard(t, "admin", "/api/github/connect", map[string]string{"code": "e2e-code", "state": start.State})
	if finish.status != 202 {
		t.Fatalf("connect = %d %s", finish.status, finish.body)
	}
	waitForGitHubRequest(t, finish)

	refresh := postDashboard(t, "admin", "/api/github/refresh", map[string]string{})
	waitForGitHubRequest(t, refresh)

	status := getDashboard(t, "admin", "/api/github")
	assertContains(t, status.body, `"isConnected":true`, `"slug":"tero-e2e"`, `"name":"tero"`, `"isPrivate":true`)
}

func testPrivateDeploy(t *testing.T) {
	repositoryID := fakeGitHub(t).RepositoryID
	response := postDashboard(t, "admin", "/api/apps", map[string]any{
		"githubRepositoryId": repositoryID, "branch": "main", "displayName": "Private npm", "name": privateApp,
		"rootDirectory": "e2e/fixtures/npm",
	})
	if response.status != 201 {
		t.Fatalf("create from the private repository = %d %s", response.status, response.body)
	}

	deploy := waitForDeploy(t, privateApp).Deploys[0]
	if deploy.State != "live" {
		t.Fatalf("private deploy ended %s at %q: %s", deploy.State, deploy.FailedStep, deploy.Reason)
	}
	waitUntil(t, 30*time.Second, func() bool {
		return strings.Contains(fetchApp(t, privateApp, "/").output, "tero fixture npm on node")
	})

	if found := run(t, "sudo grep -rl 'ghs_' /var/lib/tero-shared/logs").output; found != "" {
		t.Errorf("an installation token is in a build log: %s", found)
	}
}

func testPushWebhook(t *testing.T) {
	state := fakeGitHub(t)
	body := []byte(fmt.Sprintf(`{"ref":"refs/heads/main","after":"%s","deleted":false,`+
		`"repository":{"id":%d,"name":"tero","owner":{"login":"leanhanc"}},"sender":{"login":"e2e-pusher"}}`,
		strings.Repeat("a", 40), state.RepositoryID))
	signature := signWebhook(state.WebhookSecret, body)
	deploysBefore := len(getApp(t, privateApp).Deploys)

	if got := sendWebhook(t, "push", "e2e00000-0000-0000-0000-000000000001", signature, body); got != 202 {
		t.Fatalf("signed push = %d", got)
	}
	if got := sendWebhook(t, "push", "e2e00000-0000-0000-0000-000000000001", signature, body); got != 200 {
		t.Errorf("replayed push = %d, want 200", got)
	}
	if got := sendWebhook(t, "push", "e2e00000-0000-0000-0000-000000000002", signWebhook("not the secret", body), body); got != 401 {
		t.Errorf("forged push = %d, want 401", got)
	}

	detail := waitForDeploy(t, privateApp)
	if len(detail.Deploys) != deploysBefore+1 {
		t.Errorf("deploys after one push and a replay: %d, want %d", len(detail.Deploys), deploysBefore+1)
	}
	page := getDashboard(t, "admin", "/api/apps/"+privateApp)
	assertContains(t, page.body, `"trigger":"push"`, `"pushedBy":"e2e-pusher"`)
}

func testEveryTokenRevoked(t *testing.T) {
	state := fakeGitHub(t)
	if len(state.Minted) == 0 {
		t.Fatal("the agent minted no installation token")
	}

	for _, minted := range state.Minted {
		if !slices.Contains(state.Revoked, minted.Token) {
			t.Errorf("a token for %v wasn't revoked", minted.RepositoryIDs)
		}
		isRepositoryToken := len(minted.RepositoryIDs) > 0
		if isRepositoryToken && (!slices.Equal(minted.RepositoryIDs, []int64{state.RepositoryID}) || minted.Permissions["contents"] != "read") {
			t.Errorf("a repository token asked for %v on %v", minted.Permissions, minted.RepositoryIDs)
		}
	}
}

func testGitHubSecretsOutOfReach(t *testing.T) {
	state := fakeGitHub(t)
	files := "/var/lib/tero-shared/tero.db* /var/lib/tero-keys/keys.db*"
	for _, secret := range []string{"PRIVATE KEY", state.WebhookSecret} {
		if found := run(t, "sudo grep -l -a '"+secret+"' "+files+" 2>/dev/null").output; found != "" {
			t.Errorf("%q is stored in clear in %s", secret, found)
		}
	}
	if found := run(t, "sudo journalctl -u tero -u tero-keys --no-pager | grep -c 'PRIVATE KEY'").output; strings.TrimSpace(found) != "0" {
		t.Errorf("the private key appears in the journal (%s lines)", found)
	}
}

func signWebhook(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// sendWebhook posts a webhook to the dashboard as GitHub would, and returns
// the status code.
func sendWebhook(t *testing.T, event, guid, signature string, body []byte) int {
	t.Helper()
	script := fmt.Sprintf(`echo %s | base64 -d | curl -sS -k -o /dev/null -w '%%{http_code}' --resolve %s:443:127.0.0.1 -X POST `+
		`-H 'Content-Type: application/json' -H 'X-GitHub-Event: %s' -H 'X-GitHub-Delivery: %s' -H 'X-Hub-Signature-256: %s' `+
		`--data-binary @- https://%s/api/github/webhook`,
		base64.StdEncoding.EncodeToString(body), skippedDomain, event, guid, signature, skippedDomain)
	status, _ := strconv.Atoi(must(t, script))
	return status
}
