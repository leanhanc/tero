//go:build e2e

package e2e

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The e2e build fetches github.com/leanhanc/tero from a snapshot of this
// working tree that scripts/vm-test-repo.sh puts in the VM.
const (
	fixtureRepository = "https://github.com/leanhanc/tero"
	vmRepository      = "/opt/tero-e2e/repos/leanhanc/tero.git"
	redeployBranch    = "e2e-redeploy"
	deployTimeout     = 15 * time.Minute
)

// fixture is one app under e2e/fixtures and what its deploy should end in.
type fixture struct {
	name string
	// answer is part of the live app's response; empty for fixtures that fail.
	answer     string
	failedStep string
	reason     string
}

var fixtures = []fixture{
	{name: "npm", answer: "tero fixture npm on node"},
	{name: "pnpm", answer: "tero fixture pnpm on node"},
	{name: "yarn", answer: "tero fixture yarn on node"},
	{name: "bun", answer: "tero fixture bun on bun"},
	{name: "no-start", failedStep: "building", reason: `Add a "start" script to package.json`},
	{name: "never-listens", failedStep: "starting", reason: "didn't answer HTTP requests within 60 seconds"},
	{name: "build-fails", failedStep: "building", reason: "A build step failed"},
}

type appDetail struct {
	RunTogether bool
	Deploys     []struct {
		ID         int64
		State      string
		FailedStep string
		Reason     string
		CommitSHA  string `json:"commitSha"`
	}
}

func testDeploys(t *testing.T) {
	t.Run("apps: Not a GitHub URL (v5.0.0-1.3.6)", testNotGitHubURL)
	t.Run("apps: Default branch suggested", testDefaultBranchSuggested)
	t.Run("apps: Repository or branch not found / Path outside the repository", testCreateRefusals)

	t.Run("apps: Create a web app / build-pipeline: JavaScript package managers and runtimes / No start command / Railpack cannot plan", testFixtures)
	if t.Failed() {
		t.FailNow()
	}

	t.Run("apps: Invalid name / Name taken", testNameRules)
	t.Run("apps: App list / App page", testAppListAndPage)
	t.Run("deployments: App never listens / build-pipeline: Failed build log kept / Log rendered as text", testFailureLogs)
	t.Run("domains: New web app reachable without DNS / HTTP request to an app / Dashboard API on an app host", testAppAddresses)
	t.Run("domains: No route before live", testNoRouteBeforeLive)
	t.Run("process-isolation: Container options / Apps run as different host uids / App port not exposed", testContainerHardening)
	t.Run("process-isolation: Apps can't reach each other directly", testAppsIsolated)
	t.Run("process-isolation: Service sandbox unchanged", testServiceSandboxUnchanged)
	t.Run("build-pipeline: BuildKit lifecycle / No published port or socket / Image available after build", testBuildKitGone)
	t.Run("apps: App process exits", testAppProcessExits)
	t.Run("deployments: Redeploy picks up a new commit / Successful deploy / Redeploy while deploying", testRedeployNewCommit)
	t.Run("deployments: Failed redeploy", testFailedRedeploy)
	t.Run("deployments: Setting off, deploy succeeds / Setting off, deploy fails", testStopFirst)
	t.Run("process-isolation: Dashboard cannot trigger root work", testAppRoutesNoRootWork)
	t.Run("deployments: Interrupted deploy", testInterruptedDeploy)
}

func fixtureApp(name string) string {
	return "fx-" + name
}

func appAddress(app string) string {
	return app + "." + publicIP + ".nip.io"
}

// createApp creates an app through the dashboard API as the logged-in admin.
func createApp(t *testing.T, name, branch, rootDirectory string) dashboardResponse {
	t.Helper()
	return postDashboard(t, "admin", "/api/apps", map[string]string{
		"repository": fixtureRepository, "branch": branch, "displayName": "Fixture " + name, "name": name, "rootDirectory": rootDirectory,
	})
}

