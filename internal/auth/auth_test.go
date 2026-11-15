package auth

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leanhanc/tero/internal/keys"
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
	// keysRoot is root's client of the in-process key service.
	keysRoot *keys.Client
	// journal is what the dashboard and the key service wrote.
	journal *[]journalEntry
	// session is the session setUp started.
	session *string
}

func newTestEnv(t *testing.T) testEnv {
	t.Helper()

	keysDB, err := store.OpenWith(context.Background(), filepath.Join(t.TempDir(), "keys.db"), keys.Migrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { keysDB.Close() })

	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	entries := &[]journalEntry{}
	writeJournal := func(_ int, message string, fields map[string]string) error {
		*entries = append(*entries, journalEntry{message, fields})
		return nil
	}
	keyService, err := keys.NewService(keysDB, map[int][]byte{1: make([]byte, 32)}, keys.Options{Domain: "dash.example.com", Now: clock.Now, Journal: writeJournal})
	if err != nil {
		t.Fatal(err)
	}

	service := New(Options{
		Keys:      keys.NewLocalClient(keyService, keys.CallerDashboard),
		Domain:    "dash.example.com",
		PublicIP:  "203.0.113.5",
		Passwords: password.Policy{Breaches: noBreaches{}},
		Journal:   writeJournal,
	})

	return testEnv{service: service, clock: clock, keysRoot: keys.NewLocalClient(keyService, keys.CallerRoot), journal: entries, session: new(string)}
}

