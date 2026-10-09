package dashboard

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leanhanc/tero/internal/auth"
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
}

func newTestDashboard(t *testing.T) testDashboard {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	now := time.Unix(1_800_000_000, 0)
	service := auth.New(db, auth.Options{
		Domain:    "dash.example.com",
		Passwords: password.Policy{Breaches: noBreaches{}},
		Now:       func() time.Time { return now },
	})

	return testDashboard{handler: NewHandler(service, "dash.example.com"), service: service, now: &now}
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
	link, err := d.service.IssueSetupLink(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	_, token, _ := strings.Cut(link, "#")

	begin := d.post("/api/setup/begin", map[string]string{"token": token, "password": adminPassword})
	if begin.Code != http.StatusOK {
		t.Fatalf("setup/begin = %d %s", begin.Code, begin.Body)
	}
	var enrolment auth.Enrolment
	json.Unmarshal(begin.Body.Bytes(), &enrolment)

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
	link, _ := d.service.IssueSetupLink(context.Background(), "")
	_, token, _ := strings.Cut(link, "#")
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