func getApp(t *testing.T, name string) appDetail {
	t.Helper()

	response := getDashboard(t, "admin", "/api/apps/"+name)
	if response.status != 200 {
		t.Fatalf("get %s = %d %s", name, response.status, response.body)
	}
	var detail appDetail
	if err := json.Unmarshal([]byte(response.body), &detail); err != nil {
		t.Fatal(err)
	}
	return detail
}

// waitForDeploy waits until the app's latest deploy is live or failed and
// returns the app.
func waitForDeploy(t *testing.T, name string) appDetail {
	t.Helper()

	var detail appDetail
	waitUntil(t, deployTimeout, func() bool {
		detail = getApp(t, name)
		state := detail.Deploys[0].State
		return state == "live" || state == "failed"
	})
	return detail
}

// fetchApp requests the app's address over HTTPS from inside the VM.
func fetchApp(t *testing.T, app, path string) result {
	t.Helper()
	host := appAddress(app)
	return run(t, fmt.Sprintf("curl -sS -k --max-time 5 --resolve %s:443:127.0.0.1 https://%s%s", host, host, path))
}

func expectField(t *testing.T, response dashboardResponse, field string) {
	t.Helper()
	if response.status != 422 || !strings.Contains(response.body, `"field":"`+field+`"`) {
		t.Errorf("response = %d %s, want 422 on %s", response.status, response.body, field)
	}
}

func testNotGitHubURL(t *testing.T) {
	// The VM's metadata-style address must never be fetched; the parser
	// refuses it before any request is built.
	for _, url := range []string{"http://169.254.169.254/latest/meta-data", "https://gitlab.com/leanhanc/tero", "file:///etc/passwd"} {
		expectField(t, postDashboard(t, "admin", "/api/repositories/lookup", map[string]string{"url": url}), "repository")
		expectField(t, postDashboard(t, "admin", "/api/apps", map[string]string{"repository": url, "branch": "main", "displayName": "Nope", "name": "nope"}), "repository")
	}
}

func testDefaultBranchSuggested(t *testing.T) {
	response := postDashboard(t, "admin", "/api/repositories/lookup", map[string]string{"url": fixtureRepository + ".git"})
	assertContains(t, response.body, `"repository":"`+fixtureRepository+`"`, `"defaultBranch":"main"`, `"repositoryName":"tero"`)
}

func testCreateRefusals(t *testing.T) {
	expectField(t, createApp(t, "nope", "no-such-branch", ""), "branch")
	expectField(t, postDashboard(t, "admin", "/api/apps", map[string]string{
		"repository": "https://github.com/leanhanc/no-such-repository-for-tero", "branch": "main", "displayName": "Nope", "name": "nope",
	}), "repository")
	for _, folder := range []string{"../", "/etc", "e2e/fixtures/missing"} {
		expectField(t, createApp(t, "nope", "main", folder), "rootDirectory")
	}
}

// testFixtures deploys every fixture and checks each ends where it should.
// The server builds one at a time, in the order they were created.
func testFixtures(t *testing.T) {
	for _, f := range fixtures {
		response := createApp(t, fixtureApp(f.name), "main", "e2e/fixtures/"+f.name)
		if response.status != 201 {
			t.Fatalf("create %s = %d %s", f.name, response.status, response.body)
		}
	}

	for _, f := range fixtures {
		deploy := waitForDeploy(t, fixtureApp(f.name)).Deploys[0]
		if f.answer != "" {
			if deploy.State != "live" {
				t.Errorf("%s ended %s at %q: %s", f.name, deploy.State, deploy.FailedStep, deploy.Reason)
				continue
			}
			// The proxy picks up a newly live release within a second.
			waitUntil(t, 15*time.Second, func() bool {
				return strings.Contains(fetchApp(t, fixtureApp(f.name), "/").output, f.answer)
			})
			assertContains(t, fetchApp(t, fixtureApp(f.name), "/").output, f.answer, "port 8080")
			continue
		}

		if deploy.State != "failed" || deploy.FailedStep != f.failedStep || !strings.Contains(deploy.Reason, f.reason) {
			t.Errorf("%s = %s at %q (%s), want failed at %s with %q", f.name, deploy.State, deploy.FailedStep, deploy.Reason, f.failedStep, f.reason)
		}
	}
}

