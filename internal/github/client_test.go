package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return &Client{http: server.Client(), apiBase: server.URL}
}

func TestConvertManifestRefusesAMalformedCode(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("GitHub was called with %s", r.URL.Path)
	})

	for _, code := range []string{"", "../app/installations", "a/b", "code?x=1"} {
		if _, err := client.ConvertManifest(context.Background(), code); err == nil {
			t.Errorf("code %q was accepted", code)
		}
	}
}

// github-connection: No long-lived repository credentials
func TestInstallationTokenIsScopedToOneRepositoryAndPermission(t *testing.T) {
	var gotBody map[string]any
	var gotAuthorization string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/app/installations/77/access_tokens" {
			t.Errorf("called %s %s", r.Method, r.URL.Path)
		}
		gotAuthorization = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotBody)
		fmt.Fprint(w, `{"token":"ghs_test","expires_at":"2026-10-28T13:00:00Z"}`)
	})

	token, err := client.CreateInstallationToken(context.Background(), "app-jwt", 77, []int64{12345}, ContentsRead)
	if err != nil {
		t.Fatal(err)
	}

	if token.Token != "ghs_test" || gotAuthorization != "Bearer app-jwt" {
		t.Errorf("token %q with authorization %q", token.Token, gotAuthorization)
	}
	permissions, _ := json.Marshal(gotBody["permissions"])
	repositories, _ := json.Marshal(gotBody["repository_ids"])
	if string(permissions) != `{"contents":"read"}` || string(repositories) != `[12345]` {
		t.Errorf("asked for %s on %s", permissions, repositories)
	}
}

func TestNotFoundIsReported(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	if _, err := client.Installations(context.Background(), "jwt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestRecentDeliveriesFollowsPagesBackToSince(t *testing.T) {
	since := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cursor") == "" {
			w.Header().Set("Link", fmt.Sprintf(`<%s/app/hook/deliveries?per_page=100&cursor=b>; rel="next"`, server.URL))
			fmt.Fprint(w, `[{"id":3,"status_code":502,"delivered_at":"2026-10-28T12:00:00Z"},{"id":2,"status_code":200,"delivered_at":"2026-10-28T11:00:00Z"}]`)
			return
		}
		fmt.Fprint(w, `[{"id":1,"status_code":0,"delivered_at":"2026-10-28T10:30:00Z"},{"id":0,"status_code":500,"delivered_at":"2026-10-28T09:00:00Z"}]`)
	}))
	t.Cleanup(server.Close)
	client := &Client{http: server.Client(), apiBase: server.URL}

	deliveries, err := client.RecentDeliveries(context.Background(), "jwt", since)
	if err != nil {
		t.Fatal(err)
	}

	var failed []int64
	for _, delivery := range deliveries {
		if delivery.IsFailed() {
			failed = append(failed, delivery.ID)
		}
	}
	if len(deliveries) != 3 || fmt.Sprint(failed) != "[3 1]" {
		t.Errorf("deliveries = %+v, failed %v", deliveries, failed)
	}
}

func TestNextPageStaysOnTheAPIHost(t *testing.T) {
	client := &Client{apiBase: "https://api.github.com"}

	if got := client.nextPagePath(`<https://evil.example/app/hook/deliveries>; rel="next"`); got != "" {
		t.Errorf("followed a link to another host: %q", got)
	}
	if got := client.nextPagePath(`<https://api.github.com/app/hook/deliveries?cursor=x>; rel="next"`); got != "/app/hook/deliveries?cursor=x" {
		t.Errorf("next = %q", got)
	}
}
