package keys

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/leanhanc/tero/internal/store"
	"github.com/leanhanc/tero/internal/totp"
)

const (
	adminPassword = "a long passphrase nobody has used"
	adminIP       = "198.51.100.7"
)

type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time                 { return c.now }
func (c *testClock) Advance(duration time.Duration) { c.now = c.now.Add(duration) }

type testEnv struct {
	service   *Service
	clock     *testClock
	root      *Client
	dashboard *Client
	journal   *[]map[string]string
}

func testKey(fill byte) []byte { return bytes.Repeat([]byte{fill}, keySize) }

func newTestEnv(t *testing.T, keys map[int][]byte) testEnv {
	t.Helper()

	db, err := store.OpenWith(context.Background(), filepath.Join(t.TempDir(), "keys.db"), Migrations)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	entries := &[]map[string]string{}
	service := NewService(db, keys, Options{
		Now: clock.Now,
		Journal: func(_ int, _ string, fields map[string]string) error {
			*entries = append(*entries, fields)
			return nil
		},
	})

	return testEnv{
		service:   service,
		clock:     clock,
		root:      NewLocalClient(service, CallerRoot),
		dashboard: NewLocalClient(service, CallerDashboard),
		journal:   entries,
	}
}

// setUp walks the setup flow through the clients and returns the TOTP secret.
func (env testEnv) setUp(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	token, err := env.root.IssueSetupLink(ctx)
	if err != nil {
		t.Fatal(err)
	}
	enrolment, err := env.dashboard.BeginSetup(ctx, token, adminPassword)
	if err != nil {
		t.Fatal(err)
	}
	code, _ := totp.Code(enrolment.Secret, env.clock.Now())
	if err := env.dashboard.FinishSetup(ctx, adminIP, token, code); err != nil {
		t.Fatal(err)
	}

	return enrolment.Secret
}

func (env testEnv) login(secret, givenPassword string) error {
	code, _ := totp.Code(secret, env.clock.Now())
	return env.dashboard.CheckLogin(context.Background(), adminIP, givenPassword, code)
}

func (env testEnv) eventTypes(t *testing.T) []EventType {
	t.Helper()
	events, err := env.root.RecentEvents(context.Background(), 1000)
	if err != nil {
		t.Fatal(err)
	}

	var types []EventType
	for _, event := range events {
		types = append(types, event.Type)
	}
	return types
}

func countOf(types []EventType, want EventType) int {
	count := 0
	for _, eventType := range types {
		if eventType == want {
			count++
		}
	}
	return count
}

func TestSetupAndLogin(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	env.clock.Advance(time.Minute)

	if err := env.login(secret, adminPassword); err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if err := env.login(secret, "not the password"); !errors.Is(err, ErrWrongLogin) {
		t.Fatalf("wrong password: err = %v", err)
	}
}

// key-service: Secret encrypted at rest
func TestSecretSealedAtRest(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)

	var stored []byte
	var version int
	env.service.db.QueryRow("SELECT totp_secret, totp_key_version FROM admin").Scan(&stored, &version)
	if bytes.Contains(stored, []byte(secret)) || version != 1 {
		t.Fatalf("stored secret is in clear or has version %d", version)
	}
}