func testNameRules(t *testing.T) {
	expectField(t, createApp(t, "Not.Valid", "main", ""), "name")
	expectField(t, createApp(t, fixtureApp("npm"), "main", "e2e/fixtures/npm"), "name")
}

func testAppListAndPage(t *testing.T) {
	list := getDashboard(t, "admin", "/api/apps").body
	for _, f := range fixtures {
		assertContains(t, list, `"name":"`+fixtureApp(f.name)+`"`, "https://"+appAddress(fixtureApp(f.name)))
	}

	page := getDashboard(t, "admin", "/api/apps/"+fixtureApp("npm")).body
	assertContains(t, page, `"repository":"`+fixtureRepository+`"`, `"branch":"main"`, `"rootDirectory":"e2e/fixtures/npm"`, `"state":"live"`)
}

func testFailureLogs(t *testing.T) {
	neverListens := getApp(t, fixtureApp("never-listens")).Deploys[0]
	log := getDashboard(t, "admin", fmt.Sprintf("/api/deploys/%d/log", neverListens.ID)).body
	assertContains(t, log, "working, but not listening on PORT", "event: done", `data: "failed"`)

	buildFails := getApp(t, fixtureApp("build-fails")).Deploys[0]
	log = getDashboard(t, "admin", fmt.Sprintf("/api/deploys/%d/log", buildFails.ID)).body
	assertContains(t, log, "this build fails on purpose")
	if strings.Contains(log, "\x1b[") {
		t.Error("the build log still has ANSI escape codes")
	}
}

func testAppAddresses(t *testing.T) {
	app := fixtureApp("npm")
	host := appAddress(app)

	must(t, "curl -sSk https://127.0.0.1:15000/roots/0 -o /tmp/pebble-root.pem")
	trusted := run(t, fmt.Sprintf("curl -sS --cacert /tmp/pebble-root.pem --resolve %s:443:127.0.0.1 https://%s/", host, host))
	assertContains(t, trusted.output, "tero fixture npm")

	redirect := must(t, fmt.Sprintf("curl -sS -o /dev/null -w '%%{http_code} %%{redirect_url}' --resolve %s:80:127.0.0.1 http://%s/path", host, host))
	if redirect != "308 https://"+host+"/path" {
		t.Errorf("HTTP request = %q, want a 308 to HTTPS", redirect)
	}

	apiOnApp := fetchApp(t, app, "/api/state").output
	assertContains(t, apiOnApp, "tero fixture npm")
	if strings.Contains(apiOnApp, "isLoggedIn") {
		t.Error("the dashboard API answered on an app's address")
	}
}

func testNoRouteBeforeLive(t *testing.T) {
	page := run(t, fmt.Sprintf("curl -sS -k -w '\\n%%{http_code}' --resolve %s:443:127.0.0.1 https://%s/", appAddress(fixtureApp("no-start")), appAddress(fixtureApp("no-start"))))
	assertContains(t, page.output, "isn't deployed yet", "503")
}

func liveContainer(t *testing.T, app string) string {
	t.Helper()
	return must(t, asTero(fmt.Sprintf(`podman ps --filter label=tero.app=%s --format '{{.Names}}'`, app)))
}

