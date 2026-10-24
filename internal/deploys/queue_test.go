package deploys

import (
	"context"
	"database/sql"
	"errors"
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

	deployID, err := Enqueue(context.Background(), db, appID, testNow)
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

// deployments: Redeploy while deploying
func TestEnqueueRefusesWhileAppHasDeployInProgress(t *testing.T) {
	db := openTestDB(t, "api")
	mustEnqueue(t, db, 1)

	_, err := Enqueue(context.Background(), db, 1, testNow)
	if !errors.Is(err, ErrInProgress) {
		t.Fatalf("second enqueue error = %v, want ErrInProgress", err)
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
