//go:build e2e

package e2e

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/leanhanc/tero/internal/webauthntest"
)

// testAccountSecurity runs after setup, with the admin logged in as "admin".
func (d *dashboard) testAccountSecurity(t *testing.T) {
	t.Run("process-isolation: Session planted by a tero process / Dashboard can't reach the sessions", testNoPlantedSession)
	t.Run("account-security: List / End one session (v5.0.0-7.5.2)", d.testEndOneSession)
	t.Run("account-security: Password changed / Other sessions ended / Service name in the password (v5.0.0-6.2.3, v5.0.0-6.2.11)", d.testChangePassword)
	t.Run("account-security: Add a passkey / dashboard-access: Log in with a passkey (v5.0.0-6.3.3)", d.testPasskeys)
	t.Run("dashboard-access: Admin loses their authenticator (passkeys removed)", d.testResetRemovesPasskeys)
}

// testNoPlantedSession: sessions live only in the key service, so a token a
// process running as tero makes up is refused, and tero can't open the key
// service's database to add one.
func testNoPlantedSession(t *testing.T) {
	planted := randomLowercase(43)
	response := requestDashboard(t, "planted", "curl -H 'Cookie: __Host-tero_session="+planted+"' ", "/api/events")
	if response.status != 401 {
		t.Errorf("made-up session = %d", response.status)
	}

	if got := strings.TrimSpace(run(t, "sudo grep -a -c 'CREATE TABLE sessions' /var/lib/tero/tero.db").output); got != "0" {
		t.Errorf("tero's database has a sessions table (%s matches)", got)
	}
	for _, command := range []string{asTero("cat /var/lib/tero-keys/keys.db"), inDashboardCgroup("cat /var/lib/tero-keys/keys.db")} {
		if got := run(t, command+" >/dev/null"); got.exitCode == 0 {
			t.Error("tero can read the key service's database")
		}
	}
}

