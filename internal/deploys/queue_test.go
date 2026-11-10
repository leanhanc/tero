package deploys

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/leanhanc/tero/internal/store"
)

var testNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func openTestDB(t *testing.T, appNames ...string) *sql.DB {
	t.Helper()

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	for _, name := range appNames {
		_, err := db.Exec("INSERT INTO apps (name, repository, branch, created_at) VALUES (?, 'https://github.com/o/r', 'main', 1)", name)
		if err != nil {
			t.Fatal(err)
		}
	}

	return db
}

func mustEnqueue(t *testing.T, db *sql.DB, appID int64) int64 {
	t.Helper()

	deployID, err := Enqueue(context.Background(), db, appID, Trigger{Type: TriggerManual}, testNow)
	if err != nil {
		t.Fatal(err)
	}

	return deployID
}

func stateOf(t *testing.T, db *sql.DB, deployID int64) (State, string) {
	t.Helper()

	var state, failedStep string
	if err := db.QueryRow("SELECT state, failed_step FROM deploys WHERE id = ?", deployID).Scan(&state, &failedStep); err != nil {
		t.Fatal(err)
	}

	return State(state), failedStep
}

// deployments: Redeploy while one is queued
func TestEnqueueUpdatesTheQueuedDeploy(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "api")
	firstID, err := Enqueue(ctx, db, 1, Trigger{Type: TriggerPush, PushedBy: "ana", RequestedSHA: "abc"}, testNow)
	if err != nil {
		t.Fatal(err)
	}

	secondID, err := Enqueue(ctx, db, 1, Trigger{Type: TriggerManual}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if secondID != firstID {
		t.Errorf("a second deploy %d was queued next to %d", secondID, firstID)
	}

	var count int
	var trigger, pushedBy string
	db.QueryRow("SELECT count(*), max(trigger_type), max(pushed_by) FROM deploys").Scan(&count, &trigger, &pushedBy)
	if count != 1 || trigger != "manual" || pushedBy != "" {
		t.Errorf("deploys = %d, trigger %q by %q; want 1, manual by nobody", count, trigger, pushedBy)
	}
}

// deployments: Redeploy while deploying, Two quick pushes
func TestEnqueueQueuesOneBehindARunningDeploy(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "api")
	runningID := mustEnqueue(t, db, 1)
	if _, _, err := ClaimNext(ctx, db, testNow); err != nil {
		t.Fatal(err)
	}

	behindID, err := Enqueue(ctx, db, 1, Trigger{Type: TriggerPush, PushedBy: "ana"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	againID, err := Enqueue(ctx, db, 1, Trigger{Type: TriggerPush, PushedBy: "bo"}, testNow)
	if err != nil {
		t.Fatal(err)
	}

	if behindID == runningID || againID != behindID {
		t.Errorf("running %d, queued %d then %d; want one queued deploy behind the running one", runningID, behindID, againID)
	}
	if state, _ := stateOf(t, db, behindID); state != Queued {
		t.Errorf("the deploy behind is %s", state)
	}
	var pushedBy string
	db.QueryRow("SELECT pushed_by FROM deploys WHERE id = ?", behindID).Scan(&pushedBy)
	if pushedBy != "bo" {
		t.Errorf("the queued deploy names %q, want the newest pusher", pushedBy)
	}
}

// deployments: Two apps deployed together
func TestClaimNextRunsOneDeployAtATimeInQueueOrder(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "api", "web")
	firstID := mustEnqueue(t, db, 1)
	secondID := mustEnqueue(t, db, 2)

	first, isClaimed, err := ClaimNext(ctx, db, testNow)
	if err != nil || !isClaimed || first.ID != firstID {
		t.Fatalf("first claim = %+v, %v, %v; want deploy %d", first, isClaimed, err, firstID)
	}

	_, isClaimed, err = ClaimNext(ctx, db, testNow)
	if err != nil || isClaimed {
		t.Fatalf("claim while a deploy is building = %v, %v; want nothing", isClaimed, err)
	}

	if err := Fail(ctx, db, firstID, Building, "boom", testNow); err != nil {
		t.Fatal(err)
	}
	second, isClaimed, err := ClaimNext(ctx, db, testNow)
	if err != nil || !isClaimed || second.ID != secondID {
		t.Fatalf("claim after the first ended = %+v, %v, %v; want deploy %d", second, isClaimed, err, secondID)
	}
}

