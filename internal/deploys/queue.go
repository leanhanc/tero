// Package deploys is the deploy queue shared by the service and the agent
// through the SQLite store. Both queue deploys; the agent claims them one at
// a time across the server and moves them through their states. An app has
// at most one deploy running and one queued: a newer trigger updates the
// queued one, which builds the branch head whenever it starts.
package deploys

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// State is where a deploy is in its life.
type State string

const (
	Queued     State = "queued"
	Building   State = "building"
	Starting   State = "starting"
	Live       State = "live"
	Failed     State = "failed"
	Superseded State = "superseded"
)

// interruptedReason is shown on deploys the agent finds half done when it
// starts, after a crash, a restart or a reboot.
const interruptedReason = "Tero restarted while this deploy was running. Redeploy to try again"

// Deploy is the part of a deploy row the agent needs to run it.
type Deploy struct {
	ID    int64
	AppID int64
	State State
}

// TriggerType is what queued a deploy.
type TriggerType string

const (
	TriggerCreate TriggerType = "create"
	TriggerManual TriggerType = "manual"
	TriggerPush   TriggerType = "push"
	TriggerPoll   TriggerType = "poll"
)

// Trigger is what queued a deploy. PushedBy and RequestedSHA are set for a
// push, and only shown: the agent builds the branch head when it starts.
type Trigger struct {
	Type         TriggerType
	PushedBy     string
	RequestedSHA string
}

// Enqueue queues a deploy of the app's tracked branch and returns its id.
// When the app already has a deploy queued, that one takes the newer
// trigger instead, and its id is returned.
func Enqueue(ctx context.Context, db *sql.DB, appID int64, trigger Trigger, now time.Time) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var queuedID int64
	err = tx.QueryRowContext(ctx, "SELECT id FROM deploys WHERE app_id = ? AND state = 'queued'", appID).Scan(&queuedID)
	isAlreadyQueued := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	if isAlreadyQueued {
		_, err = tx.ExecContext(ctx,
			"UPDATE deploys SET trigger_type = ?, pushed_by = ?, requested_sha = ? WHERE id = ?",
			string(trigger.Type), trigger.PushedBy, trigger.RequestedSHA, queuedID)
		if err != nil {
			return 0, fmt.Errorf("update the queued deploy: %w", err)
		}
		return queuedID, tx.Commit()
	}

	result, err := tx.ExecContext(ctx,
		"INSERT INTO deploys (app_id, trigger_type, pushed_by, requested_sha, queued_at) VALUES (?, ?, ?, ?, ?)",
		appID, string(trigger.Type), trigger.PushedBy, trigger.RequestedSHA, now.Unix())
	if err != nil {
		return 0, fmt.Errorf("queue the deploy: %w", err)
	}
	deployID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	return deployID, tx.Commit()
}

// ClaimNext moves the oldest queued deploy to building and returns it. It
// returns false when nothing is queued or another deploy is still running,
// since builds run one at a time on the server.
func ClaimNext(ctx context.Context, db *sql.DB, now time.Time) (Deploy, bool, error) {
	// The agent asks every second. Checking outside a transaction first means
	// an empty queue doesn't take the database's write lock each time.
	var hasQueued bool
	err := db.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM deploys WHERE state = 'queued')").Scan(&hasQueued)
	if err != nil || !hasQueued {
		return Deploy{}, false, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return Deploy{}, false, err
	}
	defer tx.Rollback()

	var runningCount int
	err = tx.QueryRowContext(ctx, "SELECT count(*) FROM deploys WHERE state IN ('building', 'starting')").Scan(&runningCount)
	if err != nil {
		return Deploy{}, false, err
	}
	if runningCount > 0 {
		return Deploy{}, false, nil
	}

	deploy := Deploy{State: Building}
	err = tx.QueryRowContext(ctx, "SELECT id, app_id FROM deploys WHERE state = 'queued' ORDER BY id LIMIT 1").
		Scan(&deploy.ID, &deploy.AppID)
	if errors.Is(err, sql.ErrNoRows) {
		return Deploy{}, false, nil
	}
	if err != nil {
		return Deploy{}, false, err
	}

	_, err = tx.ExecContext(ctx, "UPDATE deploys SET state = 'building', started_at = ? WHERE id = ?", now.Unix(), deploy.ID)
	if err != nil {
		return Deploy{}, false, err
	}

	return deploy, true, tx.Commit()
}

// MarkStarting records that the deploy's image is built and its container is
// being started.
func MarkStarting(ctx context.Context, db *sql.DB, deployID int64) error {
	_, err := db.ExecContext(ctx, "UPDATE deploys SET state = 'starting' WHERE id = ? AND state = 'building'", deployID)
	return err
}

// Fail marks a deploy failed at step with a reason the admin can act on.
func Fail(ctx context.Context, db *sql.DB, deployID int64, step State, reason string, now time.Time) error {
	_, err := db.ExecContext(ctx,
		"UPDATE deploys SET state = 'failed', failed_step = ?, reason = ?, finished_at = ? WHERE id = ?",
		string(step), reason, now.Unix(), deployID)
	return err
}

