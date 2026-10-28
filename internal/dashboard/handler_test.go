package dashboard

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/leanhanc/tero/internal/apps"
	"github.com/leanhanc/tero/internal/auth"
	"github.com/leanhanc/tero/internal/githubapp"
	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/password"
	"github.com/leanhanc/tero/internal/store"
	"github.com/leanhanc/tero/internal/totp"
)

const adminPassword = "a long passphrase nobody has used"

type noBreaches struct{}

func (noBreaches) IsBreached(context.Context, string) (bool, error) { return false, nil }

type testDashboard struct {
	handler *Handler
	service *auth.Service
	now     *time.Time
	// secret is the TOTP secret setUp enrolled.
	secret *string
	// refusals holds the TERO_REASON of each refused request sent to the
	// journal.
	refusals *[]string
	db       *sql.DB
	logDir   string
	// keysRoot and keysAgent are root's and the agent's clients of the
	// in-process key service.
	keysRoot  *keys.Client
	keysAgent *keys.Client
}

func newTestDashboard(t *testing.T) testDashboard {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	keysDB, err := store.OpenWith(context.Background(), filepath.Join(t.TempDir(), "keys.db"), keys.Migrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { keysDB.Close() })

	now := time.Unix(1_800_000_000, 0)
	secret := ""
	refusals := &[]string{}
	keyService, err := keys.NewService(keysDB, map[int][]byte{1: make([]byte, 32)}, keys.Options{
		Domain: "dash.example.com",
		Now:    func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	service := auth.New(auth.Options{
		Keys:      keys.NewLocalClient(keyService, keys.CallerDashboard),
		Domain:    "dash.example.com",
		Passwords: password.Policy{Breaches: noBreaches{}},
		Journal: func(_ int, _ string, fields map[string]string) error {
			if fields["TERO_EVENT"] == "request_refused" {
				*refusals = append(*refusals, fields["TERO_REASON"])
			}
			return nil
		},
	})

	logDir := t.TempDir()
	appService := apps.NewService(db, "203.0.113.10", func() time.Time { return now })
	dashboardKeys := keys.NewLocalClient(keyService, keys.CallerDashboard)
	clock := func() time.Time { return now }
	gitHub := GitHub{
		Connection: githubapp.Connection{DB: db, Keys: dashboardKeys, Domain: "dash.example.com", Now: clock},
		Webhooks:   githubapp.Webhooks{DB: db, Events: dashboardKeys, Now: clock},
		Verifier:   dashboardKeys,
	}
	handler := NewHandler(service, appService, gitHub, "dash.example.com", logDir)

	return testDashboard{handler: handler, service: service, db: db, logDir: logDir, now: &now, secret: &secret, refusals: refusals,
		keysRoot: keys.NewLocalClient(keyService, keys.CallerRoot), keysAgent: keys.NewLocalClient(keyService, keys.CallerAgent)}
}

func (d testDashboard) post(path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	encoded, _ := json.Marshal(body)
	request := httptest.NewRequest(http.MethodPost, "https://dash.example.com"+path, bytes.NewReader(encoded))
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

func (d testDashboard) get(path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, "https://dash.example.com"+path, nil)
	request.RemoteAddr = "198.51.100.7:51000"
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}

	recorder := httptest.NewRecorder()
	d.handler.ServeHTTP(recorder, request)
	return recorder
}

// setUp completes setup through the API and returns the session cookie.
func (d testDashboard) setUp(t *testing.T) *http.Cookie {
	t.Helper()
	token, err := d.keysRoot.IssueSetupLink(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	begin := d.post("/api/setup/begin", map[string]string{"token": token, "password": adminPassword})
	if begin.Code != http.StatusOK {
		t.Fatalf("setup/begin = %d %s", begin.Code, begin.Body)
	}
	var enrolment auth.Enrolment
	json.Unmarshal(begin.Body.Bytes(), &enrolment)
	*d.secret = enrolment.Secret

	code, _ := totp.Code(enrolment.Secret, *d.now)
	finish := d.post("/api/setup/finish", map[string]string{"token": token, "code": code})
	if finish.Code != http.StatusNoContent {
		t.Fatalf("setup/finish = %d %s", finish.Code, finish.Body)
	}

	for _, cookie := range finish.Result().Cookies() {
		if cookie.Name == auth.SessionCookie {
			return cookie
		}
	}
	t.Fatal("setup set no session cookie")
	return nil
}

func TestCookieAttributes(t *testing.T) {
	d := newTestDashboard(t)
	token, _ := d.keysRoot.IssueSetupLink(context.Background())
	begin := d.post("/api/setup/begin", map[string]string{"token": token, "password": adminPassword})
	var enrolment auth.Enrolment
	json.Unmarshal(begin.Body.Bytes(), &enrolment)
	code, _ := totp.Code(enrolment.Secret, *d.now)

	cookies := d.post("/api/setup/finish", map[string]string{"token": token, "code": code}).Result().Cookies()
	if len(cookies) != 2 {
		t.Fatalf("setup set %d cookies, want the session and device cookies", len(cookies))
	}
	for _, cookie := range cookies {
		isHostPrefixed := strings.HasPrefix(cookie.Name, "__Host-")
		if !isHostPrefixed || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Domain != "" {
			t.Errorf("cookie = %+v", cookie)
		}
	}
}

func TestEventsNeedSession(t *testing.T) {
	d := newTestDashboard(t)
	cookie := d.setUp(t)

	if response := d.get("/api/events"); response.Code != http.StatusUnauthorized {
		t.Errorf("events without session = %d", response.Code)
	}

	response := d.get("/api/events", cookie)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "setup_completed") {
		t.Errorf("events with session = %d %s", response.Code, response.Body)
	}
}

func TestCrossSiteRequestsRefused(t *testing.T) {
	d := newTestDashboard(t)

	request := httptest.NewRequest(http.MethodPost, "https://dash.example.com/api/login", strings.NewReader(`{"password":"x","code":"1"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://evil.example")
	recorder := httptest.NewRecorder()
	d.handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("cross-origin login = %d", recorder.Code)
	}
}

func TestFormPostsRefused(t *testing.T) {
	d := newTestDashboard(t)

	request := httptest.NewRequest(http.MethodPost, "https://dash.example.com/api/login", strings.NewReader("password=x&code=1"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	d.handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("form post = %d", recorder.Code)
	}
}

func TestLoginBeforeSetupPointsToSetupLink(t *testing.T) {
	d := newTestDashboard(t)

	response := d.post("/api/login", map[string]string{"password": adminPassword, "code": "123456"})
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "setup link") {
		t.Errorf("login before setup = %d %s", response.Code, response.Body)
	}
}

func TestRateLimitedLoginHasRetryAfter(t *testing.T) {
	d := newTestDashboard(t)
	d.setUp(t)

	var response *httptest.ResponseRecorder
	for range 6 {
		response = d.post("/api/login", map[string]string{"password": "wrong passphrase here", "code": "000000"})
	}

	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Errorf("sixth failed login = %d, Retry-After %q", response.Code, response.Header().Get("Retry-After"))
	}
}

func TestAPIResponsesAreNotCached(t *testing.T) {
	d := newTestDashboard(t)

	if got := d.get("/api/state").Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
}

func TestAppPagesServeIndexAndAPIStaysJSON(t *testing.T) {
	d := newTestDashboard(t)

	for _, path := range []string{"/", "/setup", "/login"} {
		response := d.get(path)
		if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/html") {
			t.Errorf("GET %s = %d %s", path, response.Code, response.Header().Get("Content-Type"))
		}
	}

	if response := d.get("/api/nothing-here"); response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"error"`) {
		t.Errorf("unknown API path = %d %s", response.Code, response.Body)
	}
	if response := d.get("/assets/missing.js"); response.Code != http.StatusNotFound {
		t.Errorf("missing asset = %d, want 404 rather than the app page", response.Code)
	}
}

func TestSetupReturnsScannableQRCode(t *testing.T) {
	d := newTestDashboard(t)
	token, _ := d.keysRoot.IssueSetupLink(context.Background())

	response := d.post("/api/setup/begin", map[string]string{"token": token, "password": adminPassword})
	var body struct{ Secret, URI, QRCode string }
	json.Unmarshal(response.Body.Bytes(), &body)

	if !strings.HasPrefix(body.QRCode, "data:image/png;base64,") || body.Secret == "" || body.URI == "" {
		t.Errorf("setup/begin = %s", response.Body)
	}
}

// login waits for the next TOTP step, so the code isn't a replay, and logs in
// with the given cookies.
func (d testDashboard) login(t *testing.T, cookies ...*http.Cookie) *http.Cookie {
	t.Helper()
	*d.now = d.now.Add(30 * time.Second)
	code, _ := totp.Code(*d.secret, *d.now)

	response := d.post("/api/login", map[string]string{"password": adminPassword, "code": code}, cookies...)
	if response.Code != http.StatusNoContent {
		t.Fatalf("login = %d %s", response.Code, response.Body)
	}

	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == auth.SessionCookie {
			return cookie
		}
	}
	t.Fatal("login set no session cookie")
	return nil
}

func TestLogin_v5_0_0_7_2_4_EndsThePreviousSession(t *testing.T) {
	d := newTestDashboard(t)
	previous := d.setUp(t)

	current := d.login(t, previous)

	if response := d.get("/api/events", previous); response.Code != http.StatusUnauthorized {
		t.Errorf("events with the session from before login = %d", response.Code)
	}
	if response := d.get("/api/events", current); response.Code != http.StatusOK {
		t.Errorf("events with the new session = %d", response.Code)
	}
}

func TestLogout_v5_0_0_7_4_1_EndsTheSession(t *testing.T) {
	d := newTestDashboard(t)
	cookie := d.setUp(t)

	d.post("/api/logout", map[string]string{}, cookie)

	if response := d.get("/api/events", cookie); response.Code != http.StatusUnauthorized {
		t.Errorf("events after logout = %d", response.Code)
	}
}

func TestLogout_v5_0_0_14_3_1_ClearsSiteDataButKeepsCookies(t *testing.T) {
	d := newTestDashboard(t)
	cookie := d.setUp(t)

	response := d.post("/api/logout", map[string]string{}, cookie)

	clearSiteData := response.Header().Get("Clear-Site-Data")
	if clearSiteData != `"cache", "storage"` {
		t.Errorf("Clear-Site-Data = %q", clearSiteData)
	}
}

func TestRefusedRequests_v5_0_0_16_3_2_MissingSessionIsJournaled(t *testing.T) {
	d := newTestDashboard(t)

	d.get("/api/events")

	if !slices.Contains(*d.refusals, "no_session") {
		t.Errorf("refusals = %v, want no_session", *d.refusals)
	}
}

func TestRefusedRequests_v5_0_0_16_3_3_BypassAttemptsAreJournaled(t *testing.T) {
	d := newTestDashboard(t)

	crossSite := httptest.NewRequest(http.MethodPost, "https://dash.example.com/api/login", strings.NewReader(`{}`))
	crossSite.Header.Set("Content-Type", "application/json")
	crossSite.Header.Set("Origin", "https://evil.example")
	d.handler.ServeHTTP(httptest.NewRecorder(), crossSite)

	formPost := httptest.NewRequest(http.MethodPost, "https://dash.example.com/api/login", strings.NewReader("password=x"))
	formPost.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	d.handler.ServeHTTP(httptest.NewRecorder(), formPost)

	d.post("/api/login", map[string]any{"password": "x", "code": "1", "admin": true})

	for _, want := range []string{"cross_site", "not_json", "malformed_body"} {
		if !slices.Contains(*d.refusals, want) {
			t.Errorf("refusals = %v, want %s", *d.refusals, want)
		}
	}
}
