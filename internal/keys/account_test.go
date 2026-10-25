package keys

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/leanhanc/tero/internal/totp"
)

const newPassword = "another long passphrase for tests"

// code returns the TOTP code for step steps after now, so tests can use a
// fresh code after another request used the current one.
func (env testEnv) code(secret string, steps int) string {
	code, _ := totp.Code(secret, env.clock.Now().Add(time.Duration(steps)*30*time.Second))
	return code
}

// secondSession logs in from another browser and returns its session token.
func (env testEnv) secondSession(t *testing.T, secret string) string {
	t.Helper()
	env.clock.Advance(time.Minute)
	result, err := env.loginFrom(Browser{IP: "192.0.2.80", UserAgent: "Other browser"}, secret, adminPassword)
	if err != nil {
		t.Fatal(err)
	}
	return result.SessionToken
}

func (env testEnv) confirm(t *testing.T, session, secret string) {
	t.Helper()
	env.clock.Advance(time.Minute)
	if err := env.dashboard.Confirm(context.Background(), session, Browser{IP: adminIP}, adminPassword, env.code(secret, 0)); err != nil {
		t.Fatal(err)
	}
}

// account-security: Password changed, Other sessions ended
func TestChangePassword_v5_0_0_6_2_3_EndsOtherSessions(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	other := env.secondSession(t, secret)
	ctx := context.Background()
	env.clock.Advance(time.Minute)

	err := env.dashboard.ChangePassword(ctx, *env.session, Browser{IP: adminIP}, adminPassword, env.code(secret, 0), newPassword, true)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := env.dashboard.CheckSession(ctx, other, adminIP); !errors.Is(err, ErrSessionEnded) {
		t.Errorf("other session after the change = %v", err)
	}
	if _, err := env.dashboard.CheckSession(ctx, *env.session, adminIP); err != nil {
		t.Errorf("current session after the change = %v", err)
	}
	env.clock.Advance(time.Minute)
	if err := env.login(secret, adminPassword); !errors.Is(err, ErrWrongLogin) {
		t.Errorf("old password after the change = %v", err)
	}
	env.clock.Advance(time.Minute)
	if err := env.login(secret, newPassword); err != nil {
		t.Errorf("new password with the same authenticator = %v", err)
	}

	types := env.eventTypes(t)
	if countOf(types, EventPasswordChanged) != 1 || countOf(types, EventSessionsEnded) != 1 {
		t.Errorf("events = %v", types)
	}
}

// account-security: Other sessions kept
func TestChangePasswordCanKeepOtherSessions(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	other := env.secondSession(t, secret)
	ctx := context.Background()
	env.clock.Advance(time.Minute)

	err := env.dashboard.ChangePassword(ctx, *env.session, Browser{IP: adminIP}, adminPassword, env.code(secret, 0), newPassword, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.dashboard.CheckSession(ctx, other, adminIP); err != nil {
		t.Errorf("other session = %v", err)
	}
}

// account-security: Wrong current password or code, Code reused
func TestChangePasswordNeedsTheCurrentFactors(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	ctx := context.Background()
	browser := Browser{IP: adminIP}

	// setUp used the current code.
	if err := env.dashboard.ChangePassword(ctx, *env.session, browser, adminPassword, env.code(secret, 0), newPassword, true); !errors.Is(err, ErrWrongLogin) {
		t.Errorf("change with a used code = %v", err)
	}
	env.clock.Advance(time.Minute)
	if err := env.dashboard.ChangePassword(ctx, *env.session, browser, "not the password", env.code(secret, 0), newPassword, true); !errors.Is(err, ErrWrongLogin) {
		t.Errorf("change with a wrong password = %v", err)
	}
	if err := env.dashboard.ChangePassword(ctx, "", browser, adminPassword, env.code(secret, 0), newPassword, true); !errors.Is(err, ErrSessionEnded) {
		t.Errorf("change without a session = %v", err)
	}

	env.clock.Advance(time.Minute)
	if err := env.login(secret, adminPassword); err != nil {
		t.Errorf("the password changed after failed attempts: %v", err)
	}
	if throttled := env.count("login_throttles"); throttled != 0 {
		t.Errorf("the successful login left %d throttle rows", throttled)
	}
}

// account-security: Not confirmed, key-service: Unconfirmed session ends another
func TestEndingSessionsNeedsConfirmation(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	other := env.secondSession(t, secret)
	ctx := context.Background()

	if _, err := env.dashboard.RevokeSession(ctx, *env.session, adminIP, "", true); !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("revoke without confirming = %v", err)
	}
	if _, err := env.dashboard.CheckSession(ctx, other, adminIP); err != nil {
		t.Errorf("other session after a refused revoke = %v", err)
	}
}

