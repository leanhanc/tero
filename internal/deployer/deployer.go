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
	"github.com/leanhanc/tero/internal/githubapp"
	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/source"
	"github.com/leanhanc/tero/internal/variables"
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

// WorkDir holds checkouts and image tarballs while a deploy builds, in the
// agent's home, out of the service's reach.
const WorkDir = "/var/lib/tero-agent/builds"

// Pipeline runs deploys for the agent.
type Pipeline struct {
	DB *sql.DB
	// LogDir holds build logs, WorkDir checkouts and image tarballs.
	LogDir  string
	WorkDir string
	// Tokens mints the token a repository the GitHub App covers is fetched
	// with. Without it, only public repositories outside the App deploy.
	Tokens RepositoryTokens
	// Variables gives the agent the values of apps that have them.
	Variables variables.Releaser
	Now       func() time.Time
}

// RepositoryTokens mints a token that reads one covered repository. revoke
// must be called once the fetch ends.
type RepositoryTokens interface {
	RepositoryToken(ctx context.Context, repositoryID int64) (token source.Token, revoke func(), err error)
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
	// GitHubRepositoryID is set when the GitHub App covers the repository.
	GitHubRepositoryID int64
	HasVariables       bool
	// Source is the row as stored, compared exactly with the approved source
	// before anything is fetched.
	Source keys.AppSource
}

// Run takes a claimed deploy to live or failed. Failures the admin can act
// on are recorded on the deploy and return nil; other errors are returned
// for the agent to record.
func (p Pipeline) Run(ctx context.Context, deploy deploys.Deploy) error {
	logFile, err := deploylog.Create(p.LogDir, deploy.ID)
	if err != nil {
		return err
	}
	defer logFile.Close()
	log := deploylog.NewMasker(logFile)
	defer log.Close()

	deployApp, err := p.loadApp(ctx, deploy.AppID)
	if err != nil {
		fmt.Fprintf(log, "\n%s\n", internalErrorNote)
		return err
	}

	live, hasLive, err := deploys.LiveRelease(ctx, p.DB, deployApp.ID)
	if err != nil {
		return err
	}
	// The row is written where the dashboard can reach it: a container or
	// image the agent didn't make for this app is never stopped, run or removed.
	if hasLive && !containers.IsReleaseOf(deployApp.Name, live.DeployID, live.Container, live.Image) {
		fmt.Fprintf(log, "\n%s\n", internalErrorNote)
		return fmt.Errorf("app %s's live release names container %q and image %q, which the agent didn't make for it", deployApp.Name, live.Container, live.Image)
	}

	isRestart, err := p.canRestart(ctx, deploy, deployApp, live, hasLive)
	if err != nil {
		return err
	}
	image := live.Image
	if isRestart {
		fmt.Fprintln(log, "==> Restarting the live image with the saved variables. Nothing to fetch or build")
		err = deploys.ReuseRelease(ctx, p.DB, deploy.ID, live.DeployID)
	} else {
		image, err = p.buildImage(ctx, deploy, deployApp, log)
	}
	if err != nil {
		return p.recordFailure(ctx, deploy.ID, deploys.Building, err, log)
	}

	if err := deploys.MarkStarting(ctx, p.DB, deploy.ID); err != nil {
		return err
	}

	err = p.release(ctx, deploy, deployApp, image, isRestart, live, hasLive, log)
	return p.recordFailure(ctx, deploy.ID, deploys.Starting, err, log)
}

// canRestart reports whether the deploy can restart the live image rather than
// build. A deploy queued to restart builds when there is no live image, or when
// that image came from another source, such as before a rename was approved,
// since its values are only released for the approved source. Such a deploy
// then counts as a build.
func (p Pipeline) canRestart(ctx context.Context, deploy deploys.Deploy, deployApp app, live deploys.Release, hasLive bool) (bool, error) {
	if deploy.NeedsBuild {
		return false, nil
	}

	isSameSource := false
	if hasLive {
		imageSource, err := containers.ImageSource(ctx, deployApp.Name, live.Image)
		if err != nil {
			return false, err
		}
		isSameSource = imageSource == deployApp.Source
	}
	if isSameSource {
		return true, nil
	}

	return false, deploys.MarkBuilt(ctx, p.DB, deploy.ID)
}

// buildImage fetches the commit into a fresh checkout and builds it. The
// checkout is removed afterwards.
func (p Pipeline) buildImage(ctx context.Context, deploy deploys.Deploy, deployApp app, log *deploylog.Masker) (string, error) {
	// Asked first, so a source that isn't approved does no work at all.
	buildValues, err := p.releaseForBuild(ctx, deploy, deployApp, log)
	if err != nil {
		return "", err
	}

	checkoutDir := filepath.Join(p.WorkDir, "deploy-"+strconv.FormatInt(deploy.ID, 10))
	if err := os.RemoveAll(checkoutDir); err != nil {
		return "", err
	}
	defer os.RemoveAll(checkoutDir)

	fmt.Fprintf(log, "==> Fetching %s (%s)\n", deployApp.Repository.URL(), deployApp.Branch)
	commit, err := p.fetch(ctx, deployApp, checkoutDir)
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
		Source:    deployApp.Source,
		Variables: buildValues.Map(),
	})
	if err != nil {
		return "", err
	}

	return image, deploys.RecordImage(ctx, p.DB, deploy.ID, image)
}