// issueLink is what `sudo tero init` and `reset-login` do: root asks the key
// service for a token.
func (env testEnv) issueLink(t *testing.T) string {
	t.Helper()
	token, err := env.keysRoot.IssueSetupLink(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return SetupLink("dash.example.com", token)
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
	ctx := context.Background()

	token := tokenFromLink(t, env.issueLink(t))

	enrolment, err := env.service.BeginSetup(ctx, adminIP, token, adminPassword)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.Code(enrolment.Secret, env.clock.Now())
	result, err := env.service.FinishSetup(ctx, Browser{IP: adminIP}, token, code)
	if err != nil {
		t.Fatal(err)
	}
	*env.session = result.SessionToken

	return enrolment.Secret
}

func TestAdminCompletesSetup(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	link := env.issueLink(t)
	token := tokenFromLink(t, link)
	enrolment, err := env.service.BeginSetup(ctx, adminIP, token, adminPassword)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(enrolment.URI, "otpauth://totp/Tero:admin@dash.example.com?") {
		t.Errorf("URI = %q", enrolment.URI)
	}

	code, _ := totp.Code(enrolment.Secret, env.clock.Now())
	result, err := env.service.FinishSetup(ctx, Browser{IP: adminIP}, token, code)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := env.service.Authenticate(ctx, result.SessionToken, adminIP); err != nil {
		t.Errorf("the admin is not logged in after setup: %v", err)
	}
}

func TestSetupRejectsShortPassword_v5_0_0_6_2_1(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	link := env.issueLink(t)
	_, err := env.service.BeginSetup(ctx, adminIP, tokenFromLink(t, link), "7 chars")
	if !errors.Is(err, password.ErrTooShort) {
		t.Errorf("BeginSetup(7 chars) = %v", err)
	}
}

func TestNoLoginBeforeSetup(t *testing.T) {
	env := newTestEnv(t)

	if _, err := env.service.Login(context.Background(), Browser{IP: adminIP}, adminPassword, "123456"); !errors.Is(err, ErrNotSetUp) {
		t.Errorf("Login before setup = %v", err)
	}
}

// With an admin in place, a setup link reads as invalid, whether it is the
// one setup used or a new one.
func TestSecondAccountCannotBeCreated(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	usedToken := tokenFromLink(t, env.issueLink(t))
	enrolment, err := env.service.BeginSetup(ctx, adminIP, usedToken, adminPassword)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.Code(enrolment.Secret, env.clock.Now())
	if _, err := env.service.FinishSetup(ctx, Browser{IP: adminIP}, usedToken, code); err != nil {
		t.Fatal(err)
	}

	newToken := tokenFromLink(t, env.issueLink(t))
	for name, token := range map[string]string{"used link": usedToken, "new link": newToken} {
		if _, err := env.service.BeginSetup(ctx, adminIP, token, "a second owner passphrase"); !errors.Is(err, ErrInvalidLink) {
			t.Errorf("BeginSetup with the %s = %v", name, err)
		}
	}
}

func TestWrongCodeDoesNotSayWhichFactor(t *testing.T) {
	env := newTestEnv(t)
	env.setUp(t)
	env.clock.Advance(time.Minute)
	ctx := context.Background()

	_, wrongCodeErr := env.service.Login(ctx, Browser{IP: adminIP}, adminPassword, "000000")
	_, wrongPasswordErr := env.service.Login(ctx, Browser{IP: adminIP}, "not the admin passphrase", "000000")

	if !errors.Is(wrongCodeErr, ErrInvalidCredentials) || wrongCodeErr != wrongPasswordErr {
		t.Errorf("wrong code = %v, wrong password = %v; want the same generic error", wrongCodeErr, wrongPasswordErr)
	}
}

// dashboard-access: Key service not answering
func TestLoginFailsClosedWithoutTheKeyService(t *testing.T) {
	env := newTestEnv(t)
	env.service.keys = keys.NewClient(filepath.Join(t.TempDir(), "missing.sock"))
	ctx := context.Background()

	if _, err := env.service.Login(ctx, Browser{IP: adminIP}, adminPassword, "123456"); !errors.Is(err, ErrKeysUnavailable) {
		t.Errorf("Login without the key service = %v", err)
	}
	if _, err := env.service.Authenticate(ctx, "any token", adminIP); !errors.Is(err, ErrKeysUnavailable) {
		t.Errorf("a signed-in page without the key service = %v", err)
	}
}

// security-log: Append fails
func TestUnstoredEventGoesToTheJournal(t *testing.T) {
	env := newTestEnv(t)
	env.service.keys = keys.NewClient(filepath.Join(t.TempDir(), "missing.sock"))

	env.service.record(context.Background(), keys.EventBreachCheckSkipped, adminIP)

	entries := *env.journal
	if len(entries) != 1 || entries[0].fields["TERO_EVENT_UNSTORED"] != "1" || entries[0].fields["TERO_EVENT"] != "breach_check_skipped" {
		t.Errorf("journal = %+v", entries)
	}
}

// account-security: Domain label in the password, at setup
func TestSetupRefusesContextWords_v5_0_0_6_2_11(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	link := env.issueLink(t)
	_, err := env.service.BeginSetup(ctx, adminIP, tokenFromLink(t, link), "my dash.example.com password")
	var contextWord password.ContextWordError
	if !errors.As(err, &contextWord) {
		t.Errorf("BeginSetup(password with the domain) = %v", err)
	}
}

// The new password is checked before the key service is asked, so a refused
// one changes nothing and costs no guess.
func TestChangePasswordChecksThePolicyFirst(t *testing.T) {
	env := newTestEnv(t)
	secret := env.setUp(t)
	ctx := context.Background()
	env.clock.Advance(time.Minute)
	code, _ := totp.Code(secret, env.clock.Now())

	err := env.service.ChangePassword(ctx, *env.session, Browser{IP: adminIP}, adminPassword, code, "tero tero tero tero", true)
	var contextWord password.ContextWordError
	if !errors.As(err, &contextWord) || contextWord.Word != "tero" {
		t.Fatalf("ChangePassword(context word) = %v", err)
	}
	if err := env.service.ChangePassword(ctx, *env.session, Browser{IP: adminIP}, adminPassword, code, "a brand new passphrase", true); err != nil {
		t.Errorf("ChangePassword with the unused code = %v", err)
	}
}

// A failed passkey login reads the same whether the login is set up or not.
func TestPasskeyFailuresReadTheSame(t *testing.T) {
	env := newTestEnv(t)
	ctx := context.Background()

	options, err := env.service.BeginPasskeyLogin(ctx, adminIP)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.service.LoginPasskey(ctx, Browser{IP: adminIP}, options.ChallengeID, []byte(`{}`)); !errors.Is(err, ErrPasskeyFailed) {
		t.Errorf("passkey login before setup = %v", err)
	}
	env.setUp(t)
	if _, err := env.service.LoginPasskey(ctx, Browser{IP: adminIP}, options.ChallengeID, []byte(`{}`)); !errors.Is(err, ErrPasskeyFailed) {
		t.Errorf("passkey login with a bad answer = %v", err)
	}
}
