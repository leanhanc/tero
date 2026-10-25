package keys

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/leanhanc/tero/internal/webauthntest"
)

func newAuthenticator() *webauthntest.Authenticator {
	return &webauthntest.Authenticator{Origin: "https://" + testDomain, Counter: 1}
}

// addPasskey registers a passkey from authenticator on the env's confirmed
// session.
func (env testEnv) addPasskey(t *testing.T, authenticator *webauthntest.Authenticator, name string) PasskeyInfo {
	t.Helper()
	ctx := context.Background()

	options, err := env.dashboard.BeginPasskeyRegistration(ctx, *env.session)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := authenticator.Register(options.Options)
	if err != nil {
		t.Fatal(err)
	}
	info, err := env.dashboard.FinishPasskeyRegistration(ctx, *env.session, adminIP, options.ChallengeID, name, answer, false)
	if err != nil {
		t.Fatal(err)
	}

	return info
}

func (env testEnv) loginWithPasskey(authenticator *webauthntest.Authenticator, ip string) (LoginResult, error) {
	ctx := context.Background()
	options, err := env.dashboard.BeginPasskeyLogin(ctx, ip)
	if err != nil {
		return LoginResult{}, err
	}
	answer, err := authenticator.Login(options.Options)
	if err != nil {
		return LoginResult{}, err
	}

	return env.dashboard.LoginPasskey(ctx, Browser{IP: ip, UserAgent: "Passkey browser"}, options.ChallengeID, answer)
}

// account-security: Add a passkey, dashboard-access: Log in with a passkey
func TestPasskeyAddAndLogin_v5_0_0_6_3_3(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	env.confirm(t, *env.session, secret)
	authenticator := newAuthenticator()
	ctx := context.Background()

	added := env.addPasskey(t, authenticator, "  Laptop ")
	if added.Name != "Laptop" || !added.CreatedAt.Equal(env.clock.Now()) {
		t.Errorf("added = %+v", added)
	}

	env.clock.Advance(time.Minute)
	result, err := env.loginWithPasskey(authenticator, adminIP)
	if err != nil {
		t.Fatalf("passkey login = %v", err)
	}
	sessions, _ := env.dashboard.ListSessions(ctx, result.SessionToken)
	for _, session := range sessions {
		if session.IsCurrent && session.Method != MethodPasskey {
			t.Errorf("passkey session method = %q", session.Method)
		}
	}

	passkeys, _ := env.dashboard.ListPasskeys(ctx, result.SessionToken)
	if len(passkeys) != 1 || !passkeys[0].LastUsedAt.Equal(env.clock.Now()) {
		t.Errorf("passkeys after a login = %+v", passkeys)
	}
	types := env.eventTypes(t)
	if countOf(types, EventPasskeyAdded) != 1 || countOf(types, EventPasskeyLoginSucceeded) != 1 {
		t.Errorf("events = %v", types)
	}
}

// account-security: Add without confirming
func TestPasskeyRegistrationNeedsConfirmation(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)

	if _, err := env.dashboard.BeginPasskeyRegistration(context.Background(), *env.session); !errors.Is(err, ErrNotConfirmed) {
		t.Errorf("registration without confirming = %v", err)
	}
}

