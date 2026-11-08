package keys

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

const testApp = "shop"

var testSource = AppSource{Repository: "https://github.com/acme/shop", Branch: "main", RootDirectory: ""}

func setVariable(name, value string, scope VariableScope) VariableChange {
	return VariableChange{Name: name, Value: &value, Scope: scope}
}

func sealVariable(name, value string) VariableChange {
	change := setVariable(name, value, ScopeRuntime)
	change.IsSealed = true
	return change
}

func (env testEnv) save(changes ...VariableChange) (SavedVariables, error) {
	return env.dashboard.SaveVariables(context.Background(), *env.session, adminIP, testApp, testSource, changes)
}

func (env testEnv) release(phase DeployPhase, source AppSource) ([]ReleasedVariable, error) {
	return env.agent().ReleaseVariables(context.Background(), ReleaseRequest{App: testApp, Source: source, Phase: phase, DeployID: 7})
}

func (env testEnv) mustSave(t *testing.T, changes ...VariableChange) SavedVariables {
	t.Helper()
	saved, err := env.save(changes...)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

// expireConfirmation ages the setup's confirmation past its lifetime.
func (env testEnv) expireConfirmation() {
	env.clock.Advance(confirmationLifetime + time.Second)
}

// app-variables: Variable available at runtime, Variable removed
func TestSaveListAndRemove(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()

	saved := env.mustSave(t, setVariable("FOO", "bar", ScopeRuntime), setVariable("BUILD", "x", ScopeBuild))
	wantChanged := []ChangedVariable{{Name: "FOO", TouchesBuild: false}, {Name: "BUILD", TouchesBuild: true}}
	if !slices.Equal(saved.Changed, wantChanged) || saved.Count != 2 {
		t.Errorf("saved = %+v", saved)
	}

	listed, err := env.dashboard.ListVariables(ctx, *env.session, testApp)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Variables) != 2 || listed.Variables[1].Value != "bar" || listed.Source != testSource {
		t.Errorf("listed = %+v", listed)
	}

	env.mustSave(t, VariableChange{Name: "FOO", IsRemoved: true})
	released, err := env.release(PhaseRun, testSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(released) != 0 {
		t.Errorf("released after removal = %+v", released)
	}
}

// app-variables: Invalid save changes nothing
func TestInvalidSaveChangesNothing(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()

	cases := []VariableChange{
		setVariable("PORT", "1", ScopeRuntime),
		setVariable("1BAD", "1", ScopeRuntime),
		setVariable("A=B", "1", ScopeRuntime),
		setVariable("NUL", "a\x00b", ScopeRuntime),
		setVariable("BIG", strings.Repeat("x", maxVariableValueBytes+1), ScopeRuntime),
		setVariable("SCOPE", "1", "sometimes"),
		sealVariable("RAILPACK_BUILD_CMD", "1"),
		sealVariable("MISE_VERBOSE", "1"),
		sealVariable("TURBO_CACHE_DIR", "1"),
	}
	for _, invalid := range cases {
		if _, err := env.save(setVariable("GOOD", "1", ScopeRuntime), invalid); !errors.Is(err, ErrInvalidVariable) {
			t.Errorf("%s: err = %v", invalid.Name, err)
		}
	}

	listed, err := env.dashboard.ListVariables(ctx, *env.session, testApp)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Variables) != 0 || listed.Source != (AppSource{}) {
		t.Errorf("a refused save left %+v", listed)
	}
}

func TestAppLimits(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)

	var tooLarge []VariableChange
	for _, name := range []string{"A", "B", "C", "D", "E", "F", "G", "H", "I"} {
		tooLarge = append(tooLarge, setVariable(name, strings.Repeat("x", maxVariableValueBytes), ScopeRuntime))
	}
	if _, err := env.save(tooLarge...); !errors.Is(err, ErrTooManyVariables) {
		t.Errorf("past the app's bytes: err = %v", err)
	}
}

// app-variables: Sealed value never returned, No way back; key-service: Secrets
func TestSealedValueNeverReturned(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()

	env.mustSave(t, sealVariable("API_KEY", "s3cret-value"))

	listed, err := env.dashboard.ListVariables(ctx, *env.session, testApp)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Variables) != 1 || !listed.Variables[0].IsSealed || listed.Variables[0].Value != "" {
		t.Fatalf("listed = %+v", listed.Variables)
	}

	unsealing := VariableChange{Name: "API_KEY", Scope: ScopeRuntime}
	if _, err := env.save(unsealing); !errors.Is(err, ErrVariableSealed) {
		t.Errorf("unseal: err = %v", err)
	}

	released, err := env.release(PhaseRun, testSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(released) != 1 || released[0].Value != "s3cret-value" {
		t.Errorf("released = %+v", released)
	}
}