// releaseForBuild gets the values this deploy's build uses.
func (p Pipeline) releaseForBuild(ctx context.Context, deploy deploys.Deploy, deployApp app, log *deploylog.Masker) (variables.Released, error) {
	return p.releaseValues(deployApp, log, func(releaser variables.Releaser) (variables.Released, error) {
		return variables.ForBuild(ctx, releaser, deployApp.Name, deployApp.Source, deploy.ID)
	})
}

// releaseValues asks the key service for an app's values. It always asks,
// since has_variables is written where the dashboard can reach it: turning it
// off must not skip the source check. The flag only decides whether an app may
// go ahead without values while the key service can't answer.
func (p Pipeline) releaseValues(deployApp app, log *deploylog.Masker, ask func(variables.Releaser) (variables.Released, error)) (variables.Released, error) {
	if p.Variables == nil {
		if deployApp.HasVariables {
			return nil, errNoKeyService
		}
		return nil, nil
	}

	released, err := ask(p.Variables)
	if errors.Is(err, keys.ErrUnavailable) && !deployApp.HasVariables {
		return nil, nil
	}
	if err != nil {
		return nil, asVariablesFailure(err)
	}
	log.Add(released.Masked())

	return released, nil
}

// releaseForRun gets the values the container of image runs with.
func (p Pipeline) releaseForRun(ctx context.Context, deploy deploys.Deploy, deployApp app, image string, log *deploylog.Masker) (map[string]string, error) {
	released, err := p.releaseValues(deployApp, log, func(releaser variables.Releaser) (variables.Released, error) {
		return variables.ForImage(ctx, releaser, deployApp.Name, image, deploy.ID)
	})
	if err != nil {
		return nil, err
	}

	return released.Map(), nil
}

// release starts the new container and makes it live once it answers. A
// restart shares its image with the live release, so that image stays when
// either container goes.
func (p Pipeline) release(ctx context.Context, deploy deploys.Deploy, deployApp app, image string, isRestart bool, live deploys.Release, hasLive bool, log *deploylog.Masker) error {
	// Before the old release stops: a refusal must leave it serving.
	values, err := p.releaseForRun(ctx, deploy, deployApp, image, log)
	if err != nil {
		if !isRestart {
			p.discardNewRelease(ctx, "", image, log)
		}
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
	container, hostPort, startErr := containers.Start(ctx, deployApp.Name, deploy.ID, image, values)
	if startErr == nil {
		startErr = deploys.RecordContainer(ctx, p.DB, deploy.ID, container, hostPort)
	}
	if startErr == nil {
		startErr = containers.WaitReady(ctx, container, hostPort, startTimeout)
	}

	if startErr != nil {
		discardedImage := image
		if isRestart {
			discardedImage = ""
		}
		p.discardNewRelease(ctx, container, discardedImage, log)
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
		if isRestart {
			live.Image = ""
		}
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
func (p Pipeline) recordFailure(ctx context.Context, deployID int64, step deploys.State, err error, log *deploylog.Masker) error {
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

	reason = log.Mask(reason)
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
// fetch clones the branch head. A repository the GitHub App covers is read
// with a token for that repository alone, revoked as soon as the clone ends.
func (p Pipeline) fetch(ctx context.Context, deployApp app, checkoutDir string) (source.Commit, error) {
	if deployApp.GitHubRepositoryID == 0 {
		return source.Clone(ctx, deployApp.Repository, "", deployApp.Branch, checkoutDir)
	}
	if p.Tokens == nil {
		return source.Commit{}, githubapp.ErrNotInstalled
	}

	token, revoke, err := p.Tokens.RepositoryToken(ctx, deployApp.GitHubRepositoryID)
	if err != nil {
		return source.Commit{}, err
	}
	defer revoke()

	return source.Clone(ctx, deployApp.Repository, token, deployApp.Branch, checkoutDir)
}

// errNoKeyService means the app has variables and this agent can't ask for
// them.
var errNoKeyService = errors.New("the agent has no key service to ask for this app's variables")

// asVariablesFailure marks the key service's answers the admin can act on.
func asVariablesFailure(err error) error {
	switch {
	case errors.Is(err, keys.ErrSourceNotApproved):
		return build.Failure{Reason: "This app's source changed since its variables were saved. Approve the new source on the app's page to deploy with them"}
	case errors.Is(err, keys.ErrKeyNotLoaded):
		return build.Failure{Reason: "Some of this app's variables were sealed with a master key that was lost. Set them again on the app's page"}
	case errors.Is(err, keys.ErrUnavailable):
		return build.Failure{Reason: "The key service isn't running, so this app's variables aren't available. Check `systemctl status tero-keys` on the server and redeploy"}
	default:
		return err
	}
}

func asFailure(err error) error {
	isActionable := errors.Is(err, source.ErrRepositoryNotFound) || errors.Is(err, source.ErrBranchNotFound) ||
		errors.Is(err, source.ErrFolderNotFound) || errors.Is(err, githubapp.ErrNotInstalled)
	if isActionable {
		return build.Failure{Reason: err.Error()}
	}

	return err
}

func (p Pipeline) loadApp(ctx context.Context, appID int64) (app, error) {
	var loaded app
	var repositoryURL string
	err := p.DB.QueryRowContext(ctx,
		"SELECT id, name, repository, branch, root_directory, run_together, github_repository_id, has_variables FROM apps WHERE id = ?", appID).
		Scan(&loaded.ID, &loaded.Name, &repositoryURL, &loaded.Branch, &loaded.RootDirectory, &loaded.RunTogether, &loaded.GitHubRepositoryID, &loaded.HasVariables)
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
	loaded.Source = keys.AppSource{Repository: repositoryURL, Branch: loaded.Branch, RootDirectory: loaded.RootDirectory}

	return loaded, nil
}