// key-service: Ciphertext moved to another row
func TestSealedValueOnlyOpensInItsRow(t *testing.T) {
	keys := keyring{1: testKey(1)}
	value, err := keys.seal(pendingSecretPurpose("a"), []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := keys.open(adminSecretPurpose, value); err == nil {
		t.Error("a pending secret opened as the admin's")
	}
	if _, err := keys.open(pendingSecretPurpose("b"), value); err == nil {
		t.Error("a pending secret opened on another link")
	}

	relabelled := sealed{version: 2, ciphertext: value.ciphertext}
	keys[2] = testKey(1)
	if _, err := keys.open(pendingSecretPurpose("a"), relabelled); err == nil {
		t.Error("a value opened under another key version")
	}
}

// key-service: Replayed code
func TestCodeWorksOnce(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	env.clock.Advance(time.Minute)

	if err := env.login(secret, adminPassword); err != nil {
		t.Fatal(err)
	}
	if err := env.login(secret, adminPassword); !errors.Is(err, ErrWrongLogin) {
		t.Fatalf("replayed code: err = %v", err)
	}
}

// key-service: Dashboard can't replace the credentials
func TestDashboardCantReplaceCredentials(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	ctx := context.Background()

	if _, err := env.dashboard.BeginSetup(ctx, "any token", "a new password chosen"); !errors.Is(err, ErrAlreadySetUp) {
		t.Errorf("setup.begin with an admin: err = %v", err)
	}
	if err := env.dashboard.FinishSetup(ctx, adminIP, "any token", "123456"); !errors.Is(err, ErrAlreadySetUp) {
		t.Errorf("setup.finish with an admin: err = %v", err)
	}

	// Even a link root issued can't replace an existing admin.
	token, _ := env.root.IssueSetupLink(ctx)
	if _, err := env.dashboard.BeginSetup(ctx, token, "a new password chosen"); !errors.Is(err, ErrAlreadySetUp) {
		t.Errorf("setup.begin with a fresh link and an admin: err = %v", err)
	}

	env.clock.Advance(time.Minute)
	if err := env.login(secret, adminPassword); err != nil {
		t.Errorf("the original login stopped working: %v", err)
	}
}

// key-service: Dashboard can't issue a link, Request outside the caller's role
func TestCallersLimitedToTheirVerbs(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	ctx := context.Background()

	if _, err := env.dashboard.IssueSetupLink(ctx); !errors.Is(err, ErrRefused) {
		t.Errorf("dashboard setup.issue: err = %v", err)
	}
	if _, err := env.dashboard.RotateKey(ctx); !errors.Is(err, ErrRefused) {
		t.Errorf("dashboard key.rotate: err = %v", err)
	}
	if err := env.dashboard.ResetLogin(ctx); !errors.Is(err, ErrRefused) {
		t.Errorf("dashboard login.reset: err = %v", err)
	}
	if err := env.root.CheckLogin(ctx, "", adminPassword, "123456"); !errors.Is(err, ErrRefused) {
		t.Errorf("root login.check: err = %v", err)
	}
	if _, err := NewLocalClient(env.service, Caller("agent")).Status(ctx); !errors.Is(err, ErrRefused) {
		t.Errorf("unknown caller: err = %v", err)
	}
}

func TestSetupNeedsAnIssuedUnexpiredLink(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	ctx := context.Background()

	if _, err := env.dashboard.BeginSetup(ctx, "made up", adminPassword); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("made-up token: err = %v", err)
	}

	token, _ := env.root.IssueSetupLink(ctx)
	env.clock.Advance(setupLinkLifetime)
	if _, err := env.dashboard.BeginSetup(ctx, token, adminPassword); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("expired token: err = %v", err)
	}
}

func TestSetupWithoutCodeDoesNotComplete(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	ctx := context.Background()

	token, _ := env.root.IssueSetupLink(ctx)
	if err := env.dashboard.FinishSetup(ctx, adminIP, token, "123456"); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("finish before begin: err = %v", err)
	}
	env.dashboard.BeginSetup(ctx, token, adminPassword)
	if err := env.dashboard.FinishSetup(ctx, adminIP, token, "000000"); !errors.Is(err, ErrInvalidCode) {
		t.Errorf("wrong code: err = %v", err)
	}

	status, _ := env.root.Status(ctx)
	if status.IsSetUp {
		t.Error("setup completed without a valid code")
	}
}

// key-service: Guessing codes with the password
func TestWrongCodesWithThePasswordLockLogin(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	ctx := context.Background()

	for range codeFailureLimit {
		env.clock.Advance(time.Minute)
		if err := env.dashboard.CheckLogin(ctx, adminIP, adminPassword, "000000"); !errors.Is(err, ErrWrongLogin) {
			t.Fatalf("wrong code: err = %v", err)
		}
	}

	env.clock.Advance(time.Minute)
	if err := env.login(secret, adminPassword); !errors.Is(err, ErrLocked) {
		t.Fatalf("login after the limit: err = %v", err)
	}

	types := env.eventTypes(t)
	if countOf(types, EventCodeFailed) != codeFailureLimit || countOf(types, EventCodeLocked) != 1 {
		t.Errorf("events = %v", types)
	}

	env.clock.Advance(codeLockout)
	if err := env.login(secret, adminPassword); err != nil {
		t.Errorf("login after the lockout: %v", err)
	}
}

// key-service: Strangers can't lock the admin out
func TestWrongPasswordsDontLockLogin(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	ctx := context.Background()

	for range codeFailureLimit + 5 {
		env.clock.Advance(time.Second)
		env.dashboard.CheckLogin(ctx, "203.0.113.9", "a guessed password", "000000")
	}

	env.clock.Advance(time.Minute)
	if err := env.login(secret, adminPassword); err != nil {
		t.Fatalf("admin locked out by wrong passwords: %v", err)
	}
}

func TestResetLoginClearsCredentialsAndLock(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	ctx := context.Background()

	if err := env.root.ResetLogin(ctx); err != nil {
		t.Fatal(err)
	}
	env.clock.Advance(time.Minute)
	if err := env.login(secret, adminPassword); !errors.Is(err, ErrNotSetUp) {
		t.Fatalf("login after reset: err = %v", err)
	}

	// A new setup works again.
	env.setUp(t)
}