// deployments: Interrupted deploy
func TestMarkInterruptedFailsDeploysLeftRunning(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "api", "web")
	buildingID := mustEnqueue(t, db, 1)
	queuedID := mustEnqueue(t, db, 2)
	if _, _, err := ClaimNext(ctx, db, testNow); err != nil {
		t.Fatal(err)
	}

	count, err := MarkInterrupted(ctx, db, testNow)
	if err != nil || count != 1 {
		t.Fatalf("MarkInterrupted = %d, %v; want 1", count, err)
	}

	if state, step := stateOf(t, db, buildingID); state != Failed || step != string(Building) {
		t.Errorf("interrupted deploy = %s at %q, want failed at building", state, step)
	}
	if state, _ := stateOf(t, db, queuedID); state != Queued {
		t.Errorf("queued deploy = %s, want it still queued", state)
	}
}

func TestIsSettledLiveOnlyWhileLiveWithNoDeployStarting(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "api")
	liveID := mustEnqueue(t, db, 1)
	if _, err := db.Exec("UPDATE deploys SET state = 'live' WHERE id = ?", liveID); err != nil {
		t.Fatal(err)
	}

	assertSettled := func(want bool) {
		t.Helper()
		isSettled, err := IsSettledLive(ctx, db, liveID)
		if err != nil || isSettled != want {
			t.Fatalf("IsSettledLive = %v, %v; want %v", isSettled, err, want)
		}
	}

	assertSettled(true)

	nextID := mustEnqueue(t, db, 1)
	if _, err := db.Exec("UPDATE deploys SET state = 'starting' WHERE id = ?", nextID); err != nil {
		t.Fatal(err)
	}
	assertSettled(false)

	if err := Promote(ctx, db, nextID, testNow); err != nil {
		t.Fatal(err)
	}
	assertSettled(false)
}

