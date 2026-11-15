//go:build e2e

package e2e

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
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
	// fakeRepositoryID is the fake GitHub's id for the fixtures' repository.
	fakeRepositoryID = 4242001
	// gitHubOrganization owns the App the suite connects last, which
	// assume-breach then deletes.
	gitHubOrganization = "tero-e2e-org"
	// pollPeriod is how often the agent polls public repositories, plus
	// its jitter.
	pollPeriod = 70 * time.Second
	// notInstalled is what a covered repository's deploy says while the
	// App can't be used.
	notInstalled = "Connect GitHub to deploy this app, or add its repository to the GitHub App's installation"
	// connectGitHubInSettings ends what a private repository's deploy says
	// when GitHub isn't connected.
	connectGitHubInSettings = "connect GitHub in Settings"
)

// currentAdmin is the admin testDashboardAccess set up, for tests later in
// the run that need to confirm it's them.
var currentAdmin *dashboard

type fakeGitHubState struct {
	AppID         int64  `json:"appId"`
	Slug          string `json:"slug"`
	WebhookSecret string `json:"webhookSecret"`
	RepositoryID  int64  `json:"repositoryId"`
	Deliveries    []struct {
		ID           int64  `json:"id"`
		GUID         string `json:"guid"`
		StatusCode   int    `json:"status_code"`
		IsRedelivery bool   `json:"redelivery"`
	} `json:"deliveries"`
	DeliveryListings   int     `json:"deliveryListings"`
	RedeliveryRequests []int64 `json:"redeliveryRequests"`
	Minted             []struct {
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
	t.Run("key-service: Agent asks for a JWT / Dashboard asks for a JWT / Agent asks for account verbs / Short-lived JWT", testAgentCallerChecks)

	t.Run("setup: only the private app deploys on push", testOnlyPrivateAppOnPush)
	t.Run("deployments: Deploy on push turned off", testDeployOnPushOff)
	t.Run("github-connection: Push while the server was down / Several missed pushes / Delivery already handled", testMissedPushesRedelivered)
	t.Run("github-connection: New commit on a public repository / No change / Repository gone", testPolling)
	t.Run("github-connection: Disconnect / Reconnect / Installation removed on GitHub", testDisconnectAndReconnect)
	t.Run("github-connection: Connect an organization", testConnectOrganization)
}

// startFakeGitHub runs the fake GitHub in the VM, points Tero at it and
// restarts the agent, which reads the address when it starts.
func startFakeGitHub(t *testing.T) {
	must(t, "sudo install -m 0755 /tmp/fake-github /opt/tero-e2e/fake-github")
	must(t, "sudo systemd-run --quiet --unit=tero-e2e-github --property=DynamicUser=yes /opt/tero-e2e/fake-github -listen 127.0.0.1:9900"+
		" -webhook https://"+skippedDomain+"/api/github/webhook")
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
	start := connectGitHub(t, "")
	assertContains(t, start.Manifest, `"url":"https://`+skippedDomain+`/api/github/webhook"`, `"public":false`)

	status := getDashboard(t, "admin", "/api/github")
	assertContains(t, status.body, `"isConnected":true`, `"slug":"tero-e2e"`, `"name":"tero"`, `"isPrivate":true`)
}

type connectStart struct{ Action, Manifest, State string }

// connectGitHub goes through the manifest flow on a confirmed session, for
// the admin's account or an organization, and has the agent read the
// installation.
func connectGitHub(t *testing.T, organization string) connectStart {
	t.Helper()

	begin := postDashboard(t, "admin", "/api/github/connect/begin", map[string]string{"organization": organization})
	if begin.status != 200 {
		t.Fatalf("connect/begin = %d %s", begin.status, begin.body)
	}
	var start connectStart
	json.Unmarshal([]byte(begin.body), &start)

	// GitHub would now show the manifest to the admin and send them back
	// with a code; the fake accepts any.
	finish := postDashboard(t, "admin", "/api/github/connect", map[string]string{"code": "e2e-code", "state": start.State})
	if finish.status != 202 {
		t.Fatalf("connect = %d %s", finish.status, finish.body)
	}
	waitForGitHubRequest(t, finish)

	refresh := postDashboard(t, "admin", "/api/github/refresh", map[string]string{})
	waitForGitHubRequest(t, refresh)
	return start
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

	if found := run(t, "sudo grep -rl 'ghs_' /var/lib/tero-shared/deploy-logs").output; found != "" {
		t.Errorf("an installation token is in a build log: %s", found)
	}
}

func testPushWebhook(t *testing.T) {
	state := fakeGitHub(t)
	body := pushPayload(state.RepositoryID, strings.Repeat("a", 40), "e2e-pusher")
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

// pushPayload is the part of a push webhook to main that Tero reads.
func pushPayload(repositoryID int64, after, sender string) []byte {
	return []byte(fmt.Sprintf(`{"ref":"refs/heads/main","after":"%s","deleted":false,`+
		`"repository":{"id":%d,"name":"tero","owner":{"login":"leanhanc"}},"sender":{"login":"%s"}}`,
		after, repositoryID, sender))
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
	files := "/var/lib/tero-shared/db/tero.db* /var/lib/tero-keys/keys.db*"
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

// pushAppDetail is an app page with what deploy on push shows.
type pushAppDetail struct {
	DeployOnPush bool
	PollError    string
	Deploys      []struct {
		ID         int64
		State      string
		FailedStep string
		Reason     string
		Trigger    string
		PushedBy   string `json:"pushedBy"`
		CommitSHA  string `json:"commitSha"`
	}
}

func getPushApp(t *testing.T, name string) pushAppDetail {
	t.Helper()

	response := getDashboard(t, "admin", "/api/apps/"+name)
	if response.status != 200 {
		t.Fatalf("get %s = %d %s", name, response.status, response.body)
	}
	var detail pushAppDetail
	if err := json.Unmarshal([]byte(response.body), &detail); err != nil {
		t.Fatal(err)
	}
	return detail
}

// waitForPushApp waits until the app's newest deploy is live or failed, so
// nothing is queued behind it, and returns the app.
func waitForPushApp(t *testing.T, name string) pushAppDetail {
	t.Helper()

	var detail pushAppDetail
	waitUntil(t, deployTimeout, func() bool {
		detail = getPushApp(t, name)
		state := detail.Deploys[0].State
		return state == "live" || state == "failed"
	})
	return detail
}

func redeploy(t *testing.T, name string) pushAppDetail {
	t.Helper()
	if response := postDashboard(t, "admin", "/api/apps/"+name+"/deploys", map[string]string{}); response.status != 202 {
		t.Fatalf("redeploy %s = %d %s", name, response.status, response.body)
	}
	return waitForPushApp(t, name)
}

func setDeployOnPush(t *testing.T, name string, deployOnPush bool) {
	t.Helper()
	response := postDashboard(t, "admin", "/api/apps/"+name+"/settings", map[string]bool{"deployOnPush": deployOnPush})
	if response.status != 204 {
		t.Fatalf("deploy on push %v for %s = %d %s", deployOnPush, name, response.status, response.body)
	}
}

// postFakeGitHub changes what the fake GitHub answers, as someone acting on
// github.com would.
func postFakeGitHub(t *testing.T, path string, body any) {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	must(t, fmt.Sprintf("echo %s | base64 -d | curl -sSf -X POST -H 'Content-Type: application/json' --data-binary @- %s%s",
		base64.StdEncoding.EncodeToString(encoded), fakeGitHubAddress, path))
}

// signedPush sends a push to main signed with secret, as GitHub would.
func signedPush(t *testing.T, secret, guid, after, sender string) int {
	t.Helper()
	body := pushPayload(fakeGitHub(t).RepositoryID, after, sender)
	return sendWebhook(t, "push", guid, signWebhook(secret, body), body)
}

// sendInstallationEvent tells Tero, as GitHub would, that the admin changed
// the App's installation.
func sendInstallationEvent(t *testing.T, guid, action string) {
	t.Helper()
	body := []byte(fmt.Sprintf(`{"action":"%s","installation":{"id":77}}`, action))
	if got := sendWebhook(t, "installation", guid, signWebhook(fakeGitHub(t).WebhookSecret, body), body); got != 202 {
		t.Fatalf("installation %s = %d, want 202", action, got)
	}
}

func deliveryGUID(n int) string {
	return fmt.Sprintf("e2e00000-0000-0000-0000-%012d", n)
}

func mainHead(t *testing.T) string {
	t.Helper()
	return must(t, "git -C "+vmRepository+" rev-parse main")
}

func gitHubStatus(t *testing.T) string {
	t.Helper()
	response := getDashboard(t, "admin", "/api/github")
	if response.status != 200 {
		t.Fatalf("GitHub status = %d %s", response.status, response.body)
	}
	return response.body
}

// keysCall sends one verb to the key service over its socket, the way its
// callers do, and prints the answer. A caller the key service doesn't know
// gets no answer, so the probe exits non-zero.
func keysCall(verb string) string {
	return `python3 -c 'import json, socket, struct
s = socket.socket(socket.AF_UNIX)
s.connect("/run/tero-keys/keys.sock")
body = json.dumps({"verb": "` + verb + `"}).encode()
s.sendall(struct.pack(">I", len(body)) + body)
size = struct.unpack(">I", s.recv(4))[0]
print(s.recv(size).decode())'`
}

var jwtPattern = regexp.MustCompile(`"token":"([^"]+)"`)

func testAgentCallerChecks(t *testing.T) {
	answer := must(t, asAgent(keysCall("github.jwt")))
	found := jwtPattern.FindStringSubmatch(answer)
	if found == nil {
		t.Fatalf("the agent asked for a JWT and got: %s", answer)
	}
	now, _ := strconv.ParseInt(must(t, "date +%s"), 10, 64)
	parts := strings.Split(found[1], ".")
	if len(parts) != 3 {
		t.Fatalf("the JWT isn't three parts: %s", found[1])
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	var claims struct {
		ExpiresAt int64 `json:"exp"`
	}
	if err != nil || json.Unmarshal(payload, &claims) != nil {
		t.Fatalf("the JWT's claims don't decode: %s", found[1])
	}
	if lifetime := claims.ExpiresAt - now; lifetime <= 0 || lifetime > 5*60+5 {
		t.Errorf("the JWT expires %d seconds from now, want at most 5 minutes", lifetime)
	}

	if got := must(t, inDashboardCgroup(keysCall("github.jwt"))); jwtPattern.MatchString(got) || !strings.Contains(got, `"error"`) {
		t.Errorf("the dashboard asked for a JWT and got: %s", got)
	}
	if got := run(t, asTero(keysCall("github.jwt"))); jwtPattern.MatchString(got.output) {
		t.Errorf("a tero process asked for a JWT and got: %s", got.output)
	}
	for _, verb := range []string{"session.list", "events.list"} {
		if got := must(t, asAgent(keysCall(verb))); !strings.Contains(got, `"error"`) || strings.Contains(got, `"result"`) {
			t.Errorf("the agent asked for %s and got: %s", verb, got)
		}
	}
}

// testOnlyPrivateAppOnPush leaves deploy on push on only for the private
// app, so each push below builds one app instead of every fixture on main,
// which the App now covers too.
func testOnlyPrivateAppOnPush(t *testing.T) {
	var listing struct {
		Apps []struct{ Name string }
	}
	if err := json.Unmarshal([]byte(getDashboard(t, "admin", "/api/apps").body), &listing); err != nil {
		t.Fatal(err)
	}

	for _, app := range listing.Apps {
		if app.Name != privateApp {
			setDeployOnPush(t, app.Name, false)
		}
	}
	for _, app := range listing.Apps {
		waitForPushApp(t, app.Name)
	}
}

func testDeployOnPushOff(t *testing.T) {
	setDeployOnPush(t, privateApp, false)
	t.Cleanup(func() { setDeployOnPush(t, privateApp, true) })
	if getPushApp(t, privateApp).DeployOnPush {
		t.Error("the app page still shows deploy on push on")
	}
	deploysBefore := len(getPushApp(t, privateApp).Deploys)

	if got := signedPush(t, fakeGitHub(t).WebhookSecret, deliveryGUID(100), strings.Repeat("c", 40), "e2e-switched-off"); got != 202 {
		t.Fatalf("signed push = %d, want 202", got)
	}
	time.Sleep(5 * time.Second)
	if deploys := len(getPushApp(t, privateApp).Deploys); deploys != deploysBefore {
		t.Fatalf("a push queued %d deploys with deploy on push off", deploys-deploysBefore)
	}

	deploy := redeploy(t, privateApp).Deploys[0]
	if deploy.State != "live" || deploy.Trigger != "manual" || deploy.CommitSHA != mainHead(t) {
		t.Errorf("Redeploy = %s (%s) of %s, want live, manual, of the branch head", deploy.State, deploy.Trigger, deploy.CommitSHA)
	}
}

func testMissedPushesRedelivered(t *testing.T) {
	secret := fakeGitHub(t).WebhookSecret
	// A push Tero handled, which GitHub still lists as failed because it
	// timed out waiting for the answer.
	handled := deliveryGUID(200)
	if got := signedPush(t, secret, handled, strings.Repeat("d", 40), "e2e-handled"); got != 202 {
		t.Fatalf("signed push = %d, want 202", got)
	}
	deploysBefore := len(waitForPushApp(t, privateApp).Deploys)

	must(t, agentSystemctl+" stop tero-agent")
	missed := []string{deliveryGUID(201), deliveryGUID(202), deliveryGUID(203)}
	recordFailedPush(t, handled, strings.Repeat("d", 40), "e2e-handled", 0)
	for i, guid := range missed {
		recordFailedPush(t, guid, strings.Repeat(strconv.Itoa(i+1), 40), "e2e-missed", 502)
	}
	restartAgent(t)

	var state fakeGitHubState
	waitUntil(t, 2*time.Minute, func() bool {
		state = fakeGitHub(t)
		return countRedelivered(state) >= len(missed)
	})
	assertRedeliveredOnce(t, state, missed, handled)

	detail := waitForPushApp(t, privateApp)
	if added := len(detail.Deploys) - deploysBefore; added != 1 {
		t.Errorf("three missed pushes queued %d deploys, want 1", added)
	}
	deploy := detail.Deploys[0]
	if deploy.State != "live" || deploy.Trigger != "push" || deploy.PushedBy != "e2e-missed" || deploy.CommitSHA != mainHead(t) {
		t.Errorf("redelivered push deploy = %s (%s by %s) of %s, want live, push, of the branch head",
			deploy.State, deploy.Trigger, deploy.PushedBy, deploy.CommitSHA)
	}

	// Each delivery is asked for at most once, however often the agent
	// starts.
	listings := state.DeliveryListings
	restartAgent(t)
	waitUntil(t, time.Minute, func() bool { return fakeGitHub(t).DeliveryListings > listings })
	time.Sleep(5 * time.Second)
	if requests := len(fakeGitHub(t).RedeliveryRequests); requests != len(missed) {
		t.Errorf("redeliveries asked for after a second start = %d, want %d", requests, len(missed))
	}
}

func recordFailedPush(t *testing.T, guid, after, sender string, statusCode int) {
	t.Helper()
	body := pushPayload(fakeGitHub(t).RepositoryID, after, sender)
	postFakeGitHub(t, "/e2e/deliveries", map[string]any{"guid": guid, "event": "push", "body": string(body), "statusCode": statusCode})
}

func restartAgent(t *testing.T) {
	t.Helper()
	must(t, agentSystemctl+" restart tero-agent")
	waitUntil(t, time.Minute, func() bool { return run(t, agentSystemctl+" is-active tero-agent").exitCode == 0 })
}

func countRedelivered(state fakeGitHubState) int {
	count := 0
	for _, delivery := range state.Deliveries {
		if delivery.IsRedelivery {
			count++
		}
	}
	return count
}

// assertRedeliveredOnce checks the agent asked for each missed delivery
// once, never for the handled one, and Tero accepted each redelivery.
func assertRedeliveredOnce(t *testing.T, state fakeGitHubState, missed []string, handled string) {
	t.Helper()

	guidOf := map[int64]string{}
	for _, delivery := range state.Deliveries {
		guidOf[delivery.ID] = delivery.GUID
		if delivery.IsRedelivery && delivery.StatusCode != 202 {
			t.Errorf("Tero answered the redelivery of %s with %d, want 202", delivery.GUID, delivery.StatusCode)
		}
	}
	asked := map[string]int{}
	for _, deliveryID := range state.RedeliveryRequests {
		asked[guidOf[deliveryID]]++
	}

	for _, guid := range missed {
		if asked[guid] != 1 {
			t.Errorf("redelivery of %s asked for %d times, want once", guid, asked[guid])
		}
	}
	if asked[handled] != 0 {
		t.Errorf("the handled delivery %s was asked for again", handled)
	}
}

const (
	pollApp        = "gh-poll"
	pollOwner      = "e2e-public"
	pollRepository = "/opt/tero-e2e/repos/" + pollOwner + "/poll.git"
)

func testPolling(t *testing.T) {
	must(t, `set -e
		sudo install -d -o "$(id -un)" -m 0755 /opt/tero-e2e/repos/`+pollOwner+`
		rm -rf `+pollRepository+` && git clone -q --bare `+vmRepository+` `+pollRepository+`
		chmod -R a+rX `+pollRepository)
	postFakeGitHub(t, "/e2e/public", map[string]any{"owner": pollOwner, "name": "poll", "id": 5150001})
	response := postDashboard(t, "admin", "/api/apps", map[string]string{
		"repository": "https://github.com/" + pollOwner + "/poll", "branch": "main", "displayName": "Polled npm",
		"name": pollApp, "rootDirectory": "e2e/fixtures/npm",
	})
	if response.status != 201 {
		t.Fatalf("create from a public repository = %d %s", response.status, response.body)
	}
	if first := waitForPushApp(t, pollApp).Deploys[0]; first.State != "live" {
		t.Fatalf("first deploy ended %s: %s", first.State, first.Reason)
	}

	// No change
	time.Sleep(pollPeriod)
	if deploys := len(getPushApp(t, pollApp).Deploys); deploys != 1 {
		t.Errorf("polling an unchanged branch queued %d deploys", deploys-1)
	}

	// New commit on a public repository
	head := pushToPolledRepository(t)
	waitUntil(t, 2*pollPeriod, func() bool { return getPushApp(t, pollApp).Deploys[0].Trigger == "poll" })
	deploy := waitForPushApp(t, pollApp).Deploys[0]
	if deploy.State != "live" || deploy.CommitSHA != head {
		t.Errorf("polled deploy = %s of %s, want live of %s", deploy.State, deploy.CommitSHA, head)
	}

	// Repository gone
	postFakeGitHub(t, "/e2e/public", map[string]any{"owner": pollOwner, "name": "poll", "id": 5150001, "isGone": true})
	must(t, "rm -rf "+pollRepository+" && mkdir "+pollRepository)
	var detail pushAppDetail
	waitUntil(t, 4*pollPeriod, func() bool {
		detail = getPushApp(t, pollApp)
		return detail.PollError != ""
	})
	assertContains(t, detail.PollError, "wasn't found")
	time.Sleep(pollPeriod)
	if deploys := len(getPushApp(t, pollApp).Deploys); deploys != len(detail.Deploys) {
		t.Errorf("polling went on after it stopped: %d deploys, then %d", len(detail.Deploys), deploys)
	}
}

// pushToPolledRepository commits to the polled repository's main, as a
// developer pushing to GitHub would, and returns the new head.
func pushToPolledRepository(t *testing.T) string {
	t.Helper()
	return must(t, `set -e
		rm -rf /tmp/e2e-poll && git clone -q --branch main `+pollRepository+` /tmp/e2e-poll
		cd /tmp/e2e-poll && date > e2e-polled.txt && git add e2e-polled.txt
		git -c user.name=tero-e2e -c user.email=e2e@tero.test commit -qm "e2e polled change"
		git push -q origin main && git rev-parse HEAD`)
}

func disconnectGitHub(t *testing.T) {
	t.Helper()
	currentAdmin.confirm(t, "admin")
	if response := postDashboard(t, "admin", "/api/github/disconnect", map[string]string{}); response.status != 204 {
		t.Fatalf("disconnect = %d %s", response.status, response.body)
	}
}

// assertNoGitHubApp checks the key service holds neither the App's private
// key nor its webhook secret, and signs nothing for the agent.
func assertNoGitHubApp(t *testing.T) {
	t.Helper()

	assertContains(t, must(t, "sudo "+keysCall("github.status")), `"isConnected":false`)
	// As tero-keys, so SQLite's side files keep their owner.
	rows := must(t, `sudo -u tero-keys python3 -c 'import sqlite3
db = sqlite3.connect("/var/lib/tero-keys/keys.db")
print(db.execute("SELECT count(*) FROM github_app").fetchone()[0], db.execute("SELECT count(*) FROM github_connections").fetchone()[0])'`)
	if rows != "0 0" {
		t.Errorf("the key service still holds GitHub rows (App, connections): %s", rows)
	}
	if got := must(t, asAgent(keysCall("github.jwt"))); jwtPattern.MatchString(got) {
		t.Errorf("the agent got a JWT with no App: %s", got)
	}
}

func testDisconnectAndReconnect(t *testing.T) {
	waitForPushApp(t, privateApp)
	oldSecret := fakeGitHub(t).WebhookSecret

	// Disconnect
	disconnectGitHub(t)
	assertContains(t, gitHubStatus(t), `"isConnected":false`, `"installation":null`)
	if got := signedPush(t, oldSecret, deliveryGUID(300), strings.Repeat("e", 40), "e2e-disconnected"); got != 401 {
		t.Errorf("signed push after disconnecting = %d, want 401", got)
	}
	assertNoGitHubApp(t)
	if deploy := redeploy(t, privateApp).Deploys[0]; deploy.State != "failed" || !strings.Contains(deploy.Reason, connectGitHubInSettings) {
		t.Errorf("private deploy after disconnecting = %s: %q, want failed saying to %s", deploy.State, deploy.Reason, connectGitHubInSettings)
	}

	// Reconnect
	currentAdmin.confirm(t, "admin")
	connectGitHub(t, "")
	state := fakeGitHub(t)
	assertContains(t, gitHubStatus(t), `"isConnected":true`, `"slug":"`+state.Slug+`"`, `"name":"tero"`)
	deploysBefore := len(waitForPushApp(t, privateApp).Deploys)
	if got := signedPush(t, state.WebhookSecret, deliveryGUID(301), strings.Repeat("f", 40), "e2e-reconnected"); got != 202 {
		t.Fatalf("signed push after reconnecting = %d, want 202", got)
	}
	detail := waitForPushApp(t, privateApp)
	deploy := detail.Deploys[0]
	if len(detail.Deploys) != deploysBefore+1 || deploy.State != "live" || deploy.PushedBy != "e2e-reconnected" {
		t.Errorf("push after reconnecting = %d new deploys, newest %s by %q, want one live by e2e-reconnected",
			len(detail.Deploys)-deploysBefore, deploy.State, deploy.PushedBy)
	}

	testInstallationRemoved(t)
}

// testInstallationRemoved suspends the App's installation on GitHub, then
// removes it, then installs it again.
func testInstallationRemoved(t *testing.T) {
	postFakeGitHub(t, "/e2e/installation", map[string]bool{"isSuspended": true})
	sendInstallationEvent(t, deliveryGUID(310), "suspend")
	waitUntil(t, time.Minute, func() bool { return strings.Contains(gitHubStatus(t), `"isSuspended":true`) })
	if deploy := redeploy(t, privateApp).Deploys[0]; deploy.State != "failed" || deploy.Reason != notInstalled {
		t.Errorf("private deploy while suspended = %s: %q, want failed: %q", deploy.State, deploy.Reason, notInstalled)
	}

	postFakeGitHub(t, "/e2e/installation", map[string]bool{"isRemoved": true})
	sendInstallationEvent(t, deliveryGUID(311), "deleted")
	waitUntil(t, time.Minute, func() bool { return strings.Contains(gitHubStatus(t), `"installation":null`) })
	assertContains(t, gitHubStatus(t), `"isConnected":true`, `"installUrl":"https://github.com/apps/`)
	waitForPushApp(t, privateApp)
	if deploy := redeploy(t, privateApp).Deploys[0]; deploy.State != "failed" || !strings.Contains(deploy.Reason, connectGitHubInSettings) {
		t.Errorf("private deploy once uninstalled = %s: %q, want failed saying to %s", deploy.State, deploy.Reason, connectGitHubInSettings)
	}

	postFakeGitHub(t, "/e2e/installation", map[string]bool{})
	sendInstallationEvent(t, deliveryGUID(312), "created")
	waitUntil(t, time.Minute, func() bool { return strings.Contains(gitHubStatus(t), `"isSuspended":false`) })
	waitForPushApp(t, privateApp)
	if deploy := redeploy(t, privateApp).Deploys[0]; deploy.State != "live" {
		t.Errorf("private deploy once installed again = %s: %q", deploy.State, deploy.Reason)
	}
}

func testConnectOrganization(t *testing.T) {
	disconnectGitHub(t)
	postFakeGitHub(t, "/e2e/owner", map[string]string{"login": gitHubOrganization, "type": "Organization"})

	currentAdmin.confirm(t, "admin")
	start := connectGitHub(t, gitHubOrganization)
	assertContains(t, start.Action, "https://github.com/organizations/"+gitHubOrganization+"/settings/apps/new?state=")

	state := fakeGitHub(t)
	slug := state.Slug
	connectedApp = connectedGitHubApp{slug: slug, webhookSecret: state.WebhookSecret}
	assertContains(t, gitHubStatus(t),
		`"owner":"`+gitHubOrganization+`"`, `"isOrgOwned":true`,
		`"account":"`+gitHubOrganization+`"`, `"isOrganization":true`, `"name":"tero"`,
		`"settingsUrl":"https://github.com/organizations/`+gitHubOrganization+`/settings/apps/`+slug+`"`)
	waitForPushApp(t, privateApp)
}

// breachOutput is what testAssumeBreach's run of assume-breach printed.
var breachOutput string

// connectedGitHubApp is the App connected when the GitHub tests end. The
// fake GitHub doesn't come back after the reboot test, so it is kept here
// for testGitHubAfterBreach.
type connectedGitHubApp struct {
	slug          string
	webhookSecret string
}

var connectedApp connectedGitHubApp

// testGitHubAfterBreach runs after assume-breach, which found the
// organization's App connected.
func testGitHubAfterBreach(t *testing.T) {
	if connectedApp.slug == "" {
		t.Fatal("no App was connected when the GitHub tests ended")
	}
	settings := "https://github.com/organizations/" + gitHubOrganization + "/settings/apps/" + connectedApp.slug
	deleteLine := "Delete the GitHub App " + connectedApp.slug + " at " + settings
	assertContains(t, breachOutput, deleteLine)
	if strings.Contains(lastLine(breachOutput), deleteLine) {
		t.Error("the line to delete the App comes after the setup link")
	}

	assertNoGitHubApp(t)
	body := pushPayload(fakeRepositoryID, strings.Repeat("0", 40), "e2e-after-breach")
	if got := sendWebhook(t, "push", deliveryGUID(400), signWebhook(connectedApp.webhookSecret, body), body); got != 401 {
		t.Errorf("signed push after assume-breach = %d, want 401", got)
	}
}
