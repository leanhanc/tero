package keys

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/leanhanc/tero/internal/totp"
)

const wrongPassword = "wrong passphrase for the admin"

// failFrom sends a login with a wrong password from browser.
func (env testEnv) failFrom(browser Browser) error {
	_, err := env.dashboard.LoginPassword(context.Background(), browser, wrongPassword, "000000")
	return err
}

func (env testEnv) count(table string) int {
	var rows int
	env.service.db.QueryRow("SELECT count(*) FROM " + table).Scan(&rows)
	return rows
}

// key-service: Session created only by a login
func TestLoginStartsASessionHere(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	env.clock.Advance(time.Minute)
	ctx := context.Background()

	result, err := env.loginFrom(Browser{IP: adminIP, UserAgent: "Firefox"}, secret, adminPassword)
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionToken == "" || result.DeviceToken == "" {
		t.Fatalf("login result = %+v", result)
	}
	if _, err := env.dashboard.CheckSession(ctx, result.SessionToken, adminIP); err != nil {
		t.Errorf("new session = %v", err)
	}

	var stored int
	env.service.db.QueryRow("SELECT count(*) FROM sessions WHERE token_hash = ?", hashToken(result.SessionToken)).Scan(&stored)
	if stored != 1 {
		t.Error("the session isn't stored by its hash")
	}
}

// key-service: Invented token, process-isolation: Session planted by a tero process
func TestInventedTokenIsRefused(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()

	for _, token := range []string{"", "made-up", hashToken(*env.session)} {
		if _, err := env.dashboard.CheckSession(ctx, token, adminIP); !errors.Is(err, ErrSessionEnded) {
			t.Errorf("CheckSession(%q) = %v", token, err)
		}
	}
}

func TestSessionIdleTimeout(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)

	env.clock.Advance(sessionIdleTimeout)
	if _, err := env.dashboard.CheckSession(context.Background(), *env.session, adminIP); !errors.Is(err, ErrSessionEnded) {
		t.Errorf("session after 30 idle minutes = %v", err)
	}
}

func TestSessionAbsoluteTimeout(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()

	for elapsed := time.Duration(0); elapsed < sessionAbsoluteTimeout-20*time.Minute; elapsed += 20 * time.Minute {
		env.clock.Advance(20 * time.Minute)
		if _, err := env.dashboard.CheckSession(ctx, *env.session, adminIP); err != nil {
			t.Fatalf("active session ended early: %v", err)
		}
	}

	env.clock.Advance(20 * time.Minute)
	if _, err := env.dashboard.CheckSession(ctx, *env.session, adminIP); !errors.Is(err, ErrSessionEnded) {
		t.Errorf("session after 12 hours of activity = %v", err)
	}
}

func TestLogoutEndsTheSession(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()

	if err := env.dashboard.EndSession(ctx, *env.session); err != nil {
		t.Fatal(err)
	}
	if _, err := env.dashboard.CheckSession(ctx, *env.session, adminIP); !errors.Is(err, ErrSessionEnded) {
		t.Errorf("session after logout = %v", err)
	}
}

// The last-used time is written at most once a minute, but always when the IP
// changes.
func TestSessionCheckWritesRarely(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()
	lastSeen := func() (int64, string) {
		var at int64
		var ip string
		env.service.db.QueryRow("SELECT last_seen_at, last_ip FROM sessions").Scan(&at, &ip)
		return at, ip
	}
	created, _ := lastSeen()

	env.clock.Advance(10 * time.Second)
	env.dashboard.CheckSession(ctx, *env.session, adminIP)
	if at, _ := lastSeen(); at != created {
		t.Error("a check within the minute wrote the last-used time")
	}

	env.dashboard.CheckSession(ctx, *env.session, "203.0.113.20")
	if _, ip := lastSeen(); ip != "203.0.113.20" {
		t.Errorf("latest IP = %q", ip)
	}
}

func TestResetLoginEndsSessionsAndForgetsDevices(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	env.clock.Advance(time.Minute)
	result, _ := env.loginFrom(Browser{IP: adminIP}, secret, adminPassword)
	env.failFrom(Browser{IP: "203.0.113.9"})
	ctx := context.Background()

	if err := env.root.ResetLogin(ctx); err != nil {
		t.Fatal(err)
	}

	if _, err := env.dashboard.CheckSession(ctx, result.SessionToken, adminIP); !errors.Is(err, ErrSessionEnded) {
		t.Errorf("session after reset = %v", err)
	}
	if _, isTrusted, _ := env.service.trustedDevice(ctx, result.DeviceToken); isTrusted {
		t.Error("a device stays trusted after reset-login")
	}
	if throttles := env.count("login_throttles"); throttles != 0 {
		t.Errorf("%d throttle rows after reset-login", throttles)
	}
}

