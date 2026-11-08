//go:build e2e

package e2e

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	variablesApp = "vars"
	// valueMarker starts every value the suite searches for. Searches write it
	// split, so a search never finds its own command line.
	valueMarker = "e2evaluemarker"
)

func markedValue(suffix string) string {
	return valueMarker + "-" + suffix
}

// searchPattern matches valueMarker without containing it.
func searchPattern() string {
	return "[" + valueMarker[:1] + "]" + valueMarker[1:]
}

type variableChange struct {
	Name      string  `json:"name"`
	IsRemoved bool    `json:"isRemoved,omitempty"`
	Value     *string `json:"value,omitempty"`
	Scope     string  `json:"scope,omitempty"`
	IsSealed  bool    `json:"isSealed,omitempty"`
}

func setVariable(name, value, scope string, isSealed bool) variableChange {
	return variableChange{Name: name, Value: &value, Scope: scope, IsSealed: isSealed}
}

func putVariables(t *testing.T, changes ...variableChange) dashboardResponse {
	t.Helper()

	encoded, err := json.Marshal(map[string]any{"changes": changes})
	if err != nil {
		t.Fatal(err)
	}
	payload := base64.StdEncoding.EncodeToString(encoded)

	return requestDashboard(t, "admin", fmt.Sprintf(
		`echo %s | base64 -d | curl -X POST -H 'Content-Type: application/json' -H 'Origin: https://%s' --data-binary @- `,
		payload, skippedDomain), "/api/apps/"+variablesApp+"/variables")
}

func mustSaveVariables(t *testing.T, changes ...variableChange) {
	t.Helper()
	if response := putVariables(t, changes...); response.status != 204 {
		t.Fatalf("save variables = %d %s", response.status, response.body)
	}
}

func redeployVariables(t *testing.T) appDetail {
	t.Helper()

	before := getApp(t, variablesApp).Deploys[0].ID
	response := postDashboard(t, "admin", "/api/apps/"+variablesApp+"/variables/redeploy", map[string]string{})
	if response.status != 202 {
		t.Fatalf("redeploy = %d %s", response.status, response.body)
	}
	waitUntil(t, 30*time.Second, func() bool { return getApp(t, variablesApp).Deploys[0].ID != before })

	return waitForDeploy(t, variablesApp)
}

type variablesAnswer struct {
	PID         int               `json:"pid"`
	Running     map[string]string `json:"running"`
	SeenByBuild []string          `json:"seenByBuild"`
}

// waitForAnswer waits until the app answers as isExpected wants: the proxy
// switches to a new release a moment after it goes live.
func waitForAnswer(t *testing.T, isExpected func(variablesAnswer) bool) variablesAnswer {
	t.Helper()

	var answer variablesAnswer
	waitUntil(t, time.Minute, func() bool {
		got := fetchApp(t, variablesApp, "/")
		answer = variablesAnswer{}
		return json.Unmarshal([]byte(got.output), &answer) == nil && isExpected(answer)
	})
	return answer
}

// liveVariablesContainer is the live release's container. The old one can
// still be running for a moment after a deploy goes live.
func liveVariablesContainer(t *testing.T) string {
	t.Helper()
	for _, deploy := range getApp(t, variablesApp).Deploys {
		if deploy.State == "live" {
			return fmt.Sprintf("tero-app-%s-%d", variablesApp, deploy.ID)
		}
	}
	t.Fatal("the app has no live deploy")
	return ""
}

func appVariables(t *testing.T) variablesAnswer {
	t.Helper()

	var answer variablesAnswer
	got := fetchApp(t, variablesApp, "/")
	if err := json.Unmarshal([]byte(got.output), &answer); err != nil {
		t.Fatalf("the app answered %q: %v", got.output, err)
	}
	return answer
}

func deployLog(t *testing.T, deployID int64) string {
	t.Helper()
	return must(t, fmt.Sprintf("sudo cat /var/lib/tero-shared/logs/deploys/%d.log", deployID))
}

func pendingNames(t *testing.T) []string {
	t.Helper()

	var detail struct {
		PendingChanges []string `json:"pendingChanges"`
	}
	response := getDashboard(t, "admin", "/api/apps/"+variablesApp)
	if err := json.Unmarshal([]byte(response.body), &detail); err != nil {
		t.Fatal(err)
	}
	return detail.PendingChanges
}