func TestValuesSealedAtRest(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	env.mustSave(t, setVariable("PLAIN", "plain-marker", ScopeRuntime), sealVariable("SEALED", "sealed-marker"))

	rows, err := env.service.db.Query("SELECT value FROM app_variables")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var value []byte
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(value, []byte("marker")) {
			t.Error("a value is stored in clear")
		}
	}
}

// key-service: Value moved to another row
func TestValueOpensOnlyInItsRow(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	env.mustSave(t, setVariable("A", "first", ScopeRuntime), setVariable("B", "second", ScopeRuntime))

	_, err := env.service.db.Exec("UPDATE app_variables SET value = (SELECT value FROM app_variables WHERE name = 'B') WHERE name = 'A'")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.release(PhaseRun, testSource); err == nil {
		t.Error("a value copied to another row opened")
	}
}

// app-variables: Unconfirmed seal, Unsealed change on a sealed app, Several
// saves after one check, Confirmation expires, App with no sealed variable
func TestFreshCheckOnAppsWithSealedVariables(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	ctx := context.Background()

	env.expireConfirmation()
	env.mustSave(t, setVariable("PLAIN", "1", ScopeRuntime))
	if _, err := env.save(sealVariable("API_KEY", "x")); !errors.Is(err, ErrNotConfirmed) {
		t.Errorf("unconfirmed seal: err = %v", err)
	}

	if err := env.dashboard.Confirm(ctx, *env.session, Browser{IP: adminIP}, adminPassword, env.code(secret, 0)); err != nil {
		t.Fatal(err)
	}
	env.mustSave(t, sealVariable("API_KEY", "x"))
	env.mustSave(t, setVariable("SECOND", "1", ScopeRuntime))

	env.expireConfirmation()
	if _, err := env.dashboard.CheckSession(ctx, *env.session, adminIP); err != nil {
		t.Errorf("an expired confirmation ended the session: %v", err)
	}
	if _, err := env.save(setVariable("NODE_OPTIONS", "--require=/evil", ScopeRuntime)); !errors.Is(err, ErrNotConfirmed) {
		t.Errorf("unsealed change on a sealed app: err = %v", err)
	}
	if _, err := env.save(VariableChange{Name: "API_KEY", IsRemoved: true}); !errors.Is(err, ErrNotConfirmed) {
		t.Errorf("removing a sealed variable: err = %v", err)
	}
}

// key-service: Build release; app-variables: Runtime only, Build only, Both
func TestReleaseByPhase(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	env.mustSave(t, setVariable("RUN", "r", ScopeRuntime), setVariable("BUILD", "b", ScopeBuild), setVariable("BOTH", "x", ScopeBoth))

	names := func(phase DeployPhase) []string {
		released, err := env.release(phase, testSource)
		if err != nil {
			t.Fatal(err)
		}
		found := []string{}
		for _, variable := range released {
			found = append(found, variable.Name)
		}
		return found
	}
	if got := names(PhaseBuild); !slices.Equal(got, []string{"BOTH", "BUILD"}) {
		t.Errorf("build got %v", got)
	}
	if got := names(PhaseRun); !slices.Equal(got, []string{"BOTH", "RUN"}) {
		t.Errorf("run got %v", got)
	}
}

