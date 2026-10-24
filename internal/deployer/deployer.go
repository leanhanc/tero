// Package deployer is the agent's pipeline for one deploy: fetch the
// branch's latest commit, build it, start it next to the live release (or in
// its place, when the app's setting says so), check it answers, and make it
// live. A failed deploy always leaves the previous release serving.
package deployer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/leanhanc/tero/internal/apps"
	"github.com/leanhanc/tero/internal/build"
	"github.com/leanhanc/tero/internal/containers"
	"github.com/leanhanc/tero/internal/deploylog"
	"github.com/leanhanc/tero/internal/deploys"
	"github.com/leanhanc/tero/internal/source"
)

const (
	startTimeout     = 60 * time.Second
	lastOutputLines  = 50
	resumeOldTimeout = 60 * time.Second
	// routeSwitchTimeout bounds the wait for the service to reload its proxy,
	// which it checks for every second.
	routeSwitchTimeout = 15 * time.Second
	// cleanupTimeout bounds discarding a failed release, resuming the old
	// one and removing a replaced one, so a stuck podman can't keep the
	// deploy from ending.
	cleanupTimeout    = 2 * time.Minute
	internalErrorNote = "Tero hit an internal error during this deploy. Redeploy to try again"
)

// WorkDir holds checkouts and image tarballs while a deploy builds.
const WorkDir = "/var/lib/tero/builds"

// Pipeline runs deploys for the agent.
type Pipeline struct {
	DB *sql.DB
	// LogDir holds build logs, WorkDir checkouts and image tarballs.
	LogDir  string
	WorkDir string
	Now     func() time.Time
}

// app is an app as the pipeline needs it. Every field is checked again
// here: the database is written by the service, which the agent doesn't
// trust.
type app struct {
	ID            int64
	Name          string
	Repository    source.Repository
	Branch        string
	RootDirectory string
	RunTogether   bool
}

// Run takes a claimed deploy to live or failed. Failures the admin can act
// on are recorded on the deploy and return nil; other errors are returned
// for the agent to record.
func (p Pipeline) Run(ctx context.Context, deploy deploys.Deploy) error {
	log, err := deploylog.Create(p.LogDir, deploy.ID)
	if err != nil {
		return err
	}
	defer log.Close()

	deployApp, err := p.loadApp(ctx, deploy.AppID)
	if err != nil {
		fmt.Fprintf(log, "\n%s\n", internalErrorNote)
		return err
	}

	image, err := p.buildImage(ctx, deploy, deployApp, log)
	if err != nil {
		return p.recordFailure(ctx, deploy.ID, deploys.Building, err, log)
	}

	if err := deploys.MarkStarting(ctx, p.DB, deploy.ID); err != nil {
		return err
	}

	err = p.release(ctx, deploy, deployApp, image, log)
	return p.recordFailure(ctx, deploy.ID, deploys.Starting, err, log)
}

// buildImage fetches the commit into a fresh checkout and builds it. The
// checkout is removed afterwards.
func (p Pipeline) buildImage(ctx context.Context, deploy deploys.Deploy, deployApp app, log io.Writer) (string, error) {
	checkoutDir := filepath.Join(p.WorkDir, "deploy-"+strconv.FormatInt(deploy.ID, 10))
	if err := os.RemoveAll(checkoutDir); err != nil {
		return "", err
	}
	defer os.RemoveAll(checkoutDir)

	fmt.Fprintf(log, "==> Fetching %s (%s)\n", deployApp.Repository.URL(), deployApp.Branch)
	commit, err := source.Clone(ctx, deployApp.Repository, deployApp.Branch, checkoutDir)
	if err != nil {
		return "", asFailure(err)
	}
	if err := deploys.RecordCommit(ctx, p.DB, deploy.ID, commit.SHA, commit.Message); err != nil {
		return "", err
	}
	fmt.Fprintf(log, "Commit %s: %s\n", commit.SHA[:min(12, len(commit.SHA))], commit.Message)

	sourceDir, err := source.BuildDirectory(checkoutDir, deployApp.RootDirectory)
	if err != nil {
		return "", asFailure(err)
	}

	image, err := build.Build(ctx, build.Request{
		AppName:   deployApp.Name,
		DeployID:  deploy.ID,
		SourceDir: sourceDir,
		WorkDir:   p.WorkDir,
		Log:       log,
	})
	if err != nil {
		return "", err
	}

	return image, deploys.RecordImage(ctx, p.DB, deploy.ID, image)
}

// release starts the new container and makes it live once it answers.
func (p Pipeline) release(ctx context.Context, deploy deploys.Deploy, deployApp app, image string, log io.Writer) error {
	live, hasLive, err := deploys.LiveRelease(ctx, p.DB, deployApp.ID)
	if err != nil {
		return err
	}

	shouldStopOldFirst := hasLive && !deployApp.RunTogether
	if shouldStopOldFirst {
		fmt.Fprintln(log, "==> Stopping the live release first, as this app's settings ask")
		if err := containers.Stop(ctx, live.Container); err != nil {
			return err
		}
	}

	fmt.Fprintln(log, "==> Starting the app")
	container, hostPort, startErr := containers.Start(ctx, deployApp.Name, deploy.ID, image)
	if startErr == nil {
		startErr = deploys.RecordContainer(ctx, p.DB, deploy.ID, container, hostPort)
	}
	if startErr == nil {
		startErr = containers.WaitReady(ctx, container, hostPort, startTimeout)
	}

	if startErr != nil {
		p.discardNewRelease(ctx, container, image, log)
		if shouldStopOldFirst {
			p.resumeOldRelease(ctx, live, log)
		}
		return startErr
	}

	if err := deploys.Promote(ctx, p.DB, deploy.ID, p.Now()); err != nil {
		return err
	}
	fmt.Fprintln(log, "==> Live")

	if hasLive {
		p.retireOldRelease(ctx, deployApp.Name, hostPort, live, log)
	}

	return nil
}