// updateAppRow writes to the shared database as a compromised dashboard could.
func updateAppRow(t *testing.T, assignment string) {
	t.Helper()
	script := fmt.Sprintf(`import sqlite3; c = sqlite3.connect("/var/lib/tero-shared/tero.db", timeout=10); c.execute("UPDATE apps SET %s WHERE name = '%s'"); c.commit()`, assignment, variablesApp)
	must(t, asAgent(fmt.Sprintf("python3 -c %q", script)))
}

func testVariables(t *testing.T) {
	if response := createApp(t, variablesApp, "main", "e2e/fixtures/variables"); response.status != 201 {
		t.Fatalf("create = %d %s", response.status, response.body)
	}
	if detail := waitForDeploy(t, variablesApp); detail.Deploys[0].State != "live" {
		t.Fatalf("first deploy = %+v", detail.Deploys[0])
	}

	t.Run("app-variables: Save deploys nothing / Sealed value never returned / Fresh check", testSaveVariables)
	if t.Failed() {
		t.FailNow()
	}
	t.Run("app-variables: Build variable changed / Runtime only / Build only / Both / build-pipeline: Build prints a value", testVariablesReachTheirPhases)
	t.Run("process-isolation: Disk search / Not visible to other processes / Image's own command kept / build-pipeline: No value in the image", testValuesStayOffTheDisk)
	t.Run("app-variables: Runtime variable changed / Read list", testRestartWithNoBuild)
	t.Run("app-variables: New values break the app / build-pipeline: App prints a value and exits", testNewValuesBreakTheApp)
	t.Run("deployments: Key service down at boot / Reboot / App exits", testVariablesWithoutTheKeyService)
	t.Run("app-variables: Rewritten source refused / Image from another source / Approving a new source", testSourceApproval)
}

func testSaveVariables(t *testing.T) {
	// The app has no sealed variable yet, so a live session is enough, and
	// nothing runs until the admin redeploys.
	mustSaveVariables(t, setVariable("E2E_RUNTIME", markedValue("runtime"), "runtime", false))
	if names := pendingNames(t); !slices.Equal(names, []string{"E2E_RUNTIME"}) {
		t.Errorf("pending = %v", names)
	}
	// The proxy switches to the first release a moment after it goes live.
	if got := waitForAnswer(t, func(variablesAnswer) bool { return true }); len(got.Running) != 0 {
		t.Errorf("saving reached the running app: %+v", got.Running)
	}
	if state := getApp(t, variablesApp).Deploys[0].State; state != "live" {
		t.Errorf("saving queued a deploy: state %s", state)
	}

	// The session may still be confirmed from an earlier test, so the refusal
	// of an unconfirmed seal is checked by unit tests rather than by waiting 5
	// minutes here.
	currentAdmin.confirm(t, "admin")
	mustSaveVariables(t,
		setVariable("E2E_SEALED", markedValue("sealed"), "runtime", true),
		setVariable("E2E_BUILD", markedValue("build"), "build", false),
		setVariable("E2E_BOTH", markedValue("both"), "both", false),
		setVariable("E2E_PRINTED_IN_BUILD", markedValue("printed"), "build", true),
	)

	listed := getDashboard(t, "admin", "/api/apps/"+variablesApp+"/variables")
	if listed.status != 200 {
		t.Fatalf("list = %d %s", listed.status, listed.body)
	}
	assertContains(t, listed.body, "E2E_SEALED", markedValue("runtime"))
	if strings.Contains(listed.body, markedValue("sealed")) || strings.Contains(listed.body, markedValue("printed")) {
		t.Errorf("a sealed value was returned: %s", listed.body)
	}
}

func testVariablesReachTheirPhases(t *testing.T) {
	detail := redeployVariables(t)
	deploy := detail.Deploys[0]
	if deploy.State != "live" {
		t.Fatalf("deploy = %+v\n%s", deploy, deployLog(t, deploy.ID))
	}

	got := waitForAnswer(t, func(answer variablesAnswer) bool { return answer.Running["E2E_RUNTIME"] != "" })
	for name, want := range map[string]string{
		"E2E_RUNTIME": markedValue("runtime"), "E2E_SEALED": markedValue("sealed"), "E2E_BOTH": markedValue("both"),
	} {
		if got.Running[name] != want {
			t.Errorf("%s = %q at runtime, want %q", name, got.Running[name], want)
		}
	}
	for _, name := range []string{"E2E_BUILD", "E2E_PRINTED_IN_BUILD"} {
		if _, isSet := got.Running[name]; isSet {
			t.Errorf("build-only %s reached the running app", name)
		}
	}

	for _, name := range []string{"E2E_BUILD", "E2E_BOTH", "E2E_PRINTED_IN_BUILD"} {
		if !slices.Contains(got.SeenByBuild, name) {
			t.Errorf("the build didn't see %s: %v", name, got.SeenByBuild)
		}
	}
	for _, name := range []string{"E2E_RUNTIME", "E2E_SEALED"} {
		if slices.Contains(got.SeenByBuild, name) {
			t.Errorf("runtime-only %s reached the build", name)
		}
	}

	log := deployLog(t, deploy.ID)
	assertContains(t, log, "e2e printing [hidden]")
	if strings.Contains(log, valueMarker) {
		t.Errorf("a value is in the build log:\n%s", log)
	}
	if names := pendingNames(t); len(names) != 0 {
		t.Errorf("the notice stayed after a live deploy: %v", names)
	}
}