func testContainerHardening(t *testing.T) {
	npm := liveContainer(t, fixtureApp("npm"))
	bun := liveContainer(t, fixtureApp("bun"))

	options := must(t, asTero(`podman inspect --format '{{.HostConfig.CapAdd}} {{.EffectiveCaps}} {{.HostConfig.SecurityOpt}} {{.HostConfig.NetworkMode}} {{.HostConfig.PortBindings}}' `+npm))
	assertContains(t, options, "no-new-privileges", "127.0.0.1")
	if !strings.Contains(options, "pasta") && !strings.Contains(options, "slirp4netns") {
		t.Errorf("container isn't on a network namespace of its own: %s", options)
	}
	if !strings.HasPrefix(options, "[] []") {
		t.Errorf("container has capabilities: %s", options)
	}

	// --userns=auto gives the container a small slice of tero's range, not
	// the whole of it.
	appPID := must(t, asTero(`podman inspect --format '{{.State.Pid}}' `+npm))
	uidMap := must(t, "sudo cat /proc/"+appPID+"/uid_map")
	if strings.Contains(uidMap, "1000000") {
		t.Errorf("the app's user namespace maps tero's whole range:\n%s", uidMap)
	}

	npmUID := firstHostUID(t, npm)
	bunUID := firstHostUID(t, bun)
	if npmUID == bunUID {
		t.Errorf("two apps share host uid %s", npmUID)
	}
	subUIDStart, _ := strconv.Atoi(must(t, "grep '^tero:' /etc/subuid | cut -d: -f2"))
	for _, uid := range []string{npmUID, bunUID} {
		hostUID, _ := strconv.Atoi(uid)
		if hostUID < subUIDStart || hostUID >= subUIDStart+1_000_000 {
			t.Errorf("app host uid %s is outside tero's subordinate range starting at %d", uid, subUIDStart)
		}
	}

	port := must(t, asTero(`podman port `+npm+` 8080`))
	if !strings.HasPrefix(port, "127.0.0.1:") {
		t.Errorf("app port is published on %s, want 127.0.0.1 only", port)
	}
}

func firstHostUID(t *testing.T, container string) string {
	t.Helper()
	lines := strings.Split(must(t, asTero(`podman top `+container+` huser`)), "\n")
	if len(lines) < 2 {
		t.Fatalf("podman top %s printed no processes", container)
	}
	return strings.TrimSpace(lines[1])
}

func testAppsIsolated(t *testing.T) {
	npm := liveContainer(t, fixtureApp("npm"))
	bun := liveContainer(t, fixtureApp("bun"))
	bunPort := strings.TrimPrefix(must(t, asTero(`podman port `+bun+` 8080`)), "127.0.0.1:")
	vmIP := must(t, "hostname -I | awk '{print $1}'")

	// Every way the npm app could reach the host's loopback, where the bun
	// app's port is published. The Lima gateway is left out: Lima forwards
	// the VM's loopback ports to the Mac, which a real server doesn't do.
	for _, target := range []string{"127.0.0.1", "10.0.2.2", "169.254.1.2", "host.containers.internal", vmIP} {
		script := fmt.Sprintf(`const s=require("net").connect(%s,%q);s.setTimeout(3000);s.on("connect",()=>{console.log("REACHED");process.exit(0)});s.on("timeout",()=>{console.log("BLOCKED");process.exit(0)});s.on("error",()=>{console.log("BLOCKED");process.exit(0)})`, bunPort, target)
		got := must(t, asTero(fmt.Sprintf(`podman exec %s node -e '%s'`, npm, script)))
		if !strings.Contains(got, "BLOCKED") {
			t.Errorf("one app reached another's port through %s: %s", target, got)
		}
	}
}

func testServiceSandboxUnchanged(t *testing.T) {
	testServiceIdentity(t)
	processes := must(t, "cat /sys/fs/cgroup/system.slice/tero.service/cgroup.procs | wc -l")
	if processes != "1" {
		t.Errorf("the service's cgroup has %s processes, want only the service", processes)
	}
}

func testBuildKitGone(t *testing.T) {
	buildkit := must(t, asTero(`podman ps --all --filter name=tero-buildkit --format '{{.Names}}'`))
	if buildkit != "" {
		t.Errorf("BuildKit containers left after builds: %s", buildkit)
	}

	images := must(t, asTero(`podman images --format '{{.Repository}}:{{.Tag}}'`))
	assertContains(t, images, "localhost/tero/"+fixtureApp("npm")+":deploy-")
	if tarballs := run(t, "sudo ls /var/lib/tero/builds").output; strings.Contains(tarballs, ".tar") {
		t.Errorf("image tarballs left behind: %s", tarballs)
	}
}

func testAppProcessExits(t *testing.T) {
	app := fixtureApp("npm")
	must(t, asTero("podman kill "+liveContainer(t, app)))
	waitUntil(t, time.Minute, func() bool {
		return strings.Contains(fetchApp(t, app, "/").output, "tero fixture npm")
	})
}

