// Package agent is `tero agent`, the process that builds and runs apps. It
// runs as tero-agent under that user's systemd user manager, outside any
// service sandbox, because rootless Podman needs the setuid id-mapping
// helpers a sandboxed service may not use. It listens on nothing: the deploy
// queue in the SQLite store is its only input.
package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/leanhanc/tero/internal/build"
	"github.com/leanhanc/tero/internal/deploys"
)

// pollInterval is how often the agent looks for a queued deploy. A deploy
// takes tens of seconds, so a second of latency doesn't show.
const pollInterval = time.Second

// reconcileInterval is how often the agent checks that live apps run.
const reconcileInterval = 10 * time.Second

// Pipeline takes one claimed deploy to live or failed.
type Pipeline interface {
	Run(ctx context.Context, deploy deploys.Deploy) error
}

// Background is work the agent runs alongside deploys until ctx ends, such
// as answering the dashboard's GitHub requests.
type Background interface {
	Run(ctx context.Context)
}

// Options configure the agent. Now defaults to the real clock.
type Options struct {
	DB         *sql.DB
	Pipeline   Pipeline
	Background []Background
	Now        func() time.Time
}

// Run claims and runs queued deploys until ctx is cancelled.
func Run(ctx context.Context, opts Options) error {
	if os.Geteuid() == 0 {
		return errors.New("tero agent must not run as root: it is started for the tero user by systemd")
	}

	now := opts.Now
	if now == nil {
		now = time.Now
	}

	interruptedCount, err := deploys.MarkInterrupted(ctx, opts.DB, now())
	if err != nil {
		return fmt.Errorf("check for interrupted deploys: %w", err)
	}
	if interruptedCount > 0 {
		slog.Info("marked deploys interrupted by a restart as failed", "count", interruptedCount)
	}
	if err := removeLeftovers(ctx, opts.DB); err != nil {
		slog.Error("remove containers left by an interrupted deploy", "error", err)
	}

	go reconcilePeriodically(ctx, opts.DB)
	go pullBuildKit(ctx)
	for _, work := range opts.Background {
		go work.Run(ctx)
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		if err := runNextDeploy(ctx, opts.DB, opts.Pipeline, now); err != nil {
			slog.Error("deploy queue", "error", err)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// reconcilePeriodically runs alongside deploys. It leaves apps with a deploy
// starting alone, and a deploy promotes its release before removing the old
// container, so the two never fight over a container.
func reconcilePeriodically(ctx context.Context, db *sql.DB) {
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()

	for {
		if err := keepLiveAppsRunning(ctx, db); err != nil {
			slog.Error("keep live apps running", "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// pullBuildKit fetches the BuildKit image in the background. A failure only
// means the first deploy pulls it instead.
func pullBuildKit(ctx context.Context) {
	if err := build.PullImage(ctx); err != nil && ctx.Err() == nil {
		slog.Warn("download BuildKit ahead of the first deploy", "error", err)
	}
}

// runNextDeploy runs the oldest queued deploy, if any. A pipeline error that
// the pipeline didn't record itself still fails the deploy, so nothing stays
// building forever.
func runNextDeploy(ctx context.Context, db *sql.DB, pipeline Pipeline, now func() time.Time) error {
	deploy, isClaimed, err := deploys.ClaimNext(ctx, db, now())
	if err != nil || !isClaimed {
		return err
	}

	pipelineErr := pipeline.Run(ctx, deploy)
	if pipelineErr == nil {
		return nil
	}
	// A deploy cut short by the agent stopping is left building or starting,
	// so the next agent marks it interrupted.
	if ctx.Err() != nil {
		return nil
	}

	reason := "Tero hit an internal error during this deploy. Redeploy to try again"
	if err := deploys.FailIfRunning(ctx, db, deploy.ID, reason, now()); err != nil {
		return err
	}

	return fmt.Errorf("deploy %d: %w", deploy.ID, pipelineErr)
}