// FailIfRunning fails a deploy that is still building or starting, at the
// step it was on. It leaves a deploy the pipeline already finished alone.
func FailIfRunning(ctx context.Context, db *sql.DB, deployID int64, reason string, now time.Time) error {
	_, err := db.ExecContext(ctx,
		"UPDATE deploys SET failed_step = state, state = 'failed', reason = ?, finished_at = ? WHERE id = ? AND state IN ('building', 'starting')",
		reason, now.Unix(), deployID)
	return err
}

// MarkInterrupted fails every deploy left building or starting, and returns
// how many there were. The agent calls it before claiming anything, so it
// only ever finds deploys a previous agent process was running.
func MarkInterrupted(ctx context.Context, db *sql.DB, now time.Time) (int64, error) {
	result, err := db.ExecContext(ctx,
		"UPDATE deploys SET failed_step = state, state = 'failed', reason = ?, finished_at = ? WHERE state IN ('building', 'starting')",
		interruptedReason, now.Unix())
	if err != nil {
		return 0, err
	}

	return result.RowsAffected()
}

// RecordCommit stores the commit a deploy builds.
func RecordCommit(ctx context.Context, db *sql.DB, deployID int64, sha, message string) error {
	_, err := db.ExecContext(ctx, "UPDATE deploys SET commit_sha = ?, commit_message = ? WHERE id = ?", sha, message, deployID)
	return err
}

// RecordImage stores the image a deploy built.
func RecordImage(ctx context.Context, db *sql.DB, deployID int64, image string) error {
	_, err := db.ExecContext(ctx, "UPDATE deploys SET image = ? WHERE id = ?", image, deployID)
	return err
}

// RecordContainer stores a deploy's container and the loopback port its
// PORT is published on.
func RecordContainer(ctx context.Context, db *sql.DB, deployID int64, container string, hostPort int) error {
	_, err := db.ExecContext(ctx, "UPDATE deploys SET container = ?, host_port = ? WHERE id = ?", container, hostPort, deployID)
	return err
}

// Release is a deploy that has, or had, a running container.
type Release struct {
	DeployID  int64
	AppID     int64
	AppName   string
	Image     string
	Container string
	HostPort  int
}

// LiveRelease returns the app's live release, if it has one.
func LiveRelease(ctx context.Context, db *sql.DB, appID int64) (Release, bool, error) {
	releases, err := queryReleases(ctx, db, "AND deploys.app_id = ?", appID)
	if err != nil || len(releases) == 0 {
		return Release{}, false, err
	}

	return releases[0], true, nil
}

// LiveReleases returns every app's live release.
func LiveReleases(ctx context.Context, db *sql.DB) ([]Release, error) {
	return queryReleases(ctx, db, "")
}

// SettledLiveReleases returns the live releases of apps with no deploy
// starting. While a deploy starts, it may have stopped the live container
// on purpose.
func SettledLiveReleases(ctx context.Context, db *sql.DB) ([]Release, error) {
	return queryReleases(ctx, db,
		"AND NOT EXISTS (SELECT 1 FROM deploys AS starting WHERE starting.app_id = deploys.app_id AND starting.state = 'starting')")
}

// IsSettledLive reports whether the deploy is still live with no deploy of
// its app starting.
func IsSettledLive(ctx context.Context, db *sql.DB, deployID int64) (bool, error) {
	var isSettled bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM deploys
			WHERE id = ? AND state = 'live'
				AND NOT EXISTS (SELECT 1 FROM deploys AS starting WHERE starting.app_id = deploys.app_id AND starting.state = 'starting')
		)`, deployID).Scan(&isSettled)
	return isSettled, err
}

// Promote makes the deploy live and the app's previous live deploy
// superseded, in one transaction so the app never has two or none.
func Promote(ctx context.Context, db *sql.DB, deployID int64, now time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		"UPDATE deploys SET state = 'superseded' WHERE state = 'live' AND app_id = (SELECT app_id FROM deploys WHERE id = ?)", deployID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE deploys SET state = 'live', finished_at = ? WHERE id = ?", now.Unix(), deployID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func queryReleases(ctx context.Context, db *sql.DB, filter string, args ...any) ([]Release, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT deploys.id, deploys.app_id, apps.name, deploys.image, deploys.container, deploys.host_port
		FROM deploys JOIN apps ON apps.id = deploys.app_id
		WHERE deploys.state = 'live' `+filter+` ORDER BY apps.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var releases []Release
	for rows.Next() {
		var release Release
		err := rows.Scan(&release.DeployID, &release.AppID, &release.AppName, &release.Image, &release.Container, &release.HostPort)
		if err != nil {
			return nil, err
		}
		releases = append(releases, release)
	}

	return releases, rows.Err()
}

// WaitUntilServed waits until the service's proxy sends the app's traffic to
// hostPort, and reports whether it did before the timeout.
func WaitUntilServed(ctx context.Context, db *sql.DB, appName string, hostPort int, timeout time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for {
		var count int
		err := db.QueryRowContext(ctx,
			"SELECT count(*) FROM served_routes WHERE app_name = ? AND host_port = ?", appName, hostPort).Scan(&count)
		if err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}

		if time.Now().After(deadline) {
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
