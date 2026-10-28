package agent

import (
	"context"
	"testing"
	"time"

	"github.com/leanhanc/tero/internal/apps"
	"github.com/leanhanc/tero/internal/source"
)

// github-connection: Repository gone
func TestPollStopsAfterThreeFailures(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	db.Exec("INSERT INTO apps (name, repository, branch, created_at) VALUES ('api', 'https://github.com/o/r', 'main', 1)")
	poller := Poller{DB: db, Now: time.Now}

	for range apps.MaxPollFailures {
		if err := poller.recordFailure(ctx, 1, source.ErrRepositoryNotFound); err != nil {
			t.Fatal(err)
		}
	}

	polled, err := poller.polledApps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(polled) != 0 {
		t.Errorf("an app with %d failed polls is still polled", apps.MaxPollFailures)
	}
}

// github-connection: No change
func TestPollSkipsAppsWithADeployOnTheWay(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	db.Exec("INSERT INTO apps (name, repository, branch, created_at) VALUES ('api', 'https://github.com/o/r', 'main', 1)")
	db.Exec("INSERT INTO apps (name, repository, branch, github_repository_id, created_at) VALUES ('covered', 'https://github.com/o/c', 'main', 9, 1)")
	db.Exec("INSERT INTO apps (name, repository, branch, deploy_on_push, created_at) VALUES ('paused', 'https://github.com/o/p', 'main', 0, 1)")
	db.Exec("INSERT INTO deploys (app_id, state, queued_at) VALUES (1, 'queued', 1)")

	polled, err := Poller{DB: db, Now: time.Now}.polledApps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(polled) != 0 {
		t.Errorf("polled %+v; want none (queued, covered by the App, or paused)", polled)
	}
}

// A poll's deploy that fails before fetching isn't queued again for the
// same head every minute.
func TestKnownHead(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	db.Exec("INSERT INTO apps (name, repository, branch, created_at) VALUES ('api', 'https://github.com/o/r', 'main', 1)")

	if isKnown, _ := isKnownHead(ctx, db, 1, "abc"); isKnown {
		t.Error("a head is known before any deploy")
	}
	db.Exec("INSERT INTO deploys (app_id, state, commit_sha, queued_at) VALUES (1, 'live', 'abc', 1)")
	if isKnown, _ := isKnownHead(ctx, db, 1, "abc"); !isKnown {
		t.Error("the live commit isn't known")
	}
	db.Exec("INSERT INTO deploys (app_id, state, trigger_type, requested_sha, queued_at) VALUES (1, 'failed', 'poll', 'def', 2)")
	if isKnown, _ := isKnownHead(ctx, db, 1, "def"); !isKnown {
		t.Error("a head a failed poll deploy asked for is polled again")
	}
	if isKnown, _ := isKnownHead(ctx, db, 1, "ghi"); isKnown {
		t.Error("a new head counts as known")
	}
}