// account-security: End one session, List, No session tokens exposed
func TestListAndEndOneSession_v5_0_0_7_5_2(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	other := env.secondSession(t, secret)
	env.confirm(t, *env.session, secret)
	ctx := context.Background()

	sessions, err := env.dashboard.ListSessions(ctx, *env.session)
	if err != nil || len(sessions) != 2 {
		t.Fatalf("sessions = %+v, %v", sessions, err)
	}
	currentAt := slices.IndexFunc(sessions, func(info SessionInfo) bool { return info.IsCurrent })
	otherAt := slices.IndexFunc(sessions, func(info SessionInfo) bool { return !info.IsCurrent })
	if currentAt < 0 || otherAt < 0 {
		t.Fatalf("no current or other session in %+v", sessions)
	}
	otherInfo := sessions[otherAt]
	if otherInfo.Method != MethodPassword || otherInfo.LoginIP != "192.0.2.80" || otherInfo.UserAgent != "Other browser" {
		t.Errorf("other session = %+v", otherInfo)
	}
	for _, info := range sessions {
		if info.ID == other || info.ID == hashToken(other) || len(info.ID) != 32 {
			t.Errorf("listed id %q is not a separate id", info.ID)
		}
	}

	ended, err := env.dashboard.RevokeSession(ctx, *env.session, adminIP, otherInfo.ID, false)
	if err != nil || ended != 1 {
		t.Fatalf("revoke = %d, %v", ended, err)
	}
	if _, err := env.dashboard.CheckSession(ctx, other, adminIP); !errors.Is(err, ErrSessionEnded) {
		t.Errorf("ended session = %v", err)
	}
}

// account-security: End all other sessions
func TestEndAllOtherSessions_v5_0_0_7_4_5(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	first := env.secondSession(t, secret)
	second := env.secondSession(t, secret)
	env.confirm(t, *env.session, secret)
	ctx := context.Background()

	if ended, err := env.dashboard.RevokeSession(ctx, *env.session, adminIP, "", true); err != nil || ended != 2 {
		t.Fatalf("revoke all others = %d, %v", ended, err)
	}
	for _, token := range []string{first, second} {
		if _, err := env.dashboard.CheckSession(ctx, token, adminIP); !errors.Is(err, ErrSessionEnded) {
			t.Errorf("other session = %v", err)
		}
	}
	if _, err := env.dashboard.CheckSession(ctx, *env.session, adminIP); err != nil {
		t.Errorf("current session = %v", err)
	}
}

// account-security: Confirmation expires, Confirmation belongs to one session
func TestConfirmation_v5_0_0_7_5_1_ExpiresAndStaysOnItsSession(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	other := env.secondSession(t, secret)
	env.confirm(t, *env.session, secret)
	ctx := context.Background()

	state, _ := env.dashboard.CheckSession(ctx, *env.session, adminIP)
	otherState, _ := env.dashboard.CheckSession(ctx, other, adminIP)
	if !state.IsConfirmed || otherState.IsConfirmed {
		t.Errorf("confirmed: current %v, other %v", state.IsConfirmed, otherState.IsConfirmed)
	}

	env.clock.Advance(confirmationLifetime)
	if _, err := env.dashboard.RevokeSession(ctx, *env.session, adminIP, "", true); !errors.Is(err, ErrNotConfirmed) {
		t.Errorf("revoke after the confirmation expired = %v", err)
	}
}