// app-variables: Rewritten source refused, Approving a new source, Approval
// needs a check
func TestReleaseOnlyForTheApprovedSource(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	secret := env.setUp(t)
	ctx := context.Background()
	env.mustSave(t, sealVariable("API_KEY", "x"))

	rewritten := []AppSource{
		{Repository: "https://github.com/evil/shop", Branch: "main"},
		{Repository: testSource.Repository, Branch: "other"},
		{Repository: testSource.Repository, Branch: "main", RootDirectory: "sub"},
	}
	for _, source := range rewritten {
		if _, err := env.release(PhaseBuild, source); !errors.Is(err, ErrSourceNotApproved) {
			t.Errorf("%+v: err = %v", source, err)
		}
	}
	if countOf(env.eventTypes(t), EventVariablesRefused) != len(rewritten) {
		t.Errorf("refusals recorded: %v", env.eventTypes(t))
	}

	env.expireConfirmation()
	if err := env.dashboard.ApproveSource(ctx, *env.session, adminIP, testApp, rewritten[0]); !errors.Is(err, ErrNotConfirmed) {
		t.Errorf("unconfirmed approval: err = %v", err)
	}
	if err := env.dashboard.Confirm(ctx, *env.session, Browser{IP: adminIP}, adminPassword, env.code(secret, 0)); err != nil {
		t.Fatal(err)
	}
	if err := env.dashboard.ApproveSource(ctx, *env.session, adminIP, testApp, rewritten[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := env.release(PhaseRun, rewritten[0]); err != nil {
		t.Errorf("after approval: %v", err)
	}
	if _, err := env.release(PhaseRun, testSource); !errors.Is(err, ErrSourceNotApproved) {
		t.Errorf("old source after approval: err = %v", err)
	}
}

func TestSaveKeepsTheApprovedSource(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()
	env.mustSave(t, setVariable("A", "1", ScopeRuntime))

	other := AppSource{Repository: "https://github.com/evil/shop", Branch: "main"}
	if _, err := env.dashboard.SaveVariables(ctx, *env.session, adminIP, testApp, other, []VariableChange{setVariable("B", "2", ScopeRuntime)}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.release(PhaseRun, other); !errors.Is(err, ErrSourceNotApproved) {
		t.Errorf("a later save moved the source: err = %v", err)
	}
}

// key-service: Dashboard asks without a session, Dashboard asks for a release,
// Root asks for values
func TestWhoGetsValues(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()
	env.mustSave(t, setVariable("PLAIN", "plain-marker", ScopeRuntime), sealVariable("API_KEY", "sealed-marker"))

	if _, err := env.dashboard.ListVariables(ctx, "not a session", testApp); !errors.Is(err, ErrSessionEnded) {
		t.Errorf("list without a session: err = %v", err)
	}
	release := ReleaseRequest{App: testApp, Source: testSource, Phase: PhaseRun, DeployID: 7}
	if _, err := env.dashboard.ReleaseVariables(ctx, release); !errors.Is(err, ErrRefused) {
		t.Errorf("dashboard release: err = %v", err)
	}
	if _, err := env.root.ReleaseVariables(ctx, release); !errors.Is(err, ErrRefused) {
		t.Errorf("root release: err = %v", err)
	}
	if _, err := env.agent().ListVariables(ctx, *env.session, testApp); !errors.Is(err, ErrRefused) {
		t.Errorf("agent list: err = %v", err)
	}
	if _, err := env.dashboard.VariablesSummary(ctx); !errors.Is(err, ErrRefused) {
		t.Errorf("dashboard summary: err = %v", err)
	}

	summary, err := env.root.VariablesSummary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(summary) != 1 || len(summary[0].Variables) != 2 || summary[0].Source != testSource {
		t.Errorf("summary = %+v", summary)
	}
}

// key-service: Rotation re-seals values
func TestRotationResealsValues(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()
	env.mustSave(t, sealVariable("API_KEY", "same"))

	env.service.keys[2] = testKey(2)
	inUse, err := env.root.RotateKey(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(inUse, []int{2}) {
		t.Errorf("versions in use = %v", inUse)
	}

	delete(env.service.keys, 1)
	released, err := env.release(PhaseRun, testSource)
	if err != nil {
		t.Fatal(err)
	}
	if released[0].Value != "same" {
		t.Errorf("released = %+v", released)
	}
}

func TestLostKeyMarksValuesUnreadable(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()
	env.mustSave(t, setVariable("PLAIN", "x", ScopeRuntime))

	env.service.keys[2] = testKey(2)
	delete(env.service.keys, 1)

	listed, err := env.dashboard.ListVariables(ctx, *env.session, testApp)
	if err != nil {
		t.Fatal(err)
	}
	if !listed.Variables[0].IsUnreadable {
		t.Errorf("listed = %+v", listed.Variables)
	}
	if _, err := env.release(PhaseRun, testSource); !errors.Is(err, ErrKeyNotLoaded) {
		t.Errorf("release: err = %v", err)
	}
}

// security-log: Change recorded, Refused release is a warning; app-variables:
// Release shown
func TestVariableEvents(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()
	const secretValue = "sealed-marker"

	env.mustSave(t, sealVariable("API_KEY", secretValue))
	env.mustSave(t, sealVariable("API_KEY", "replaced-marker"))
	env.mustSave(t, setVariable("PLAIN", "plain-marker", ScopeRuntime))
	if _, err := env.release(PhaseRun, testSource); err != nil {
		t.Fatal(err)
	}
	if _, err := env.release(PhaseRun, AppSource{Repository: "https://github.com/evil/x", Branch: "main"}); err == nil {
		t.Fatal("the release should be refused")
	}

	types := env.eventTypes(t)
	for _, want := range []EventType{EventVariableSealed, EventVariableReplaced, EventVariablesChanged, EventSealedVariableReleased, EventVariablesRefused} {
		if countOf(types, want) != 1 {
			t.Errorf("%s recorded %d times in %v", want, countOf(types, want), types)
		}
	}
	for _, fields := range *env.journal {
		for _, value := range fields {
			if strings.Contains(value, "marker") {
				t.Errorf("a value reached the journal: %v", fields)
			}
		}
	}

	reads, err := env.dashboard.VariableReads(ctx, *env.session, testApp, "API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if len(reads) != 1 || reads[0].Phase != PhaseRun || reads[0].DeployID != 7 {
		t.Errorf("reads = %+v", reads)
	}

	for _, event := range []EventType{EventVariablesChanged, EventSealedVariableReleased, EventSourceApproved} {
		if err := env.dashboard.AppendEvent(ctx, event, adminIP); !errors.Is(err, ErrRefused) {
			t.Errorf("dashboard appended %s: err = %v", event, err)
		}
	}
}

// A lost key mustn't stop assume-breach from replacing it.
func TestMarkLostVariablesLetsRotationRun(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()
	env.mustSave(t, sealVariable("LOST", "x"))

	env.service.keys[2] = testKey(2)
	delete(env.service.keys, 1)
	if _, err := env.root.RotateKey(ctx); !errors.Is(err, ErrKeyNotLoaded) {
		t.Fatalf("rotation with a lost value: err = %v", err)
	}

	// As after a lost key: reset-login removes the login sealed with it.
	if err := env.root.ResetLogin(ctx); err != nil {
		t.Fatal(err)
	}
	if err := env.root.MarkLostVariables(ctx); err != nil {
		t.Fatal(err)
	}
	env.service.keys[3] = testKey(3)
	inUse, err := env.root.RotateKey(ctx)
	if err != nil {
		t.Fatalf("rotation after marking lost values: %v", err)
	}
	if slices.Contains(inUse, 0) {
		t.Errorf("versions in use = %v", inUse)
	}

	if _, err := env.release(PhaseRun, testSource); !errors.Is(err, ErrKeyNotLoaded) {
		t.Errorf("release of a lost value: err = %v", err)
	}
	listed := env.unreadableAfterSetup(t)
	if !listed {
		t.Error("the lost sealed variable isn't listed as needing to be set again")
	}
	env.mustSave(t, sealVariable("LOST", "new"))
	released, err := env.release(PhaseRun, testSource)
	if err != nil || released[0].Value != "new" {
		t.Errorf("after setting it again: %+v, %v", released, err)
	}
}

// unreadableAfterSetup sets the login up again and reports whether the lost
// variable is listed as unreadable.
func (env testEnv) unreadableAfterSetup(t *testing.T) bool {
	t.Helper()
	env.setUp(t)
	listed, err := env.dashboard.ListVariables(context.Background(), *env.session, testApp)
	if err != nil {
		t.Fatal(err)
	}
	return len(listed.Variables) == 1 && listed.Variables[0].IsUnreadable
}

func TestSourceGoesWithTheLastVariableAndFollowsTheFirstSeal(t *testing.T) {
	env := newTestEnv(t, map[int][]byte{1: testKey(1)})
	env.setUp(t)
	ctx := context.Background()

	evil := AppSource{Repository: "https://github.com/evil/shop", Branch: "main"}
	if _, err := env.dashboard.SaveVariables(ctx, *env.session, adminIP, testApp, evil, []VariableChange{setVariable("X", "1", ScopeRuntime)}); err != nil {
		t.Fatal(err)
	}
	env.mustSave(t, VariableChange{Name: "X", IsRemoved: true})
	if err := env.dashboard.ApproveSource(ctx, *env.session, adminIP, testApp, evil); !errors.Is(err, ErrRefused) {
		t.Errorf("approval with no variables: err = %v", err)
	}

	if _, err := env.dashboard.SaveVariables(ctx, *env.session, adminIP, testApp, evil, []VariableChange{setVariable("Y", "1", ScopeRuntime)}); err != nil {
		t.Fatal(err)
	}
	env.mustSave(t, sealVariable("API_KEY", "x"))
	if _, err := env.release(PhaseRun, testSource); err != nil {
		t.Errorf("the first seal didn't pin the current source: %v", err)
	}
	if _, err := env.release(PhaseRun, evil); !errors.Is(err, ErrSourceNotApproved) {
		t.Errorf("the earlier source still works: err = %v", err)
	}
}