type accountState struct {
	IsConfirmed bool `json:"isConfirmed"`
	Sessions    []struct {
		ID        string `json:"id"`
		Method    string `json:"method"`
		IsCurrent bool   `json:"isCurrent"`
	} `json:"sessions"`
	Passkeys []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"passkeys"`
}

func account(t *testing.T, browser string) accountState {
	t.Helper()
	response := getDashboard(t, browser, "/api/account")
	if response.status != 200 {
		t.Fatalf("account for %s = %d %s", browser, response.status, response.body)
	}
	var state accountState
	if err := json.Unmarshal([]byte(response.body), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func (d *dashboard) login(t *testing.T, browser string) {
	t.Helper()
	response := postDashboard(t, browser, "/api/login", map[string]string{"password": adminPassword, "code": d.nextCode(t)})
	if response.status != 204 {
		t.Fatalf("login as %s = %d %s", browser, response.status, response.body)
	}
}

func (d *dashboard) confirm(t *testing.T, browser string) {
	t.Helper()
	response := postDashboard(t, browser, "/api/account/confirm", map[string]string{"password": adminPassword, "code": d.nextCode(t)})
	if response.status != 204 {
		t.Fatalf("confirm as %s = %d %s", browser, response.status, response.body)
	}
}

func (d *dashboard) testEndOneSession(t *testing.T) {
	d.login(t, "other-browser")

	var otherID string
	for _, session := range account(t, "admin").Sessions {
		if !session.IsCurrent {
			otherID = session.ID
		}
	}
	if otherID == "" {
		t.Fatal("the other browser's session isn't listed")
	}

	// The admin's session is confirmed by the setup that started it, or by
	// this; the unit tests cover the refusal without it.
	d.confirm(t, "admin")
	if response := postDashboard(t, "admin", "/api/sessions/end", map[string]any{"id": otherID}); response.status != 204 {
		t.Fatalf("ending a session = %d %s", response.status, response.body)
	}
	if response := getDashboard(t, "other-browser", "/api/events"); response.status != 401 {
		t.Errorf("ended session = %d", response.status)
	}
}

func (d *dashboard) testChangePassword(t *testing.T) {
	d.login(t, "third-browser")
	newPassword := randomLowercase(64)

	withContextWord := postDashboard(t, "admin", "/api/account/password", map[string]any{
		"currentPassword": adminPassword, "code": d.nextCode(t), "newPassword": "my tero " + newPassword, "isEndingOthers": true,
	})
	if withContextWord.status != 422 {
		t.Errorf("new password with \"tero\" = %d", withContextWord.status)
	}

	response := postDashboard(t, "admin", "/api/account/password", map[string]any{
		"currentPassword": adminPassword, "code": d.nextCode(t), "newPassword": newPassword, "isEndingOthers": true,
	})
	if response.status != 204 {
		t.Fatalf("password change = %d %s", response.status, response.body)
	}
	oldPassword := adminPassword
	adminPassword = newPassword

	if response := getDashboard(t, "third-browser", "/api/events"); response.status != 401 {
		t.Errorf("other session after the change = %d", response.status)
	}
	if response := getDashboard(t, "admin", "/api/events"); response.status != 200 {
		t.Errorf("current session after the change = %d", response.status)
	}
	oldLogin := postDashboard(t, "anonymous", "/api/login", map[string]string{"password": oldPassword, "code": d.nextCode(t)})
	if oldLogin.status != 401 {
		t.Errorf("old password after the change = %d", oldLogin.status)
	}
	d.login(t, "third-browser")
	assertContains(t, getDashboard(t, "admin", "/api/events").body, "password_changed", "sessions_ended")
}

// newAuthenticator is a software passkey authenticator in place of the
// admin's phone or laptop: the suite drives the API without a browser.
func newAuthenticator() *webauthntest.Authenticator {
	return &webauthntest.Authenticator{Origin: "https://" + skippedDomain, Counter: 1}
}

var authenticator = newAuthenticator()

func (d *dashboard) testPasskeys(t *testing.T) {
	d.confirm(t, "admin")

	begin := postDashboard(t, "admin", "/api/passkeys/register/begin", map[string]any{})
	var options struct {
		ChallengeID string          `json:"challengeId"`
		Options     json.RawMessage `json:"options"`
	}
	if err := json.Unmarshal([]byte(begin.body), &options); err != nil {
		t.Fatalf("register/begin = %d %s", begin.status, begin.body)
	}
	answer, err := authenticator.Register(options.Options)
	if err != nil {
		t.Fatal(err)
	}
	finish := postDashboard(t, "admin", "/api/passkeys/register/finish", map[string]any{
		"challengeId": options.ChallengeID, "name": "Laptop", "credential": answer,
	})
	if finish.status != 200 {
		t.Fatalf("register/finish = %d %s", finish.status, finish.body)
	}

	if response := loginWithPasskey(t, "passkey-browser"); response.status != 204 {
		t.Fatalf("passkey login = %d %s", response.status, response.body)
	}
	// A passkey login doesn't confirm the session; a password login does.
	if response := postDashboard(t, "passkey-browser", "/api/passkeys/register/begin", map[string]any{}); response.status != 403 {
		t.Errorf("adding a passkey from an unconfirmed session = %d", response.status)
	}
	for _, session := range account(t, "passkey-browser").Sessions {
		if session.IsCurrent && session.Method != "passkey" {
			t.Errorf("passkey session method = %q", session.Method)
		}
	}
	if passkeys := account(t, "admin").Passkeys; len(passkeys) != 1 || passkeys[0].Name != "Laptop" {
		t.Errorf("passkeys = %+v", passkeys)
	}
}

func loginWithPasskey(t *testing.T, browser string) dashboardResponse {
	t.Helper()
	begin := postDashboard(t, browser, "/api/login/passkey/begin", map[string]any{})
	var options struct {
		ChallengeID string          `json:"challengeId"`
		Options     json.RawMessage `json:"options"`
	}
	if err := json.Unmarshal([]byte(begin.body), &options); err != nil {
		t.Fatalf("login/passkey/begin = %d %s", begin.status, begin.body)
	}
	answer, err := authenticator.Login(options.Options)
	if err != nil {
		t.Fatal(err)
	}

	return postDashboard(t, browser, "/api/login/passkey/finish", map[string]any{"challengeId": options.ChallengeID, "credential": answer})
}

// testResetRemovesPasskeys resets the login, checks the passkey no longer
// logs in, and sets the login up again for the tests that follow.
func (d *dashboard) testResetRemovesPasskeys(t *testing.T) {
	got := run(t, "sudo tero reset-login")
	if got.exitCode != 0 {
		t.Fatalf("reset-login failed (%d):\n%s", got.exitCode, got.output)
	}
	d.token = tokenOf(t, lastLine(got.output))
	d.lastStep = 0

	if response := getDashboard(t, "passkey-browser", "/api/events"); response.status != 401 {
		t.Errorf("passkey session after reset = %d", response.status)
	}
	d.testRecoveryVisible(t)
	if response := loginWithPasskey(t, "passkey-browser"); response.status != 401 {
		t.Errorf("old passkey after reset = %d %s", response.status, response.body)
	}
}