func testValuesStayOffTheDisk(t *testing.T) {
	pattern := searchPattern()
	for _, directory := range []string{"/var/lib/tero-agent", "/var/lib/tero-shared", "/var/lib/tero", "/var/lib/tero-keys"} {
		if found := run(t, fmt.Sprintf("sudo grep -rls --binary-files=text '%s' %s", pattern, directory)).output; strings.TrimSpace(found) != "" {
			t.Errorf("a value is on disk under %s:\n%s", directory, found)
		}
	}
	if found := run(t, fmt.Sprintf("sudo journalctl --no-pager | grep '%s'", pattern)).output; strings.TrimSpace(found) != "" {
		t.Errorf("a value is in the journal:\n%s", found)
	}
	if found := run(t, fmt.Sprintf("sudo grep -l --binary-files=text '%s' /proc/[0-9]*/cmdline 2>/dev/null", pattern)).output; strings.TrimSpace(found) != "" {
		t.Errorf("a value is on a command line:\n%s", found)
	}

	container := liveVariablesContainer(t)
	if inspected := must(t, asAgent("podman inspect "+container)); strings.Contains(inspected, valueMarker) {
		t.Error("podman inspect shows a value")
	}

	uid := must(t, "id -u tero-agent")
	if got := run(t, asTero(fmt.Sprintf("cat /run/user/%s/tero-env/%s", uid, container))); got.exitCode == 0 {
		t.Error("a process running as tero read the app's values")
	}

	// PID 1 is the image's own command, not Tero's helper.
	pid := must(t, asAgent(`podman inspect --format '{{.State.Pid}}' `+container))
	command := must(t, fmt.Sprintf("sudo tr '\\0' ' ' < /proc/%s/cmdline", pid))
	if strings.Contains(command, "app-entry") {
		t.Errorf("the app's first process is still the helper: %s", command)
	}

	otherApp := liveContainer(t, fixtureApp("npm"))
	if got := run(t, asAgent(fmt.Sprintf("podman exec %s cat /run/tero/env", otherApp))); got.exitCode == 0 {
		t.Error("another app has an env file")
	}
}

func testRestartWithNoBuild(t *testing.T) {
	before := getApp(t, variablesApp).Deploys[0]
	currentAdmin.confirm(t, "admin")
	mustSaveVariables(t, setVariable("E2E_RUNTIME", markedValue("runtime-2"), "runtime", false))

	detail := redeployVariables(t)
	deploy := detail.Deploys[0]
	if deploy.State != "live" {
		t.Fatalf("deploy = %+v\n%s", deploy, deployLog(t, deploy.ID))
	}
	if deploy.CommitSHA != before.CommitSHA {
		t.Errorf("commit changed: %s, was %s", deploy.CommitSHA, before.CommitSHA)
	}
	log := deployLog(t, deploy.ID)
	if strings.Contains(log, "Fetching") || strings.Contains(log, "Building the image") {
		t.Errorf("a restart fetched or built:\n%s", log)
	}
	waitForAnswer(t, func(answer variablesAnswer) bool { return answer.Running["E2E_RUNTIME"] == markedValue("runtime-2") })

	reads := getDashboard(t, "admin", "/api/apps/"+variablesApp+"/variables/E2E_SEALED/reads")
	assertContains(t, reads.body, fmt.Sprintf(`"deployId":%d`, deploy.ID), `"phase":"run"`)
}

