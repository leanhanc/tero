package dashboard

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/leanhanc/tero/internal/auth"
	"github.com/leanhanc/tero/internal/totp"
	"github.com/leanhanc/tero/internal/webauthntest"
)

func (d testDashboard) nextCode() string {
	*d.now = d.now.Add(30 * time.Second)
	code, _ := totp.Code(*d.secret, *d.now)
	return code
}

func (d testDashboard) account(t *testing.T, cookie *http.Cookie) accountResponse {
	t.Helper()
	response := d.get("/api/account", cookie)
	if response.Code != http.StatusOK {
		t.Fatalf("account = %d %s", response.Code, response.Body)
	}
	var account accountResponse
	json.Unmarshal(response.Body.Bytes(), &account)
	return account
}

func (d testDashboard) confirm(t *testing.T, cookie *http.Cookie) {
	t.Helper()
	response := d.post("/api/account/confirm", map[string]string{"password": adminPassword, "code": d.nextCode()}, cookie)
	if response.Code != http.StatusNoContent {
		t.Fatalf("confirm = %d %s", response.Code, response.Body)
	}
}

// account-security: No session
func TestAccountCallsNeedASession(t *testing.T) {
	d := newTestDashboard(t)
	d.setUp(t)

	for path, body := range map[string]any{
		"/api/account/confirm":          map[string]string{"password": adminPassword, "code": "123456"},
		"/api/account/password":         map[string]any{"currentPassword": adminPassword, "code": "123456", "newPassword": "x"},
		"/api/sessions/end":             map[string]any{"isAllOthers": true},
		"/api/passkeys/register/begin":  map[string]any{},
		"/api/passkeys/register/finish": map[string]any{"challengeId": "x", "name": "x", "credential": map[string]any{}},
		"/api/passkeys/rename":          map[string]string{"id": "x", "name": "x"},
		"/api/passkeys/remove":          map[string]any{"id": "x"},
	} {
		if response := d.post(path, body); response.Code != http.StatusUnauthorized {
			t.Errorf("%s without a session = %d", path, response.Code)
		}
	}
	if response := d.get("/api/account"); response.Code != http.StatusUnauthorized {
		t.Errorf("account without a session = %d", response.Code)
	}
}

// account-security: Open settings, End one session
func TestSessionListAndEnd(t *testing.T) {
	d := newTestDashboard(t)
	first := d.setUp(t)
	second := d.login(t)

	account := d.account(t, first)
	if len(account.Sessions) != 2 || account.IsConfirmed {
		t.Fatalf("account = %+v", account)
	}

	var otherID string
	for _, session := range account.Sessions {
		if !session.IsCurrent {
			otherID = session.ID
		}
	}
	if response := d.post("/api/sessions/end", map[string]string{"id": otherID}, first); response.Code != http.StatusForbidden {
		t.Errorf("end a session without confirming = %d", response.Code)
	}

	d.confirm(t, first)
	if !d.account(t, first).IsConfirmed {
		t.Error("the session isn't confirmed after confirming")
	}
	if response := d.post("/api/sessions/end", map[string]string{"id": otherID}, first); response.Code != http.StatusNoContent {
		t.Fatalf("end a session = %d %s", response.Code, response.Body)
	}
	if response := d.get("/api/events", second); response.Code != http.StatusUnauthorized {
		t.Errorf("ended session = %d", response.Code)
	}
}

// account-security: Domain label in the password, on change
func TestPasswordChangeRefusesContextWords(t *testing.T) {
	d := newTestDashboard(t)
	cookie := d.setUp(t)

	response := d.post("/api/account/password", map[string]any{
		"currentPassword": adminPassword, "code": d.nextCode(), "newPassword": "my dash.example.com passphrase", "isEndingOthers": true,
	}, cookie)
	if response.Code != http.StatusUnprocessableEntity {
		t.Errorf("context word = %d %s", response.Code, response.Body)
	}

	response = d.post("/api/account/password", map[string]any{
		"currentPassword": adminPassword, "code": d.nextCode(), "newPassword": "a brand new passphrase", "isEndingOthers": true,
	}, cookie)
	if response.Code != http.StatusNoContent {
		t.Errorf("password change = %d %s", response.Code, response.Body)
	}
}

// dashboard-access: Log in with a passkey
func TestPasskeyRegistrationAndLoginOverHTTP(t *testing.T) {
	d := newTestDashboard(t)
	cookie := d.setUp(t)
	d.confirm(t, cookie)
	authenticator := &webauthntest.Authenticator{Origin: "https://dash.example.com", Counter: 1}

	var options auth.PasskeyOptions
	begin := d.post("/api/passkeys/register/begin", map[string]any{}, cookie)
	json.Unmarshal(begin.Body.Bytes(), &options)
	answer, err := authenticator.Register(options.Options)
	if err != nil {
		t.Fatal(err)
	}
	finish := d.post("/api/passkeys/register/finish", map[string]any{
		"challengeId": options.ChallengeID, "name": "Laptop", "credential": answer,
	}, cookie)
	if finish.Code != http.StatusOK {
		t.Fatalf("register = %d %s", finish.Code, finish.Body)
	}

	loginBegin := d.post("/api/login/passkey/begin", map[string]any{})
	json.Unmarshal(loginBegin.Body.Bytes(), &options)
	answer, _ = authenticator.Login(options.Options)
	login := d.post("/api/login/passkey/finish", map[string]any{"challengeId": options.ChallengeID, "credential": answer})
	if login.Code != http.StatusNoContent {
		t.Fatalf("passkey login = %d %s", login.Code, login.Body)
	}

	replay := d.post("/api/login/passkey/finish", map[string]any{"challengeId": options.ChallengeID, "credential": answer})
	if replay.Code != http.StatusUnauthorized {
		t.Errorf("replayed passkey login = %d", replay.Code)
	}
}
