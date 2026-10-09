package auth

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/leanhanc/tero/internal/password"
	"github.com/leanhanc/tero/internal/store"
	"github.com/leanhanc/tero/internal/totp"
)

const (
	adminPassword = "a long passphrase nobody has used"
	adminIP       = "198.51.100.7"
)

// testClock is a settable clock, starting at a fixed time.
type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time                 { return c.now }
func (c *testClock) Advance(duration time.Duration) { c.now = c.now.Add(duration) }

type journalEntry struct {
	message string
	fields  map[string]string
}

type testEnv struct {
	service *Service
	clock   *testClock
	journal *[]journalEntry
}

func newTestEnv(t *testing.T) testEnv {
	t.Helper()

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	entries := &[]journalEntry{}
	service := New(db, Options{
		Domain:    "dash.example.com",
		Passwords: password.Policy{Breaches: noBreaches{}},
		Now:       clock.Now,
		Journal: func(_ int, message string, fields map[string]string) error {
			*entries = append(*entries, journalEntry{message, fields})
			return nil
		},
	})

	return testEnv{service: service, clock: clock, journal: entries}
}

type noBreaches struct{}

func (noBreaches) IsBreached(context.Context, string) (bool, error) { return false, nil }

func tokenFromLink(t *testing.T, link string) string {
	t.Helper()
	_, token, isFound := strings.Cut(link, "#")
	if !isFound || !strings.HasPrefix(link, "https://dash.example.com/setup#") {
		t.Fatalf("unexpected link %q", link)
	}
	return token
}

// setUp walks the setup link flow and returns the TOTP secret.
func (env testEnv) setUp(t *testing.T) string {
	t.Helper()
	secret, _ := env.setUpWithDevice(t)
	return secret
}

// setUpWithDevice also returns the trusted-device token setup issued.
func (env testEnv) setUpWithDevice(t *testing.T) (string, string) {
	t.Helper()
	ctx := context.Background()

	link, err := env.service.IssueSetupLink(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	token := tokenFromLink(t, link)

	enrolment, err := env.service.BeginSetup(ctx, adminIP, token, adminPassword)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.Code(enrolment.Secret, env.clock.Now())
	result, err := env.service.FinishSetup(ctx, adminIP, token, code)
	if err != nil {
		t.Fatal(err)
	}

	return enrolment.Secret, result.DeviceToken
}

func (env testEnv) login(secret string) (string, error) {
	result, err := env.loginFrom(adminIP, "", secret)
	return result.SessionToken, err
}

func (env testEnv) loginFrom(ip, deviceToken, secret string) (LoginResult, error) {
	code, _ := totp.Code(secret, env.clock.Now())
	return env.service.Login(context.Background(), ip, deviceToken, adminPassword, code)
}

func (env testEnv) eventTypes(t *testing.T) []EventType {
	t.Helper()
	events, err := env.service.RecentEvents(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}

	var types []EventType
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

func TestAdminCompletesSetup(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	link, _ := env.service.IssueSetupLink(ctx, "")
	token := tokenFromLink(t, link)
	enrolment, err := env.service.BeginSetup(ctx, adminIP, token, adminPassword)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enrolment.URI, "otpauth://totp/Tero:admin@dash.example.com?") {
		t.Errorf("URI = %q", enrolment.URI)
	}

	code, _ := totp.Code(enrolment.Secret, env.clock.Now())
	result, err := env.service.FinishSetup(ctx, adminIP, token, code)
	if err != nil {
		t.Fatal(err)
	}

	if err := env.service.Authenticate(ctx, result.SessionToken); err != nil {
		t.Errorf("the admin is not logged in after setup: %v", err)
	}
}

func TestSetupWithoutTOTPCreatesNoAccount(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	link, _ := env.service.IssueSetupLink(ctx, "")
	token := tokenFromLink(t, link)
	if _, err := env.service.BeginSetup(ctx, adminIP, token, adminPassword); err != nil {
		t.Fatal(err)
	}

	if _, err := env.service.FinishSetup(ctx, adminIP, token, "000000"); !errors.Is(err, ErrInvalidCode) {
		t.Errorf("FinishSetup(wrong code) = %v", err)
	}
	if isSetUp, _ := env.service.IsSetUp(ctx); isSetUp {
		t.Error("an account exists without a confirmed TOTP code")
	}
}

func TestSetupLinkReusedIsRejected(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	link, _ := env.service.IssueSetupLink(ctx, "")
	token := tokenFromLink(t, link)
	enrolment, _ := env.service.BeginSetup(ctx, adminIP, token, adminPassword)
	code, _ := totp.Code(enrolment.Secret, env.clock.Now())
	if _, err := env.service.FinishSetup(ctx, adminIP, token, code); err != nil {
		t.Fatal(err)
	}

	if _, err := env.service.BeginSetup(ctx, adminIP, token, "another long passphrase here"); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("BeginSetup(used link) = %v", err)
	}
}