// deployments: Variable change while a push is queued
func TestSkippingTheBuildCantCancelAQueuedBuild(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "api")

	pushed, err := Enqueue(ctx, db, 1, Trigger{Type: TriggerPush, PushedBy: "ana"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	merged, err := Enqueue(ctx, db, 1, Trigger{Type: TriggerVariables, SkipsBuild: true}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if merged != pushed {
		t.Fatalf("got a second deploy %d", merged)
	}

	deploy, isClaimed, err := ClaimNext(ctx, db, testNow)
	if err != nil || !isClaimed {
		t.Fatalf("claim: %v, %v", isClaimed, err)
	}
	if !deploy.NeedsBuild {
		t.Error("the queued push no longer builds")
	}

	var trigger string
	if err := db.QueryRow("SELECT trigger_type FROM deploys WHERE id = ?", pushed).Scan(&trigger); err != nil {
		t.Fatal(err)
	}
	if trigger != string(TriggerPush) {
		t.Errorf("trigger = %s", trigger)
	}
}

func TestABuildUpgradesAQueuedRestart(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "api")

	if _, err := Enqueue(ctx, db, 1, Trigger{Type: TriggerVariables, SkipsBuild: true}, testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := Enqueue(ctx, db, 1, Trigger{Type: TriggerManual}, testNow); err != nil {
		t.Fatal(err)
	}

	deploy, _, err := ClaimNext(ctx, db, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if !deploy.NeedsBuild {
		t.Error("the restart wasn't upgraded to a build")
	}
}

// app-variables: Runtime variable changed, New values break the app; a save
// made after a deploy started stays pending.
func TestPromoteClearsOnlyWhatTheDeployApplied(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "api")
	pend := func(name string) {
		t.Helper()
		if _, err := db.Exec("INSERT INTO app_pending_changes (app_id, name, touches_build) VALUES (1, ?, 0)", name); err != nil {
			t.Fatal(err)
		}
	}
	pending := func() int {
		t.Helper()
		var count int
		if err := db.QueryRow("SELECT count(*) FROM app_pending_changes").Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}

	pend("A")
	failing := mustEnqueue(t, db, 1)
	if _, _, err := ClaimNext(ctx, db, testNow); err != nil {
		t.Fatal(err)
	}
	if err := Fail(ctx, db, failing, Building, "no", testNow); err != nil {
		t.Fatal(err)
	}
	if pending() != 1 {
		t.Error("a failed deploy cleared the notice")
	}

	deployID := mustEnqueue(t, db, 1)
	if _, _, err := ClaimNext(ctx, db, testNow); err != nil {
		t.Fatal(err)
	}
	pend("B")
	if err := Promote(ctx, db, deployID, testNow); err != nil {
		t.Fatal(err)
	}
	if pending() != 1 {
		t.Errorf("pending after promote = %d, want the save made during the deploy", pending())
	}
}

func TestReuseReleaseCopiesImageAndCommit(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "api")
	first := mustEnqueue(t, db, 1)
	if _, err := db.Exec("UPDATE deploys SET state = 'live', image = 'img', commit_sha = 'abc', commit_message = 'msg' WHERE id = ?", first); err != nil {
		t.Fatal(err)
	}
	second := mustEnqueue(t, db, 1)

	if err := ReuseRelease(ctx, db, second, first); err != nil {
		t.Fatal(err)
	}
	var image, sha, message string
	if err := db.QueryRow("SELECT image, commit_sha, commit_message FROM deploys WHERE id = ?", second).Scan(&image, &sha, &message); err != nil {
		t.Fatal(err)
	}
	if image != "img" || sha != "abc" || message != "msg" {
		t.Errorf("got %s %s %s", image, sha, message)
	}
}

// A restart applies runtime-only changes and leaves a change that reaches the
// build pending.
func TestRestartLeavesBuildChangesPending(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "api")
	for name, touchesBuild := range map[string]bool{"RUN": false, "BUILD": true} {
		if _, err := db.Exec("INSERT INTO app_pending_changes (app_id, name, touches_build) VALUES (1, ?, ?)", name, touchesBuild); err != nil {
			t.Fatal(err)
		}
	}

	deployID, err := Enqueue(ctx, db, 1, Trigger{Type: TriggerVariables, SkipsBuild: true}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := ClaimNext(ctx, db, testNow); err != nil {
		t.Fatal(err)
	}
	if err := Promote(ctx, db, deployID, testNow); err != nil {
		t.Fatal(err)
	}

	var left string
	if err := db.QueryRow("SELECT group_concat(name) FROM app_pending_changes").Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != "BUILD" {
		t.Errorf("pending after a restart = %q, want BUILD", left)
	}
}

// Audit finding TERO-03: after a breach the agent restarted apps from images
// an attacker could have changed.
func TestFailLiveReleasesLeavesOtherDeploysAlone(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, "api", "web")
	liveID := mustEnqueue(t, db, 1)
	if _, err := db.Exec("UPDATE deploys SET state = 'live' WHERE id = ?", liveID); err != nil {
		t.Fatal(err)
	}
	queuedID := mustEnqueue(t, db, 2)

	count, err := FailLiveReleases(ctx, db, testNow)
	if err != nil || count != 1 {
		t.Fatalf("FailLiveReleases = %d, %v; want 1", count, err)
	}
	if state, _ := stateOf(t, db, liveID); state != Failed {
		t.Errorf("live release = %s, want failed", state)
	}
	if state, _ := stateOf(t, db, queuedID); state != Queued {
		t.Errorf("queued deploy = %s, want it still queued", state)
	}
	releases, err := SettledLiveReleases(ctx, db)
	if err != nil || len(releases) != 0 {
		t.Errorf("live releases after = %v, %v", releases, err)
	}
}
