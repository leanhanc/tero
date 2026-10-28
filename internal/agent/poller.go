package agent

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/leanhanc/tero/internal/apps"
	"github.com/leanhanc/tero/internal/deploys"
	"github.com/leanhanc/tero/internal/source"
)

const (
	// pollPeriod is how often public repositories outside the GitHub App are
	// checked for new commits.
	pollPeriod = time.Minute
	// pollJitter spreads each round's start, so servers don't all ask
	// GitHub at the top of the minute.
	pollJitter = 10 * time.Second
)

// polledApp is an app whose public repository is polled.
type polledApp struct {
	id         int64
	repository source.Repository
	branch     string
}

// Poller deploys new commits on public repositories the GitHub App doesn't
// cover, which send no webhooks. It reads only the branch's head, with no
// credential.
type Poller struct {
	DB  *sql.DB
	Now func() time.Time
}

// Run polls every pollPeriod until ctx ends.
func (p Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(pollPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(rand.N(pollJitter)):
		}

		if err := p.pollAll(ctx); err != nil && ctx.Err() == nil {
			slog.Error("poll public repositories", "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p Poller) pollAll(ctx context.Context) error {
	polled, err := p.polledApps(ctx)
	if err != nil {
		return err
	}

	for _, app := range polled {
		if err := p.poll(ctx, app); err != nil && ctx.Err() == nil {
			slog.Warn("poll a repository", "repository", app.repository.URL(), "error", err)
		}
	}

	return nil
}

// polledApps are the apps on public repositories outside the GitHub App,
// with deploy on push on, and polling not stopped by failures. Apps with a
// deploy queued or running are left out: that deploy builds the head anyway.
func (p Poller) polledApps(ctx context.Context) ([]polledApp, error) {
	rows, err := p.DB.QueryContext(ctx, `
		SELECT id, repository, branch FROM apps
		WHERE github_repository_id = 0 AND deploy_on_push = 1 AND poll_failures < ?
			AND NOT EXISTS (SELECT 1 FROM deploys WHERE deploys.app_id = apps.id AND state IN ('queued', 'building', 'starting'))`,
		apps.MaxPollFailures)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var polled []polledApp
	for rows.Next() {
		var app polledApp
		var repositoryURL string
		if err := rows.Scan(&app.id, &repositoryURL, &app.branch); err != nil {
			return nil, err
		}

		// The service wrote these; they are checked again before use.
		repository, err := source.ParseRepository(repositoryURL)
		if err != nil || source.CheckBranchName(app.branch) != nil {
			continue
		}
		app.repository = repository
		polled = append(polled, app)
	}

	return polled, rows.Err()
}

func (p Poller) poll(ctx context.Context, app polledApp) error {
	head, err := source.BranchHead(ctx, app.repository, "", app.branch)
	isGone := errors.Is(err, source.ErrRepositoryNotFound) || errors.Is(err, source.ErrBranchNotFound)
	if isGone {
		return p.recordFailure(ctx, app.id, err)
	}
	if err != nil {
		return err
	}

	if _, err := p.DB.ExecContext(ctx, "UPDATE apps SET poll_failures = 0, poll_error = '' WHERE id = ?", app.id); err != nil {
		return err
	}

	isKnown, err := isKnownHead(ctx, p.DB, app.id, head)
	if err != nil || isKnown {
		return err
	}

	_, err = deploys.Enqueue(ctx, p.DB, app.id, deploys.Trigger{Type: deploys.TriggerPoll, RequestedSHA: head}, p.Now())
	return err
}

// recordFailure counts a poll that found the repository or branch gone. The
// third in a row stops polling, and the app page shows why.
func (p Poller) recordFailure(ctx context.Context, appID int64, reason error) error {
	_, err := p.DB.ExecContext(ctx,
		"UPDATE apps SET poll_failures = poll_failures + 1, poll_error = ? WHERE id = ?", reason.Error(), appID)
	return err
}

// isKnownHead reports whether the app's newest deploy built head, or was
// queued for it by a poll. A poll's deploy that failed before fetching
// anything isn't queued again for the same head every minute.
func isKnownHead(ctx context.Context, db *sql.DB, appID int64, head string) (bool, error) {
	var commitSHA, requestedSHA string
	err := db.QueryRowContext(ctx,
		"SELECT commit_sha, requested_sha FROM deploys WHERE app_id = ? ORDER BY id DESC LIMIT 1", appID).
		Scan(&commitSHA, &requestedSHA)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	return head == commitSHA || head == requestedSHA, err
}