// account-security: Rename and remove
func TestPasskeyRenameAndRemove(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	env.confirm(t, *env.session, secret)
	authenticator := newAuthenticator()
	added := env.addPasskey(t, authenticator, "Laptop")
	ctx := context.Background()

	if err := env.dashboard.RenamePasskey(ctx, *env.session, added.ID, "Work laptop"); err != nil {
		t.Fatal(err)
	}
	passkeys, _ := env.dashboard.ListPasskeys(ctx, *env.session)
	if len(passkeys) != 1 || passkeys[0].Name != "Work laptop" {
		t.Errorf("after rename = %+v", passkeys)
	}
	if err := env.dashboard.RenamePasskey(ctx, *env.session, added.ID, ""); !errors.Is(err, ErrInvalidName) {
		t.Errorf("empty name = %v", err)
	}

	if err := env.dashboard.RemovePasskey(ctx, *env.session, adminIP, added.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := env.loginWithPasskey(authenticator, adminIP); !errors.Is(err, ErrWrongLogin) {
		t.Errorf("login with a removed passkey = %v", err)
	}

	// account-security: Last passkey removed
	env.clock.Advance(time.Minute)
	if err := env.login(secret, adminPassword); err != nil {
		t.Errorf("password login after removing the last passkey = %v", err)
	}
}

func TestPasskeyRemovalCanEndOtherSessions_v5_0_0_7_4_3(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	other := env.secondSession(t, secret)
	env.confirm(t, *env.session, secret)
	added := env.addPasskey(t, newAuthenticator(), "Laptop")
	ctx := context.Background()

	if err := env.dashboard.RemovePasskey(ctx, *env.session, adminIP, added.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := env.dashboard.CheckSession(ctx, other, adminIP); !errors.Is(err, ErrSessionEnded) {
		t.Errorf("other session after removing a passkey = %v", err)
	}
}

// account-security: Limit reached
func TestPasskeyLimit(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	env.confirm(t, *env.session, secret)

	for index := range maxPasskeys {
		env.addPasskey(t, newAuthenticator(), fmt.Sprintf("Key %d", index+1))
	}
	if _, err := env.dashboard.BeginPasskeyRegistration(context.Background(), *env.session); !errors.Is(err, ErrPasskeyLimit) {
		t.Errorf("registration past the limit = %v", err)
	}
}

// account-security: Passkey for another site
func TestPasskeyForAnotherSiteIsRefused(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	env.confirm(t, *env.session, secret)
	ctx := context.Background()

	phished := newAuthenticator()
	phished.Origin = "https://dash.example.com.evil.test"
	options, _ := env.dashboard.BeginPasskeyRegistration(ctx, *env.session)
	answer, _ := phished.Register(options.Options)
	if _, err := env.dashboard.FinishPasskeyRegistration(ctx, *env.session, adminIP, options.ChallengeID, "Evil", answer, false); !errors.Is(err, ErrInvalidPasskey) {
		t.Errorf("registration from another origin = %v", err)
	}

	authenticator := newAuthenticator()
	env.addPasskey(t, authenticator, "Laptop")
	authenticator.Origin = "https://evil.test"
	if _, err := env.loginWithPasskey(authenticator, adminIP); !errors.Is(err, ErrWrongLogin) {
		t.Errorf("login from another origin = %v", err)
	}
	authenticator.Origin = "https://" + testDomain
	authenticator.RPID = "evil.test"
	if _, err := env.loginWithPasskey(authenticator, adminIP); !errors.Is(err, ErrWrongLogin) {
		t.Errorf("login for another relying party = %v", err)
	}
}

// key-service: Passkey without user verification
func TestPasskeyNeedsUserVerification(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	env.confirm(t, *env.session, secret)
	ctx := context.Background()

	presenceOnly := newAuthenticator()
	presenceOnly.SkipUserVerification = true
	options, _ := env.dashboard.BeginPasskeyRegistration(ctx, *env.session)
	answer, _ := presenceOnly.Register(options.Options)
	if _, err := env.dashboard.FinishPasskeyRegistration(ctx, *env.session, adminIP, options.ChallengeID, "Key", answer, false); !errors.Is(err, ErrInvalidPasskey) {
		t.Errorf("registration without user verification = %v", err)
	}

	authenticator := newAuthenticator()
	env.addPasskey(t, authenticator, "Laptop")
	authenticator.SkipUserVerification = true
	if _, err := env.loginWithPasskey(authenticator, adminIP); !errors.Is(err, ErrWrongLogin) {
		t.Errorf("login without user verification = %v", err)
	}
}

// dashboard-access: Reused challenge
func TestPasskeyAnswerWorksOnce(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	env.confirm(t, *env.session, secret)
	authenticator := newAuthenticator()
	env.addPasskey(t, authenticator, "Laptop")
	ctx := context.Background()

	options, _ := env.dashboard.BeginPasskeyLogin(ctx, adminIP)
	answer, _ := authenticator.Login(options.Options)
	if _, err := env.dashboard.LoginPasskey(ctx, Browser{IP: adminIP}, options.ChallengeID, answer); err != nil {
		t.Fatal(err)
	}
	if _, err := env.dashboard.LoginPasskey(ctx, Browser{IP: adminIP}, options.ChallengeID, answer); !errors.Is(err, ErrInvalidPasskey) {
		t.Errorf("replayed passkey answer = %v", err)
	}
}

// key-service: Expired challenge
func TestPasskeyChallengeExpires(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	env.confirm(t, *env.session, secret)
	authenticator := newAuthenticator()
	env.addPasskey(t, authenticator, "Laptop")
	ctx := context.Background()

	options, _ := env.dashboard.BeginPasskeyLogin(ctx, adminIP)
	answer, _ := authenticator.Login(options.Options)
	env.clock.Advance(challengeLifetime)
	if _, err := env.dashboard.LoginPasskey(ctx, Browser{IP: adminIP}, options.ChallengeID, answer); !errors.Is(err, ErrInvalidPasskey) {
		t.Errorf("answer after the challenge expired = %v", err)
	}
}

// key-service: Flood of login challenges
func TestLoginChallengesAreCapped(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()

	admins, _ := env.dashboard.BeginPasskeyLogin(ctx, adminIP)
	for range 5000 {
		if _, err := env.dashboard.BeginPasskeyLogin(ctx, "203.0.113.50"); err != nil {
			t.Fatal(err)
		}
	}
	if kept := len(env.service.challenges.byID); kept != maxLoginChallengesPerIP+1 {
		t.Errorf("%d login challenges kept after a flood from one IP, want %d", kept, maxLoginChallengesPerIP+1)
	}
	if _, isUsable := env.service.challenges.take(admins.ChallengeID, challengeLogin, "", env.clock.Now()); !isUsable {
		t.Error("a flood from one IP pushed out the admin's challenge")
	}

	for index := range 5000 {
		env.dashboard.BeginPasskeyLogin(ctx, fmt.Sprintf("2001:db8:%x::1", index))
	}
	if kept := len(env.service.challenges.byID); kept != maxLoginChallenges {
		t.Errorf("%d login challenges kept, want %d", kept, maxLoginChallenges)
	}
}

// Review finding: a challenge pushed out by a flood failed the admin's login
// and charged the admin's IP, so a flood locked the admin out.
func TestMissingChallengeIsNotCharged(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()

	for range ipThreshold + 2 {
		if _, err := env.dashboard.LoginPasskey(ctx, Browser{IP: adminIP}, "gone", []byte(`{}`)); !errors.Is(err, ErrInvalidPasskey) {
			t.Fatalf("answer for a missing challenge = %v", err)
		}
	}
	var failures int
	env.service.db.QueryRow("SELECT coalesce(max(failures), 0) FROM login_throttles").Scan(&failures)
	if failures != 0 {
		t.Errorf("missing challenges were charged %d failures", failures)
	}
}

// Review finding: an authenticator that ignores the exclusion list made the
// unique constraint fail with an internal error.
func TestSamePasskeyTwiceIsRefused(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	env.confirm(t, *env.session, secret)
	ctx := context.Background()

	options, _ := env.dashboard.BeginPasskeyRegistration(ctx, *env.session)
	answer, _ := newAuthenticator().Register(options.Options)
	if _, err := env.dashboard.FinishPasskeyRegistration(ctx, *env.session, adminIP, options.ChallengeID, "One", answer, false); err != nil {
		t.Fatal(err)
	}
	again, _ := env.dashboard.BeginPasskeyRegistration(ctx, *env.session)
	// The same answer, made again for the new challenge, keeps its id.
	var credential map[string]any
	json.Unmarshal(answer, &credential)
	repeated, _ := newAuthenticator().RegisterWithID(again.Options, credential["rawId"].(string))
	if _, err := env.dashboard.FinishPasskeyRegistration(ctx, *env.session, adminIP, again.ChallengeID, "Two", repeated, false); !errors.Is(err, ErrInvalidPasskey) {
		t.Errorf("registering a stored passkey id again = %v", err)
	}
}

// A counter that goes backwards means a copied passkey.
func TestPasskeyCounterGoingBackIsRefused(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	env.confirm(t, *env.session, secret)
	authenticator := newAuthenticator()
	authenticator.Counter = 10
	env.addPasskey(t, authenticator, "Laptop")

	if _, err := env.loginWithPasskey(authenticator, adminIP); err != nil {
		t.Fatal(err)
	}
	authenticator.Counter = 5
	if _, err := env.loginWithPasskey(authenticator, adminIP); !errors.Is(err, ErrWrongLogin) {
		t.Errorf("login with a counter that went back = %v", err)
	}
	if countOf(env.eventTypes(t), EventPasskeyCounterMismatch) != 1 {
		t.Error("no passkey_counter_mismatch event")
	}
}

// dashboard-access: Strangers can't lock the admin out with passkeys
func TestFailedPasskeysOnlyThrottleTheirIP(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	env.confirm(t, *env.session, secret)
	authenticator := newAuthenticator()
	env.addPasskey(t, authenticator, "Laptop")
	stranger := newAuthenticator()
	stranger.Register([]byte(`{"challenge":"AAAA","rp":{"id":"` + testDomain + `"},"user":{"id":"AAAA"}}`))

	var err error
	for range accountThreshold + 5 {
		_, err = env.loginWithPasskey(stranger, "203.0.113.40")
	}
	var limited RateLimitedError
	if !errors.As(err, &limited) {
		t.Errorf("stranger after many failures = %v, want rate limited", err)
	}

	if _, err := env.loginWithPasskey(authenticator, "192.0.2.60"); err != nil {
		t.Errorf("admin's passkey from another IP = %v", err)
	}
	env.clock.Advance(time.Minute)
	if err := env.login(secret, adminPassword); err != nil {
		t.Errorf("admin's password login from another IP = %v", err)
	}
}

// dashboard-access: No passkey yet; reset-login removes passkeys
func TestResetLoginRemovesPasskeys(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	env.confirm(t, *env.session, secret)
	authenticator := newAuthenticator()
	env.addPasskey(t, authenticator, "Laptop")

	if err := env.root.ResetLogin(context.Background()); err != nil {
		t.Fatal(err)
	}
	env.setUp(t)

	if _, err := env.loginWithPasskey(authenticator, adminIP); !errors.Is(err, ErrWrongLogin) {
		t.Errorf("old passkey after a reset = %v", err)
	}
	if passkeys := env.count("passkeys"); passkeys != 0 {
		t.Errorf("%d passkeys after a reset", passkeys)
	}
}