// key-service: Rotation
func TestRotationResealsTheAdmin(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	ctx := context.Background()

	env.service.keys[2] = testKey(2)
	inUse, err := env.root.RotateKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(inUse) != 1 || inUse[0] != 2 {
		t.Fatalf("versions in use after rotation = %v, want [2]", inUse)
	}

	delete(env.service.keys, 1)
	env.clock.Advance(time.Minute)
	if err := env.login(secret, adminPassword); err != nil {
		t.Fatalf("login after rotation: %v", err)
	}
}

func TestRotationResealsAPendingSetup(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	ctx := context.Background()

	token, _ := env.root.IssueSetupLink(ctx)
	enrolment, err := env.dashboard.BeginSetup(ctx, token, adminPassword)
	if err != nil {
		t.Fatal(err)
	}

	env.service.keys[2] = testKey(2)
	if inUse, err := env.root.RotateKey(ctx); err != nil || len(inUse) != 1 || inUse[0] != 2 {
		t.Fatalf("rotation: %v, in use %v", err, inUse)
	}
	delete(env.service.keys, 1)

	code, _ := totp.Code(enrolment.Secret, env.clock.Now())
	if err := env.dashboard.FinishSetup(ctx, adminIP, token, code); err != nil {
		t.Fatalf("finishing setup after rotation: %v", err)
	}
}

// key-service: Interrupted rotation
func TestRotationRefusesWithAMissingKey(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	ctx := context.Background()

	// Version 1 sealed the admin, but only version 2 is loaded.
	env.service.keys = keyring{2: testKey(2)}
	if _, err := env.root.RotateKey(ctx); !errors.Is(err, ErrKeyNotLoaded) {
		t.Fatalf("rotation without the old key: err = %v", err)
	}

	// With both loaded, as after a restart, it completes.
	env.service.keys = keyring{1: testKey(1), 2: testKey(2)}
	if _, err := env.root.RotateKey(ctx); err != nil {
		t.Fatal(err)
	}
	env.clock.Advance(time.Minute)
	if err := env.login(secret, adminPassword); err != nil {
		t.Fatal(err)
	}
}

// security-log: Login events come from tero-keys, Caller can't choose who or when
func TestEventSourcesAndTimes(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	ctx := context.Background()
	env.clock.Advance(time.Minute)
	env.login(secret, adminPassword)

	if err := env.dashboard.AppendEvent(ctx, EventRateLimited, adminIP); err != nil {
		t.Fatal(err)
	}

	events, _ := env.root.RecentEvents(ctx, 10)
	if events[0].Type != EventRateLimited || events[0].Source != "dashboard" || !events[0].At.Equal(env.clock.Now()) {
		t.Errorf("appended event = %+v", events[0])
	}
	if events[1].Type != EventLoginSucceeded || events[1].Source != SourceKeys {
		t.Errorf("login event = %+v", events[1])
	}
}

// security-log: Event type not allowed for the caller
func TestCallersCantAppendOtherEvents(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	ctx := context.Background()

	for _, eventType := range []EventType{EventLoginSucceeded, EventBreachReset, "made_up"} {
		if err := env.dashboard.AppendEvent(ctx, eventType, ""); !errors.Is(err, ErrRefused) {
			t.Errorf("dashboard appending %s: err = %v", eventType, err)
		}
	}
	if types := env.eventTypes(t); len(types) != 0 {
		t.Errorf("stored %v", types)
	}
}

// security-log: Journal copy
func TestEventsGoToTheJournalWithoutSecrets(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)

	for _, fields := range *env.journal {
		for _, value := range fields {
			if strings.Contains(value, secret) || strings.Contains(value, adminPassword) {
				t.Fatalf("journal entry holds a secret: %v", fields)
			}
		}
		if fields["TERO_EVENT"] == "" {
			t.Errorf("journal entry without TERO_EVENT: %v", fields)
		}
	}
	if len(*env.journal) == 0 {
		t.Fatal("nothing written to the journal")
	}
}

// security-log: Flood from the dashboard
func TestFloodIsCappedAndCounted(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	ctx := context.Background()
	env.root.IssueSetupLink(ctx)

	for range 10_000 {
		env.dashboard.AppendEvent(ctx, EventRateLimited, adminIP)
	}
	if err := env.service.FlushCounts(ctx); err != nil {
		t.Fatal(err)
	}

	types := env.eventTypes(t)
	if got := countOf(types, EventRateLimited); got != appendLimit {
		t.Errorf("stored %d appended events, want %d", got, appendLimit)
	}
	if countOf(types, EventSetupLinkIssued) != 1 {
		t.Error("the earlier event was pushed out")
	}

	events, _ := env.root.RecentEvents(ctx, 1)
	if events[0].Type != EventEventsDropped || events[0].Count != 10_000-appendLimit {
		t.Errorf("dropped event = %+v", events[0])
	}
}

