package dashboard

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/leanhanc/tero/internal/keys"
)

func (d testDashboard) putVariables(body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	encoded, _ := json.Marshal(body)
	request := httptest.NewRequest(http.MethodPost, "https://dash.example.com/api/apps/api/variables", bytes.NewReader(encoded))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://dash.example.com")
	request.RemoteAddr = "198.51.100.7:51000"
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	d.handler.ServeHTTP(recorder, request)
	return recorder
}

func change(name, value string, scope keys.VariableScope, isSealed bool) map[string]any {
	return map[string]any{"name": name, "value": value, "scope": scope, "isSealed": isSealed}
}

func (d testDashboard) deployRows(t *testing.T) (count int, needsBuild bool) {
	t.Helper()
	err := d.db.QueryRow("SELECT count(*), coalesce(max(needs_build), 0) FROM deploys WHERE state = 'queued'").Scan(&count, &needsBuild)
	if err != nil {
		t.Fatal(err)
	}
	return count, needsBuild
}

func TestVariableRoutesNeedSession(t *testing.T) {
	d := newTestDashboard(t)
	d.insertApp(t, "api", "live")

	for _, code := range []int{
		d.get("/api/apps/api/variables").Code,
		d.putVariables(map[string]any{"changes": []any{}}).Code,
		d.post("/api/apps/api/variables/redeploy", nil).Code,
		d.post("/api/apps/api/variables/approve-source", nil).Code,
		d.get("/api/apps/api/variables/X/reads").Code,
	} {
		if code != http.StatusUnauthorized {
			t.Errorf("status without a session = %d, want 401", code)
		}
	}
}

// app-variables: Save deploys nothing, Several saves counted once per variable,
// Runtime variable changed, Sealed value never returned
func TestSaveRecordsPendingAndRedeployRestarts(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)
	d.insertApp(t, "api", "live")

	for _, save := range []map[string]any{
		{"changes": []any{change("A", "1", keys.ScopeRuntime, false)}},
		{"changes": []any{change("A", "2", keys.ScopeRuntime, false), change("B", "x", keys.ScopeRuntime, false)}},
	} {
		if response := d.putVariables(save, session); response.Code != http.StatusNoContent {
			t.Fatalf("save = %d %s", response.Code, response.Body)
		}
	}

	if count, _ := d.deployRows(t); count != 0 {
		t.Errorf("a save queued %d deploys", count)
	}

	var detail struct {
		PendingChanges    []string `json:"pendingChanges"`
		PendingNeedsBuild bool     `json:"pendingNeedsBuild"`
	}
	json.Unmarshal(d.get("/api/apps/api", session).Body.Bytes(), &detail)
	if len(detail.PendingChanges) != 2 || detail.PendingNeedsBuild {
		t.Errorf("detail = %+v", detail)
	}

	var list struct {
		Apps []struct {
			PendingChanges int `json:"pendingChanges"`
		} `json:"apps"`
	}
	json.Unmarshal(d.get("/api/apps", session).Body.Bytes(), &list)
	if len(list.Apps) != 1 || list.Apps[0].PendingChanges != 2 {
		t.Errorf("list = %+v", list)
	}

	if response := d.post("/api/apps/api/variables/redeploy", nil, session); response.Code != http.StatusAccepted {
		t.Fatalf("redeploy = %d %s", response.Code, response.Body)
	}
	if count, needsBuild := d.deployRows(t); count != 1 || needsBuild {
		t.Errorf("queued %d deploys, needsBuild %v", count, needsBuild)
	}
}

// app-variables: Build variable changed
func TestBuildChangesRedeployWithABuild(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)
	d.insertApp(t, "api", "live")

	saved := d.putVariables(map[string]any{"changes": []any{change("A", "1", keys.ScopeBoth, false)}}, session)
	if saved.Code != http.StatusNoContent {
		t.Fatalf("save = %d %s", saved.Code, saved.Body)
	}
	d.post("/api/apps/api/variables/redeploy", nil, session)

	if count, needsBuild := d.deployRows(t); count != 1 || !needsBuild {
		t.Errorf("queued %d deploys, needsBuild %v", count, needsBuild)
	}
}

func TestRedeployWithNothingPendingDoesNothing(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)
	d.insertApp(t, "api", "live")

	d.post("/api/apps/api/variables/redeploy", nil, session)

	if count, _ := d.deployRows(t); count != 0 {
		t.Errorf("queued %d deploys", count)
	}
}

// app-variables: Sealed value never returned, Unconfirmed seal
func TestSealedValuesAndConfirmation(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)
	d.insertApp(t, "api", "live")
	const secretValue = "sealed-marker-value"

	saved := d.putVariables(map[string]any{"changes": []any{change("API_KEY", secretValue, keys.ScopeRuntime, true)}}, session)
	if saved.Code != http.StatusNoContent {
		t.Fatalf("seal on the setup's confirmed session = %d %s", saved.Code, saved.Body)
	}

	listed := d.get("/api/apps/api/variables", session)
	if listed.Code != http.StatusOK || strings.Contains(listed.Body.String(), secretValue) {
		t.Errorf("list = %d %s", listed.Code, listed.Body)
	}
	var response variablesResponse
	json.Unmarshal(listed.Body.Bytes(), &response)
	if len(response.Variables) != 1 || !response.Variables[0].IsSealed || !response.IsSourceApproved {
		t.Errorf("response = %+v", response)
	}

	*d.now = d.now.Add(6 * 60 * 1e9)
	refused := d.putVariables(map[string]any{"changes": []any{change("NODE_OPTIONS", "--require=/evil", keys.ScopeRuntime, false)}}, session)
	if refused.Code != http.StatusForbidden || !strings.Contains(refused.Body.String(), notConfirmedCode) {
		t.Errorf("unconfirmed change = %d %s", refused.Code, refused.Body)
	}
}

// app-variables: Invalid save changes nothing
func TestInvalidVariablesExplainTheRules(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)
	d.insertApp(t, "api", "live")

	response := d.putVariables(map[string]any{"changes": []any{change("PORT", "1", keys.ScopeRuntime, false)}}, session)
	if response.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d %s", response.Code, response.Body)
	}
}

// app-variables: Approving a new source
func TestSourceMismatchIsShownAndApproved(t *testing.T) {
	d := newTestDashboard(t)
	session := d.setUp(t)
	d.insertApp(t, "api", "live")
	d.putVariables(map[string]any{"changes": []any{change("A", "1", keys.ScopeRuntime, false)}}, session)

	if _, err := d.db.Exec("UPDATE apps SET repository = 'https://github.com/evil/r' WHERE name = 'api'"); err != nil {
		t.Fatal(err)
	}

	var response variablesResponse
	json.Unmarshal(d.get("/api/apps/api/variables", session).Body.Bytes(), &response)
	if response.IsSourceApproved || response.ApprovedSource.Repository != "https://github.com/o/r" || response.Source.Repository != "https://github.com/evil/r" {
		t.Errorf("response = %+v", response)
	}

	if approved := d.post("/api/apps/api/variables/approve-source", nil, session); approved.Code != http.StatusNoContent {
		t.Fatalf("approve = %d %s", approved.Code, approved.Body)
	}
	json.Unmarshal(d.get("/api/apps/api/variables", session).Body.Bytes(), &response)
	if !response.IsSourceApproved {
		t.Errorf("still not approved: %+v", response)
	}
}