func TestLogin_v5_0_0_6_3_1_RepeatedFailuresFromOneIP(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)

	for range ipThreshold {
		env.failFrom(Browser{IP: adminIP})
	}

	env.clock.Advance(5 * time.Second)
	_, err := env.loginFrom(Browser{IP: adminIP}, secret, adminPassword)
	var limited RateLimitedError
	if !errors.As(err, &limited) {
		t.Fatalf("login after %d failures = %v, want rate limited", ipThreshold, err)
	}

	firstDelay := limited.RetryAfter
	env.clock.Advance(firstDelay)
	env.failFrom(Browser{IP: adminIP})
	err = env.failFrom(Browser{IP: adminIP})
	if !errors.As(err, &limited) || limited.RetryAfter <= firstDelay {
		t.Errorf("delay after another failure = %v, want more than %v", limited.RetryAfter, firstDelay)
	}
}

func TestLogin_v5_0_0_6_3_1_DistributedGuessingHitsAccountLimit(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)

	for attempt := range accountThreshold {
		env.failFrom(Browser{IP: fmt.Sprintf("203.0.113.%d", attempt+1)})
	}

	env.clock.Advance(5 * time.Second)
	var limited RateLimitedError
	if _, err := env.loginFrom(Browser{IP: "192.0.2.50"}, secret, adminPassword); !errors.As(err, &limited) {
		t.Errorf("login from a fresh IP after failures from many IPs = %v, want rate limited", err)
	}
}

func TestLogin_v5_0_0_6_3_1_LimitDoesNotLeakValidity(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)

	for range ipThreshold {
		env.failFrom(Browser{IP: adminIP})
	}

	_, withRightCredentials := env.loginFrom(Browser{IP: adminIP}, secret, adminPassword)
	withWrongCredentials := env.failFrom(Browser{IP: adminIP})
	if withRightCredentials.Error() != withWrongCredentials.Error() {
		t.Errorf("rate-limited responses differ: %q vs %q", withRightCredentials, withWrongCredentials)
	}
}

func TestRateLimitTriggerIsRecorded(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)

	for range ipThreshold {
		env.failFrom(Browser{IP: adminIP})
	}

	if countOf(env.eventTypes(t), EventRateLimited) == 0 {
		t.Error("no rate_limited event")
	}
}

// Review finding: parallel logins got past the throttle because the check and
// the count were separate steps.
func TestLogin_v5_0_0_6_3_1_ParallelFailuresCannotPassThrottle(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)

	var wg sync.WaitGroup
	for range 40 {
		wg.Go(func() { env.failFrom(Browser{IP: adminIP}) })
	}
	wg.Wait()

	if checked := countOf(env.eventTypes(t), EventLoginFailed); checked > ipThreshold {
		t.Errorf("%d parallel attempts had their credentials checked, want at most %d", checked, ipThreshold)
	}
}

// Review finding: strangers failing logins locked the real admin out of the
// account for as long as they kept going.
func TestTrustedDeviceIgnoresAccountLockout(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	env.clock.Advance(time.Minute)
	trusted, err := env.loginFrom(Browser{IP: adminIP}, secret, adminPassword)
	if err != nil {
		t.Fatal(err)
	}

	for attempt := range accountThreshold + 5 {
		env.failFrom(Browser{IP: fmt.Sprintf("203.0.113.%d", attempt+1)})
	}
	env.clock.Advance(5 * time.Second)

	var limited RateLimitedError
	if _, err := env.loginFrom(Browser{IP: "192.0.2.50"}, secret, adminPassword); !errors.As(err, &limited) {
		t.Errorf("a new browser during the attack = %v, want rate limited", err)
	}
	// The authenticator's next code: the first login used the current one.
	nextCode, _ := totp.Code(secret, env.clock.Now().Add(30*time.Second))
	browser := Browser{IP: adminIP, DeviceToken: trusted.DeviceToken}
	if _, err := env.dashboard.LoginPassword(context.Background(), browser, adminPassword, nextCode); err != nil {
		t.Errorf("the admin's trusted browser during the attack = %v", err)
	}
}

func TestTrustedDeviceHasItsOwnLimit(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	env.clock.Advance(time.Minute)
	trusted, _ := env.loginFrom(Browser{IP: adminIP}, secret, adminPassword)

	for attempt := range deviceThreshold {
		env.failFrom(Browser{IP: fmt.Sprintf("203.0.113.%d", attempt+1), DeviceToken: trusted.DeviceToken})
	}

	var limited RateLimitedError
	if _, err := env.loginFrom(Browser{IP: "192.0.2.50", DeviceToken: trusted.DeviceToken}, secret, adminPassword); !errors.As(err, &limited) {
		t.Errorf("a stolen device cookie after %d failures = %v, want rate limited", deviceThreshold, err)
	}
}