// pushToRedeployBranch commits a change to the bun fixture on the e2e-redeploy
// branch of the VM's repository, as a developer pushing to GitHub would.
func pushToRedeployBranch(t *testing.T, indexTS string) {
	t.Helper()
	content := base64.StdEncoding.EncodeToString([]byte(indexTS))
	must(t, fmt.Sprintf(`set -e
		rm -rf /tmp/e2e-push && git clone -q --branch %s %s /tmp/e2e-push
		cd /tmp/e2e-push && echo %s | base64 -d > e2e/fixtures/bun/index.ts
		git -c user.name=tero-e2e -c user.email=e2e@tero.test commit -qam "e2e change"
		git push -q origin %s`, redeployBranch, vmRepository, content, redeployBranch))
}

func bunServer(text string) string {
	return "Bun.serve({ port: Number(process.env.PORT), fetch: () => new Response('" + text + "') });\n"
}

const neverListensBun = "console.log('working, but not listening on PORT');\nsetInterval(() => {}, 1000);\n"

// redeployAndWatch redeploys the app and requests it every second until the
// deploy ends, returning the deploy and how many requests failed.
func redeployAndWatch(t *testing.T, app string) (appDetail, int) {
	t.Helper()

	if response := postDashboard(t, "admin", "/api/apps/"+app+"/deploys", map[string]string{}); response.status != 202 {
		t.Fatalf("redeploy = %d %s", response.status, response.body)
	}

	failedRequests := 0
	var detail appDetail
	deadline := time.Now().Add(deployTimeout)
	for time.Now().Before(deadline) {
		if fetchApp(t, app, "/").exitCode != 0 {
			failedRequests++
		}
		detail = getApp(t, app)
		state := detail.Deploys[0].State
		if state == "live" || state == "failed" {
			return detail, failedRequests
		}
		time.Sleep(time.Second)
	}

	t.Fatalf("%s's redeploy didn't finish", app)
	return detail, failedRequests
}

const redeployApp = "fx-redeploy"

func testRedeployNewCommit(t *testing.T) {
	if response := createApp(t, redeployApp, redeployBranch, "e2e/fixtures/bun"); response.status != 201 {
		t.Fatalf("create = %d %s", response.status, response.body)
	}
	first := waitForDeploy(t, redeployApp).Deploys[0]
	if first.State != "live" {
		t.Fatalf("first deploy = %s: %s", first.State, first.Reason)
	}

	pushToRedeployBranch(t, bunServer("second commit"))
	if response := postDashboard(t, "admin", "/api/apps/"+redeployApp+"/deploys", map[string]string{}); response.status != 202 {
		t.Fatalf("redeploy = %d %s", response.status, response.body)
	}
	second := postDashboard(t, "admin", "/api/apps/"+redeployApp+"/deploys", map[string]string{})
	if second.status != 409 || !strings.Contains(second.body, "already in progress") {
		t.Errorf("redeploy while deploying = %d %s, want 409", second.status, second.body)
	}

	detail := waitForDeploy(t, redeployApp)
	if detail.Deploys[0].State != "live" || detail.Deploys[0].CommitSHA == first.CommitSHA {
		t.Fatalf("redeploy = %s on %s, want live on a new commit", detail.Deploys[0].State, detail.Deploys[0].CommitSHA)
	}
	if detail.Deploys[1].State != "superseded" {
		t.Errorf("previous deploy = %s, want superseded", detail.Deploys[1].State)
	}
	waitUntil(t, 15*time.Second, func() bool {
		return strings.Contains(fetchApp(t, redeployApp, "/").output, "second commit")
	})
}

func testFailedRedeploy(t *testing.T) {
	pushToRedeployBranch(t, neverListensBun)

	detail, failedRequests := redeployAndWatch(t, redeployApp)
	if detail.Deploys[0].State != "failed" || detail.Deploys[0].FailedStep != "starting" {
		t.Fatalf("redeploy = %s at %s, want failed at starting", detail.Deploys[0].State, detail.Deploys[0].FailedStep)
	}
	if failedRequests > 0 {
		t.Errorf("%d requests failed while the redeploy ran", failedRequests)
	}
	assertContains(t, fetchApp(t, redeployApp, "/").output, "second commit")
}

