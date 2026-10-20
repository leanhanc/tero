package dashboard

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/leanhanc/tero/internal/deploylog"
)

func (d testDashboard) insertApp(t *testing.T, name, deployState string) {
	t.Helper()

	result, err := d.db.Exec("INSERT INTO apps (name, repository, branch, created_at) VALUES (?, 'https://github.com/o/r', 'main', 1)", name)
	if err != nil {
		t.Fatal(err)
	}
	appID, _ := result.LastInsertId()
	if _, err := d.db.Exec("INSERT INTO deploys (app_id, state, queued_at) VALUES (?, ?, 1)", appID, deployState); err != nil {
		t.Fatal(err)
	}
}

func TestAppRoutesNeedSession(t *testing.T) {
	d := newTestDashboard(t)

	for _, response := range []int{
		d.get("/api/apps").Code,
		d.post("/api/apps", map[string]string{"name": "api"}).Code,
		d.post("/api/apps/api/deploys", map[string]string{}).Code,
		d.get("/api/deploys/1/log").Code,
	} {
		if response != http.StatusUnauthorized {
			t.Errorf("status without a session = %d, want 401", response)
		}
	}
}

// apps: Invalid name. The name is checked before anything is fetched.
func TestCreateAppPointsAtTheInvalidField(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)

	response := d.post("/api/apps", map[string]string{"repository": "https://github.com/o/r", "branch": "main", "name": "My.App"}, session)

	var body struct{ Error, Field string }
	json.Unmarshal(response.Body.Bytes(), &body)
	if response.Code != http.StatusUnprocessableEntity || body.Field != "name" {
		t.Fatalf("response = %d %s, want 422 on name", response.Code, response.Body)
	}
}

// apps: Not a GitHub URL (v5.0.0-1.3.6)
func TestLookupRefusesOtherHosts_v5_0_0_1_3_6(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)

	response := d.post("/api/repositories/lookup", map[string]string{"url": "http://169.254.169.254/latest"}, session)

	var body struct{ Error, Field string }
	json.Unmarshal(response.Body.Bytes(), &body)
	if response.Code != http.StatusUnprocessableEntity || body.Field != "repository" {
		t.Fatalf("response = %d %s, want 422 on repository", response.Code, response.Body)
	}
}

// deployments: Redeploy while deploying
func TestRedeployRefusedWhileDeploying(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)
	d.insertApp(t, "api", "building")

	response := d.post("/api/apps/api/deploys", map[string]string{}, session)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "already in progress") {
		t.Fatalf("redeploy = %d %s, want 409", response.Code, response.Body)
	}
}

func TestListAndGetApps(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)
	d.insertApp(t, "api", "live")

	list := d.get("/api/apps", session)
	if !strings.Contains(list.Body.String(), `"address":"https://api.203.0.113.10.sslip.io"`) ||
		!strings.Contains(list.Body.String(), `"latestState":"live"`) {
		t.Fatalf("list = %s", list.Body)
	}

	if missing := d.get("/api/apps/nope", session); missing.Code != http.StatusNotFound {
		t.Fatalf("unknown app = %d, want 404", missing.Code)
	}
}

// build-pipeline: Failed build log kept
func TestLogStreamSendsTheLogThenTheFinalState(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)
	d.insertApp(t, "api", "failed")
	if err := os.WriteFile(deploylog.Path(d.logDir, 1), []byte("#1 ERROR: <b>boom</b>\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	response := d.get("/api/deploys/1/log", session)

	want := "event: log\ndata: \"#1 ERROR: \\u003cb\\u003eboom\\u003c/b\\u003e\\n\"\n\nevent: done\ndata: \"failed\"\n\n"
	if response.Header().Get("Content-Type") != "text/event-stream" || response.Body.String() != want {
		t.Fatalf("stream = %q", response.Body)
	}
}