// security-log: Old event
func TestPruneDeletesOldEvents(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	ctx := context.Background()
	env.root.IssueSetupLink(ctx)

	env.clock.Advance(eventRetention + time.Hour)
	env.root.AppendEvent(ctx, EventBreachReset, "")
	if err := env.service.Prune(ctx); err != nil {
		t.Fatal(err)
	}

	types := env.eventTypes(t)
	if len(types) != 1 || types[0] != EventBreachReset {
		t.Errorf("events after prune = %v", types)
	}
}

func TestLoadKeysReadsEveryVersion(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "kek_1"), testKey(1), 0o400)
	os.WriteFile(filepath.Join(dir, "kek_2"), testKey(2), 0o400)
	os.WriteFile(filepath.Join(dir, "other"), []byte("x"), 0o400)

	keys, err := LoadKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || !bytes.Equal(keys[2], testKey(2)) {
		t.Errorf("keys = %v", keys)
	}

	os.WriteFile(filepath.Join(dir, "kek_3"), []byte("short"), 0o400)
	if _, err := LoadKeys(dir); err == nil {
		t.Error("a short key was accepted")
	}
	if _, err := LoadKeys(t.TempDir()); err == nil {
		t.Error("an empty credentials directory was accepted")
	}
}

// key-service: Other process running as tero
func TestOnlyTheDashboardsExactCgroupCounts(t *testing.T) {
	for _, cgroup := range []string{
		"/user.slice/user-999.slice/user@999.service/app.slice/tero-agent.service",
		"/user.slice/user-999.slice/user@999.service/app.slice/x/system.slice/tero.service",
		"/system.slice/tero.service/child",
		"/system.slice/ssh.service",
	} {
		if _, err := (peers{dashboard: dashboardCgroup}).callerFor(cgroup); err == nil {
			t.Errorf("%s accepted", cgroup)
		}
	}

	if caller, err := (peers{dashboard: dashboardCgroup}).callerFor("/system.slice/tero.service"); err != nil || caller != CallerDashboard {
		t.Errorf("dashboard cgroup: %v, %v", caller, err)
	}
}

// key-service: Oversized or idle request
func TestOversizedMessageRefused(t *testing.T) {
	var buffer bytes.Buffer
	binary.Write(&buffer, binary.BigEndian, uint32(maxMessage+1))
	buffer.Write(make([]byte, 16))

	var req request
	if err := readMessage(&buffer, &req); err == nil {
		t.Error("an oversized message was read")
	}
}

// key-service: Guessing passwords from the dashboard
func TestLoginChecksAreLimitedPerMinute(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	ctx := context.Background()
	env.clock.Advance(time.Minute)

	for range loginCheckLimit {
		env.dashboard.CheckLogin(ctx, adminIP, "a guessed password", "000000")
	}
	if err := env.login(secret, adminPassword); !errors.Is(err, ErrHashingIsBusy) {
		t.Fatalf("check over the limit: err = %v", err)
	}
	if err := env.service.FlushCounts(ctx); err != nil {
		t.Fatal(err)
	}
	events, _ := env.root.RecentEvents(ctx, 1)
	if events[0].Type != EventRateLimited || events[0].Source != SourceKeys || events[0].Count != 1 {
		t.Errorf("refused checks recorded as %+v", events[0])
	}

	env.clock.Advance(time.Minute)
	if err := env.login(secret, adminPassword); err != nil {
		t.Errorf("login the next minute: %v", err)
	}
}

func TestDashboardSeesWrongCodesAsFailedLogins(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()
	env.dashboard.CheckLogin(ctx, adminIP, adminPassword, "000000")

	events, err := env.dashboard.RecentEvents(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if events[0].Type != EventLoginFailed {
		t.Errorf("the dashboard sees %s", events[0].Type)
	}
	if types := env.eventTypes(t); countOf(types, EventCodeFailed) != 1 {
		t.Errorf("root sees %v", types)
	}
}

func TestFloodDoesntPushOutResets(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	ctx := context.Background()
	env.root.AppendEvent(ctx, EventBreachReset, "")

	tx, _ := env.service.db.BeginTx(ctx, nil)
	for range maxEvents + 10 {
		tx.ExecContext(ctx, "INSERT INTO security_events (at, type, source, ip, count) VALUES (?, 'login_failed', 'tero-keys', '', 1)",
			env.clock.Now().Unix())
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := env.service.Prune(ctx); err != nil {
		t.Fatal(err)
	}

	var resets, total int
	env.service.db.QueryRowContext(ctx, "SELECT count(*) FILTER (WHERE type = 'breach_reset'), count(*) FROM security_events").Scan(&resets, &total)
	if resets != 1 || total > maxEvents+1 {
		t.Errorf("after prune: %d resets, %d events", resets, total)
	}
}