// testAppRoutesNoRootWork extends slice 1's check to the app routes: the
// service hands deploys to the agent through the database and never starts a
// process itself.
func testAppRoutesNoRootWork(t *testing.T) {
	servicePID := must(t, "systemctl show tero --property=MainPID --value")
	auditRule := "always,exit -F arch=b64 -S execve,execveat -F ppid=" + servicePID + " -k e2e-tero-app-exec"
	must(t, "sudo auditctl -a "+auditRule)
	defer run(t, "sudo auditctl -d "+auditRule)

	app := fixtureApp("npm")
	deployID := getApp(t, app).Deploys[0].ID
	postDashboard(t, "admin", "/api/repositories/lookup", map[string]string{"url": fixtureRepository})
	getDashboard(t, "admin", "/api/apps")
	getDashboard(t, "admin", "/api/apps/"+app)
	getDashboard(t, "admin", fmt.Sprintf("/api/deploys/%d/log", deployID))
	postDashboard(t, "admin", "/api/apps/"+app+"/display-name", map[string]string{"displayName": "Root audit"})
	postDashboard(t, "admin", "/api/apps/"+app+"/settings", map[string]bool{"runTogether": true})
	postDashboard(t, "admin", "/api/apps/"+app+"/deploys", map[string]string{})
	createApp(t, "root-audit", "main", "e2e/fixtures/npm")
	waitForDeploy(t, app)
	waitForDeploy(t, "root-audit")

	spawned := run(t, "sudo ausearch -k e2e-tero-app-exec --raw")
	if strings.Contains(spawned.output, "type=SYSCALL") {
		t.Errorf("the service started processes:\n%s", spawned.output)
	}
}

func testStopFirst(t *testing.T) {
	if response := postDashboard(t, "admin", "/api/apps/"+redeployApp+"/settings", map[string]bool{"runTogether": false}); response.status != 204 {
		t.Fatalf("settings = %d %s", response.status, response.body)
	}

	pushToRedeployBranch(t, bunServer("stopped first"))
	detail, _ := redeployAndWatch(t, redeployApp)
	if detail.Deploys[0].State != "live" {
		t.Fatalf("stop-first redeploy = %s: %s", detail.Deploys[0].State, detail.Deploys[0].Reason)
	}
	log := getDashboard(t, "admin", fmt.Sprintf("/api/deploys/%d/log", detail.Deploys[0].ID)).body
	assertContains(t, log, "Stopping the live release first")
	waitUntil(t, 15*time.Second, func() bool {
		return strings.Contains(fetchApp(t, redeployApp, "/").output, "stopped first")
	})

	pushToRedeployBranch(t, neverListensBun)
	detail, _ = redeployAndWatch(t, redeployApp)
	if detail.Deploys[0].State != "failed" {
		t.Fatalf("failing stop-first redeploy = %s", detail.Deploys[0].State)
	}
	waitUntil(t, time.Minute, func() bool {
		return strings.Contains(fetchApp(t, redeployApp, "/").output, "stopped first")
	})
}

func testInterruptedDeploy(t *testing.T) {
	app := fixtureApp("npm")
	if response := postDashboard(t, "admin", "/api/apps/"+app+"/deploys", map[string]string{}); response.status != 202 {
		t.Fatalf("redeploy = %d %s", response.status, response.body)
	}
	waitUntil(t, time.Minute, func() bool { return getApp(t, app).Deploys[0].State == "building" })

	must(t, agentSystemctl+" restart tero-agent")

	detail := waitForDeploy(t, app)
	if detail.Deploys[0].State != "failed" || !strings.Contains(detail.Deploys[0].Reason, "Tero restarted") {
		t.Errorf("interrupted deploy = %s: %s", detail.Deploys[0].State, detail.Deploys[0].Reason)
	}
	waitUntil(t, time.Minute, func() bool {
		return must(t, asTero(`podman ps --all --filter name=tero-buildkit --format '{{.Names}}'`)) == ""
	})
	assertContains(t, fetchApp(t, app, "/").output, "tero fixture npm")
}