func TestSetupLinkExpires(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	link, _ := env.service.IssueSetupLink(ctx, "")
	env.clock.Advance(setupLinkLifetime)

	if _, err := env.service.BeginSetup(ctx, adminIP, tokenFromLink(t, link), adminPassword); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("BeginSetup(expired link) = %v", err)
	}
}

func TestNewSetupLinkReplacesTheOldOne(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	oldLink, _ := env.service.IssueSetupLink(ctx, "")
	env.service.IssueSetupLink(ctx, "")

	if _, err := env.service.BeginSetup(ctx, adminIP, tokenFromLink(t, oldLink), adminPassword); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("BeginSetup(replaced link) = %v", err)
	}
}

func TestSetupRejectsShortPassword_v5_0_0_6_2_1(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	link, _ := env.service.IssueSetupLink(ctx, "")
	_, err := env.service.BeginSetup(ctx, adminIP, tokenFromLink(t, link), "fourteen chars")
	if !errors.Is(err, password.ErrTooShort) {
		t.Errorf("BeginSetup(14 chars) = %v", err)
	}
}

func TestNoLoginBeforeSetup(t *testing.T) {
	env := newTestEnv(t)

	if _, err := env.service.Login(context.Background(), adminIP, "", adminPassword, "123456"); !errors.Is(err, ErrNotSetUp) {
		t.Errorf("Login before setup = %v", err)
	}
}

func TestSecondAccountCannotBeCreated(t *testing.T) {
	env := newTestEnv(t)
	env.setUp(t)
	ctx := context.Background()

	link, _ := env.service.IssueSetupLink(ctx, "")
	if _, err := env.service.BeginSetup(ctx, adminIP, tokenFromLink(t, link), "a second admin passphrase"); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("BeginSetup with an admin present = %v", err)
	}
}

func TestCorrectPasswordAndCodeLogsIn(t *testing.T) {
	env := newTestEnv(t)
	secret := env.setUp(t)
	env.clock.Advance(time.Minute)

	if _, err := env.login(secret); err != nil {
		t.Fatalf("Login = %v", err)
	}
}

func TestWrongCodeDoesNotSayWhichFactor(t *testing.T) {
	env := newTestEnv(t)
	env.setUp(t)
	env.clock.Advance(time.Minute)
	ctx := context.Background()

	_, wrongCodeErr := env.service.Login(ctx, adminIP, "", adminPassword, "000000")
	_, wrongPasswordErr := env.service.Login(ctx, adminIP, "", "not the admin passphrase", "000000")

	if !errors.Is(wrongCodeErr, ErrInvalidCredentials) || wrongCodeErr != wrongPasswordErr {
		t.Errorf("wrong code = %v, wrong password = %v; want the same generic error", wrongCodeErr, wrongPasswordErr)
	}
}

func TestLogin_v5_0_0_6_5_1_ReplayedCodeFails(t *testing.T) {
	env := newTestEnv(t)
	secret := env.setUp(t)
	env.clock.Advance(time.Minute)

	code, _ := totp.Code(secret, env.clock.Now())
	ctx := context.Background()
	if _, err := env.service.Login(ctx, adminIP, "", adminPassword, code); err != nil {
		t.Fatal(err)
	}

	env.clock.Advance(5 * time.Second)
	if _, err := env.service.Login(ctx, adminIP, "", adminPassword, code); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("replayed code = %v", err)
	}
}

func TestLogin_v5_0_0_6_3_1_RepeatedFailuresFromOneIP(t *testing.T) {
	env := newTestEnv(t)
	secret := env.setUp(t)
	ctx := context.Background()

	for range ipThreshold {
		env.service.Login(ctx, adminIP, "", "wrong passphrase for the admin", "000000")
	}

	env.clock.Advance(5 * time.Second)
	_, err := env.login(secret)
	var limited RateLimitedError
	if !errors.As(err, &limited) {
		t.Fatalf("login after %d failures = %v, want rate limited", ipThreshold, err)
	}

	firstDelay := limited.RetryAfter
	env.clock.Advance(firstDelay)
	env.service.Login(ctx, adminIP, "", "wrong passphrase for the admin", "000000")
	_, err = env.service.Login(ctx, adminIP, "", adminPassword, "000000")
	if !errors.As(err, &limited) || limited.RetryAfter <= firstDelay {
		t.Errorf("delay after another failure = %v, want more than %v", limited.RetryAfter, firstDelay)
	}
}