// account-security: Guessing through confirmation
func TestConfirmationIsThrottledLikeLogin(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()

	var err error
	for range ipThreshold + 1 {
		err = env.dashboard.Confirm(ctx, *env.session, Browser{IP: "203.0.113.30"}, wrongPassword, "000000")
	}
	var limited RateLimitedError
	if !errors.As(err, &limited) {
		t.Errorf("confirmation after %d failures = %v, want rate limited", ipThreshold, err)
	}
}

// Wrong codes on confirmation count toward the code guessing limit, and the
// dashboard sees them as failed confirmations.
func TestConfirmationCodesCountTowardTheLock(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	ctx := context.Background()

	for range codeFailureLimit {
		env.clock.Advance(20 * time.Minute)
		env.service.checkCredentials(ctx, adminIP, adminPassword, "000000", confirmCheck)
	}

	env.clock.Advance(time.Minute)
	if err := env.check(adminIP, adminPassword, env.code(secret, 0)); !errors.Is(err, ErrLocked) {
		t.Errorf("check after %d wrong confirmation codes = %v", codeFailureLimit, err)
	}
	if count := countOf(env.eventTypes(t), EventConfirmCodeFailed); count != codeFailureLimit {
		t.Errorf("%d confirm_totp_failed events", count)
	}

	env.clock.Advance(codeLockout)
	*env.session = env.secondSession(t, secret)
	events, _ := env.dashboard.RecentEvents(ctx, *env.session, 100)
	for _, event := range events {
		if event.Type == EventConfirmCodeFailed || event.Type == EventCodeFailed {
			t.Fatalf("the dashboard sees %s", event.Type)
		}
	}
}

// key-service: Dashboard asks without a session
func TestAccountVerbsNeedASession(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()

	if _, err := env.dashboard.ListSessions(ctx, ""); !errors.Is(err, ErrSessionEnded) {
		t.Errorf("session.list = %v", err)
	}
	if _, err := env.dashboard.RecentEvents(ctx, "made-up", 10); !errors.Is(err, ErrSessionEnded) {
		t.Errorf("events.list = %v", err)
	}
	if _, err := env.dashboard.BeginPasskeyRegistration(ctx, ""); !errors.Is(err, ErrSessionEnded) {
		t.Errorf("passkey.register.begin = %v", err)
	}
	if _, err := env.dashboard.ListPasskeys(ctx, ""); !errors.Is(err, ErrSessionEnded) {
		t.Errorf("passkey.list = %v", err)
	}
	if _, err := env.root.RecentEvents(ctx, "", 10); err != nil {
		t.Errorf("root events.list = %v", err)
	}
}

// Review finding: confirming with the code the admin just logged in with
// counted as a guessed code toward the 24-hour lock.
func TestReusedCodeIsNotAGuess(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	ctx := context.Background()

	// setUp used the current code.
	err := env.dashboard.Confirm(ctx, *env.session, Browser{IP: adminIP}, adminPassword, env.code(secret, 0))
	if !errors.Is(err, ErrWrongLogin) {
		t.Fatalf("confirm with a used code = %v", err)
	}
	if failures := env.count("code_failures"); failures != 0 {
		t.Errorf("a used code was counted as %d guessed codes", failures)
	}
	if types := env.eventTypes(t); countOf(types, EventConfirmCodeFailed) != 0 {
		t.Errorf("a used code was recorded as a wrong code: %v", types)
	}
}

func TestEndingAnUnknownSessionSaysSo(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	env.confirm(t, *env.session, secret)

	if _, err := env.dashboard.RevokeSession(context.Background(), *env.session, adminIP, "made-up", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("ending an unknown session = %v", err)
	}
}

// Review finding: made-up IP strings from the dashboard each added a
// throttle row.
func TestMadeUpIPsShareOneThrottle(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)

	for index := range 20 {
		env.failFrom(Browser{IP: strings.Repeat("x", index+1)})
	}
	if rows := env.count("login_throttles"); rows > 2 {
		t.Errorf("%d throttle rows for made-up IPs, want the shared one and the account", rows)
	}
}