// retireOldRelease removes the previous release once the proxy serves the
// new one. The deploy is already live, so nothing here can fail it: problems
// go to the log, and a container left behind is removed when the agent next
// starts.
func (p Pipeline) retireOldRelease(ctx context.Context, appName string, hostPort int, live deploys.Release, log io.Writer) {
	isServed, err := deploys.WaitUntilServed(ctx, p.DB, appName, hostPort, routeSwitchTimeout)
	// The proxy may still send traffic to the old release, so a stopping
	// agent leaves it running.
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		fmt.Fprintf(log, "Couldn't check that the proxy switched to the new release: %v\n", err)
	} else if !isServed {
		fmt.Fprintln(log, "The proxy hadn't switched to the new release yet when the old one was stopped")
	}

	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	if err := containers.Remove(cleanupCtx, live.Container, live.Image); err != nil {
		fmt.Fprintf(log, "Couldn't remove the previous release: %v\n", err)
	}
}

func (p Pipeline) discardNewRelease(ctx context.Context, container, image string, log io.Writer) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	if container != "" {
		if output := containers.LastOutput(cleanupCtx, container, lastOutputLines); output != "" {
			fmt.Fprintf(log, "\n==> Your app's last output\n%s\n", output)
		}
	}

	_ = containers.Remove(cleanupCtx, container, image)
}

func (p Pipeline) resumeOldRelease(ctx context.Context, live deploys.Release, log io.Writer) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	fmt.Fprintln(log, "==> Starting the previous release again")

	err := containers.Resume(cleanupCtx, live.Container)
	if err == nil {
		err = containers.WaitReady(cleanupCtx, live.Container, live.HostPort, resumeOldTimeout)
	}
	if err != nil {
		fmt.Fprintf(log, "The previous release didn't come back: %v\n", err)
	}
}

// recordFailure fails the deploy at step with a reason the admin can act
// on. Errors that aren't the admin's to fix are returned for the agent.
func (p Pipeline) recordFailure(ctx context.Context, deployID int64, step deploys.State, err error, log io.Writer) error {
	if err == nil {
		return nil
	}
	// The agent is stopping; the deploy is marked interrupted when it starts
	// again.
	if ctx.Err() != nil {
		return err
	}

	reason, isActionable := actionableReason(err)
	if !isActionable {
		fmt.Fprintf(log, "\n%s\n", internalErrorNote)
		return err
	}

	fmt.Fprintf(log, "\n%s\n", reason)
	return deploys.Fail(context.WithoutCancel(ctx), p.DB, deployID, step, reason, p.Now())
}

func actionableReason(err error) (string, bool) {
	var failure build.Failure
	if errors.As(err, &failure) {
		return failure.Reason, true
	}

	var exited containers.ExitedError
	if errors.As(err, &exited) {
		return exited.Error(), true
	}
	if errors.Is(err, containers.ErrStartTimedOut) {
		return containers.ErrStartTimedOut.Error(), true
	}
	if errors.Is(err, containers.ErrNeverAnswered) {
		return err.Error(), true
	}

	return "", false
}

// asFailure marks the source package's errors the admin can fix.
func asFailure(err error) error {
	isActionable := errors.Is(err, source.ErrRepositoryNotFound) || errors.Is(err, source.ErrBranchNotFound) ||
		errors.Is(err, source.ErrFolderNotFound)
	if isActionable {
		return build.Failure{Reason: err.Error()}
	}

	return err
}

func (p Pipeline) loadApp(ctx context.Context, appID int64) (app, error) {
	var loaded app
	var repositoryURL string
	err := p.DB.QueryRowContext(ctx,
		"SELECT id, name, repository, branch, root_directory, run_together FROM apps WHERE id = ?", appID).
		Scan(&loaded.ID, &loaded.Name, &repositoryURL, &loaded.Branch, &loaded.RootDirectory, &loaded.RunTogether)
	if err != nil {
		return app{}, fmt.Errorf("load app %d: %w", appID, err)
	}

	if err := apps.CheckName(loaded.Name); err != nil {
		return app{}, fmt.Errorf("app %d has an invalid name %q", appID, loaded.Name)
	}
	repository, err := source.ParseRepository(repositoryURL)
	if err != nil {
		return app{}, fmt.Errorf("app %s has an invalid repository %q", loaded.Name, repositoryURL)
	}
	if err := source.CheckBranchName(loaded.Branch); err != nil {
		return app{}, fmt.Errorf("app %s has an invalid branch %q", loaded.Name, loaded.Branch)
	}
	loaded.Repository = repository

	return loaded, nil
}