func TestLogin_v5_0_0_6_3_1_DistributedGuessingHitsAccountLimit(t *testing.T) {
	env := newTestEnv(t)
	secret := env.setUp(t)
	ctx := context.Background()

	for attempt := range accountThreshold {
		ip := "203.0.113." + string(rune('a'+attempt))
		env.service.Login(ctx, ip, "", "wrong passphrase for the admin", "000000")
	}

	env.clock.Advance(5 * time.Second)
	var limited RateLimitedError
	if _, err := env.login(secret); !errors.As(err, &limited) {
		t.Errorf("login from a fresh IP after failures from many IPs = %v, want rate limited", err)
	}
}

func TestLogin_v5_0_0_6_3_1_LimitDoesNotLeakValidity(t *testing.T) {
	env := newTestEnv(t)
	secret := env.setUp(t)
	ctx := context.Background()

	for range ipThreshold {
		env.service.Login(ctx, adminIP, "", "wrong passphrase for the admin", "000000")
	}

	_, withRightCredentials := env.login(secret)
	_, withWrongCredentials := env.service.Login(ctx, adminIP, "", "wrong passphrase for the admin", "000000")
	if withRightCredentials.Error() != withWrongCredentials.Error() {
		t.Errorf("rate-limited responses differ: %q vs %q", withRightCredentials, withWrongCredentials)
	}
}

func TestSessionIdleTimeout(t *testing.T) {
	env := newTestEnv(t)
	secret := env.setUp(t)
	env.clock.Advance(time.Minute)
	sessionToken, _ := env.login(secret)

	env.clock.Advance(idleTimeout)
	if err := env.service.Authenticate(context.Background(), sessionToken); !errors.Is(err, ErrSessionExpired) {
		t.Errorf("session after 30 idle minutes = %v", err)
	}
}

func TestSessionAbsoluteTimeout(t *testing.T) {
	env := newTestEnv(t)
	secret := env.setUp(t)
	env.clock.Advance(time.Minute)
	sessionToken, _ := env.login(secret)
	ctx := context.Background()

	for elapsed := time.Duration(0); elapsed < absoluteTimeout-20*time.Minute; elapsed += 20 * time.Minute {
		env.clock.Advance(20 * time.Minute)
		if err := env.service.Authenticate(ctx, sessionToken); err != nil {
			t.Fatalf("active session ended early: %v", err)
		}
	}

	env.clock.Advance(20 * time.Minute)
	if err := env.service.Authenticate(ctx, sessionToken); !errors.Is(err, ErrSessionExpired) {
		t.Errorf("session after 12 hours of activity = %v", err)
	}
}

func TestResetLoginRevokesSessionsAndCredentials(t *testing.T) {
	env := newTestEnv(t)
	secret := env.setUp(t)
	env.clock.Advance(time.Minute)
	sessionToken, _ := env.login(secret)
	ctx := context.Background()

	link, err := env.service.ResetLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tokenFromLink(t, link)

	if err := env.service.Authenticate(ctx, sessionToken); !errors.Is(err, ErrSessionExpired) {
		t.Errorf("session after reset = %v", err)
	}
	env.clock.Advance(time.Minute)
	if _, err := env.login(secret); !errors.Is(err, ErrNotSetUp) {
		t.Errorf("old credentials after reset = %v", err)
	}
}

func TestEventsRecordedWithoutSecrets(t *testing.T) {
	env := newTestEnv(t)
	secret := env.setUp(t)
	env.clock.Advance(time.Minute)
	ctx := context.Background()

	env.service.Login(ctx, adminIP, "", "wrong passphrase for the admin", "000000")
	sessionToken, _ := env.login(secret)
	env.service.ResetLogin(ctx)

	types := env.eventTypes(t)
	for _, want := range []EventType{EventSetupLinkIssued, EventSetupCompleted, EventLoginFailed, EventLoginSucceeded, EventLoginReset} {
		if !containsEvent(types, want) {
			t.Errorf("no %s event in %v", want, types)
		}
	}

	events, _ := env.service.RecentEvents(ctx, 100)
	secrets := []string{adminPassword, secret, sessionToken}
	for _, event := range events {
		for _, value := range []string{string(event.Type), event.IP, event.Detail} {
			for _, sensitive := range secrets {
				if strings.Contains(value, sensitive) {
					t.Errorf("event %+v contains a secret", event)
				}
			}
		}
	}

	if len(*env.journal) != len(events) {
		t.Errorf("journal has %d entries, database has %d events", len(*env.journal), len(events))
	}
	for _, entry := range *env.journal {
		for _, sensitive := range secrets {
			if strings.Contains(entry.message, sensitive) {
				t.Errorf("journal entry %q contains a secret", entry.message)
			}
		}
	}
}