// Review finding: one IPv6 host can rotate through its /64.
func TestIPv6ClientsAreThrottledPerSlash64(t *testing.T) {
	first := ipKey("2001:db8:1:2::10")
	second := ipKey("2001:db8:1:2:ffff::1")
	other := ipKey("2001:db8:1:3::10")

	if first != second {
		t.Errorf("addresses in one /64 got different keys: %v, %v", first, second)
	}
	if first == other {
		t.Error("different /64s share a key")
	}
	if ipKey("198.51.100.7").name != "ip:198.51.100.7" {
		t.Error("IPv4 keys changed")
	}
}

func TestLockoutIsNeverShortened(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	ctx := context.Background()
	key := ipKey(adminIP)
	longLock := env.clock.Now().Add(maxLockout)

	// A row as a slower request would find it: below threshold on its count,
	// but already locked by a faster request.
	env.service.db.ExecContext(ctx, "INSERT INTO login_throttles (key, failures, last_failure, locked_until) VALUES (?, 1, ?, ?)",
		key.name, env.clock.Now().Unix(), longLock.Unix())

	tx, _ := env.service.db.BeginTx(ctx, nil)
	if _, err := env.service.countFailure(ctx, tx, key); err != nil {
		t.Fatal(err)
	}
	tx.Commit()

	lockedUntil, _ := env.service.lockedUntil(ctx, env.service.db, key)
	if lockedUntil.Before(longLock) {
		t.Errorf("counting a failure shortened the lockout from %v to %v", longLock, lockedUntil)
	}
}

// Review finding: expired sessions and old throttle rows were never deleted.
func TestPruneDeletesOnlyWhatCanNoLongerBeUsed(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	ctx := context.Background()
	env.clock.Advance(time.Minute)

	old, _ := env.loginFrom(Browser{IP: adminIP}, secret, adminPassword)
	env.failFrom(Browser{IP: "203.0.113.9"})

	env.clock.Advance(failureMemory + time.Minute)
	fresh, err := env.loginFrom(Browser{IP: adminIP}, secret, adminPassword)
	if err != nil {
		t.Fatal(err)
	}

	if err := env.service.Prune(ctx); err != nil {
		t.Fatal(err)
	}

	if sessions := env.count("sessions"); sessions != 1 {
		t.Errorf("%d sessions after pruning, want only the fresh one", sessions)
	}
	if _, err := env.dashboard.CheckSession(ctx, fresh.SessionToken, adminIP); err != nil {
		t.Errorf("pruning ended a live session: %v", err)
	}
	if _, err := env.dashboard.CheckSession(ctx, old.SessionToken, adminIP); !errors.Is(err, ErrSessionEnded) {
		t.Errorf("old session = %v", err)
	}
	if throttles := env.count("login_throttles"); throttles != 0 {
		t.Errorf("%d stale throttle rows left", throttles)
	}
}

// Review finding: a bogus setup request cleared the throttle counters, so an
// attacker could alternate it with wrong logins and never be locked out.
func TestBogusSetupDoesntClearTheThrottle(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()

	for range ipThreshold - 1 {
		env.failFrom(Browser{IP: adminIP})
	}
	var limited RateLimitedError
	if _, err := env.dashboard.FinishSetup(ctx, Browser{IP: adminIP}, "not-a-token", "000000"); errors.As(err, &limited) {
		t.Fatalf("bogus setup was rate limited early: %v", err)
	}
	env.failFrom(Browser{IP: adminIP})

	if err := env.failFrom(Browser{IP: adminIP}); !errors.As(err, &limited) {
		t.Errorf("login after %d failures: err = %v, want rate limited", ipThreshold, err)
	}
}

// The setup session is a session like any other and trusts the browser.
func TestSetupLogsTheAdminIn(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	ctx := context.Background()
	token, _ := env.root.IssueSetupLink(ctx)
	enrolment, _ := env.dashboard.BeginSetup(ctx, adminIP, token, adminPassword)
	code, _ := totp.Code(enrolment.Secret, env.clock.Now())

	result, err := env.dashboard.FinishSetup(ctx, Browser{IP: adminIP, UserAgent: "Safari"}, token, code)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := env.dashboard.ListSessions(ctx, result.SessionToken)
	if err != nil || len(sessions) != 1 || sessions[0].Method != MethodSetup || sessions[0].UserAgent != "Safari" {
		t.Errorf("sessions after setup = %+v, %v", sessions, err)
	}
	if _, isTrusted, _ := env.service.trustedDevice(ctx, result.DeviceToken); !isTrusted {
		t.Error("setup didn't trust the browser")
	}
}