func testNewValuesBreakTheApp(t *testing.T) {
	currentAdmin.confirm(t, "admin")
	mustSaveVariables(t, setVariable("E2E_CRASH", markedValue("crash"), "runtime", true))

	detail := redeployVariables(t)
	deploy := detail.Deploys[0]
	if deploy.State != "failed" {
		t.Fatalf("deploy = %+v", deploy)
	}
	log := deployLog(t, deploy.ID)
	assertContains(t, log, "e2e crashing with [hidden]")
	if strings.Contains(log, valueMarker) {
		t.Errorf("a value is in the deploy log:\n%s", log)
	}

	if got := appVariables(t); got.Running["E2E_RUNTIME"] != markedValue("runtime-2") {
		t.Errorf("the previous release stopped serving: %+v", got)
	}
	if names := pendingNames(t); !slices.Contains(names, "E2E_CRASH") {
		t.Errorf("a failed deploy cleared the notice: %v", names)
	}

	currentAdmin.confirm(t, "admin")
	mustSaveVariables(t, variableChange{Name: "E2E_CRASH", IsRemoved: true})
	if detail := redeployVariables(t); detail.Deploys[0].State != "live" {
		t.Fatalf("deploy after removing the bad value = %+v", detail.Deploys[0])
	}
}

// testVariablesWithoutTheKeyService empties tmpfs as a reboot does, with the
// key service stopped. Tero's own reboot test runs later on the real thing.
func testVariablesWithoutTheKeyService(t *testing.T) {
	container := liveVariablesContainer(t)
	uid := must(t, "id -u tero-agent")

	// An app that merely exits still has its values.
	must(t, "sudo systemctl stop tero-keys.socket tero-keys.service")
	t.Cleanup(func() { run(t, "sudo systemctl start tero-keys.socket") })
	must(t, asAgent("podman stop --time 1 "+container))
	waitUntil(t, 3*time.Minute, func() bool {
		return strings.Contains(fetchApp(t, variablesApp, "/").output, markedValue("runtime-2"))
	})

	must(t, asAgent("podman stop --time 1 "+container))
	must(t, asAgent(fmt.Sprintf("rm /run/user/%s/tero-env/%s", uid, container)))
	waitUntil(t, time.Minute, func() bool {
		return strings.Contains(getDashboard(t, "admin", "/api/apps/"+variablesApp).body, "key service")
	})
	if got := fetchApp(t, fixtureApp("npm"), "/"); !strings.Contains(got.output, "tero fixture npm") {
		t.Errorf("an app with no variables stopped: %s", got.output)
	}

	must(t, "sudo systemctl start tero-keys.socket")
	waitUntil(t, 3*time.Minute, func() bool {
		return strings.Contains(fetchApp(t, variablesApp, "/").output, markedValue("runtime-2"))
	})
	waitUntil(t, 30*time.Second, func() bool {
		return !strings.Contains(getDashboard(t, "admin", "/api/apps/"+variablesApp).body, "can't start")
	})
}

func testSourceApproval(t *testing.T) {
	updateAppRow(t, "branch = '"+redeployBranch+"'")

	response := postDashboard(t, "admin", "/api/apps/"+variablesApp+"/deploys", map[string]string{})
	if response.status != 202 {
		t.Fatalf("redeploy = %d %s", response.status, response.body)
	}
	waitUntil(t, time.Minute, func() bool { return getApp(t, variablesApp).Deploys[0].State == "failed" })
	deploy := getApp(t, variablesApp).Deploys[0]
	assertContains(t, deploy.Reason, "source changed")
	if log := deployLog(t, deploy.ID); strings.Contains(log, "Fetching") {
		t.Errorf("a refused source fetched anyway:\n%s", log)
	}
	assertContains(t, must(t, "sudo journalctl -u tero-keys --no-pager | tail -50"), "unapproved source")

	listed := getDashboard(t, "admin", "/api/apps/"+variablesApp+"/variables")
	assertContains(t, listed.body, `"isSourceApproved":false`)

	// The running app keeps its values, and the page never asks it to stop.
	if got := appVariables(t); got.Running["E2E_SEALED"] != markedValue("sealed") {
		t.Errorf("the running app lost its values: %+v", got)
	}

	updateAppRow(t, "branch = 'main'")
	if listed := getDashboard(t, "admin", "/api/apps/"+variablesApp+"/variables"); !strings.Contains(listed.body, `"isSourceApproved":true`) {
		t.Errorf("restoring the source: %s", listed.body)
	}

}

// testVariablesAfterRotation runs once `sudo tero rotate-key` has run.
func testVariablesAfterRotation(t *testing.T) {
	container := liveVariablesContainer(t)
	uid := must(t, "id -u tero-agent")
	must(t, asAgent("podman stop --time 1 "+container))
	must(t, asAgent(fmt.Sprintf("rm /run/user/%s/tero-env/%s", uid, container)))
	waitUntil(t, 3*time.Minute, func() bool {
		return strings.Contains(fetchApp(t, variablesApp, "/").output, markedValue("sealed"))
	})
}