func TestRateLimitTriggerIsRecorded(t *testing.T) {
	env := newTestEnv(t)
	env.setUp(t)
	ctx := context.Background()

	for range ipThreshold {
		env.service.Login(ctx, adminIP, "", "wrong passphrase for the admin", "000000")
	}

	if !containsEvent(env.eventTypes(t), EventRateLimited) {
		t.Error("no rate_limited event")
	}
}

func containsEvent(types []EventType, want EventType) bool {
	for _, eventType := range types {
		if eventType == want {
			return true
		}
	}
	return false
}

// Review finding: parallel logins got past the throttle because the check and
// the count were separate steps.
func TestLogin_v5_0_0_6_3_1_ParallelFailuresCannotPassThrottle(t *testing.T) {
	env := newTestEnv(t)
	env.setUp(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	for range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			env.service.Login(ctx, adminIP, "", "wrong passphrase for the admin", "000000")
		}()
	}
	wg.Wait()

	checked := 0
	for _, eventType := range env.eventTypes(t) {
		if eventType == EventLoginFailed {
			checked++
		}
	}
	if checked > ipThreshold {
		t.Errorf("%d parallel attempts had their credentials checked, want at most %d", checked, ipThreshold)
	}
}

// Review finding: strangers failing logins locked the real admin out of the
// account for as long as they kept going.
func TestTrustedDeviceIgnoresAccountLockout(t *testing.T) {
	env := newTestEnv(t)
	secret, deviceToken := env.setUpWithDevice(t)
	ctx := context.Background()

	for attempt := range accountThreshold + 5 {
		attackerIP := fmt.Sprintf("203.0.113.%d", attempt+1)
		env.service.Login(ctx, attackerIP, "", "wrong passphrase for the admin", "000000")
	}
	env.clock.Advance(5 * time.Second)

	var limited RateLimitedError
	if _, err := env.loginFrom("192.0.2.50", "", secret); !errors.As(err, &limited) {
		t.Errorf("a new browser during the attack = %v, want rate limited", err)
	}
	// The authenticator's next code: setup already used the current one.
	nextCode, _ := totp.Code(secret, env.clock.Now().Add(30*time.Second))
	if _, err := env.service.Login(ctx, adminIP, deviceToken, adminPassword, nextCode); err != nil {
		t.Errorf("the admin's trusted browser during the attack = %v", err)
	}
}

func TestTrustedDeviceHasItsOwnLimit(t *testing.T) {
	env := newTestEnv(t)
	secret, deviceToken := env.setUpWithDevice(t)
	ctx := context.Background()

	for attempt := range deviceThreshold {
		ip := fmt.Sprintf("203.0.113.%d", attempt+1)
		env.service.Login(ctx, ip, deviceToken, "wrong passphrase for the admin", "000000")
	}

	var limited RateLimitedError
	if _, err := env.loginFrom("192.0.2.50", deviceToken, secret); !errors.As(err, &limited) {
		t.Errorf("a stolen device cookie after %d failures = %v, want rate limited", deviceThreshold, err)
	}
}

func TestResetLoginForgetsTrustedDevices(t *testing.T) {
	env := newTestEnv(t)
	_, deviceToken := env.setUpWithDevice(t)

	env.service.ResetLogin(context.Background())

	if _, isTrusted, _ := env.service.trustedDevice(context.Background(), deviceToken); isTrusted {
		t.Error("a device stays trusted after reset-login")
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
	env := newTestEnv(t)
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
	env := newTestEnv(t)
	secret := env.setUp(t)
	ctx := context.Background()
	env.clock.Advance(time.Minute)

	oldSession, _ := env.login(secret)
	env.service.Login(ctx, "203.0.113.9", "", "wrong passphrase for the admin", "000000")

	env.clock.Advance(failureMemory + time.Minute)
	freshSession, err := env.login(secret)
	if err != nil {
		t.Fatal(err)
	}

	if err := env.service.Prune(ctx); err != nil {
		t.Fatal(err)
	}

	count := func(table string) int {
		var rows int
		env.service.db.QueryRow("SELECT count(*) FROM " + table).Scan(&rows)
		return rows
	}
	if sessions := count("sessions"); sessions != 1 {
		t.Errorf("%d sessions after pruning, want only the fresh one", sessions)
	}
	if err := env.service.Authenticate(ctx, freshSession); err != nil {
		t.Errorf("pruning ended a live session: %v", err)
	}
	if err := env.service.Authenticate(ctx, oldSession); !errors.Is(err, ErrSessionExpired) {
		t.Errorf("old session = %v", err)
	}
	if throttles := count("login_throttles"); throttles != 0 {
		t.Errorf("%d stale throttle rows left", throttles)
	}
}
