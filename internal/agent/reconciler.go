package agent

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/leanhanc/tero/internal/build"
	"github.com/leanhanc/tero/internal/containers"
	"github.com/leanhanc/tero/internal/deploys"
	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/podman"
	"github.com/leanhanc/tero/internal/variables"
)

// refusedRetryInterval spaces out asking again for an app whose image's source
// isn't approved, since each refusal is a security event. Nothing changes that
// answer but the admin approving the source.
const refusedRetryInterval = 10 * time.Minute

// reconciler keeps live apps running.
type reconciler struct {
	db        *sql.DB
	variables variables.Releaser
	now       func() time.Time
	// refusedAt is when each release was last refused its values.
	refusedAt map[int64]time.Time
}

func newReconciler(db *sql.DB, releaser variables.Releaser) *reconciler {
	return &reconciler{db: db, variables: releaser, now: time.Now, refusedAt: map[int64]time.Time{}}
}

// keepLiveAppsRunning starts the container of every live release that isn't
// running: after the app's process exits, after a reboot, or if the
// container was removed by hand. A running container published on another
// port than the one recorded, left by an agent stopped mid-restart, has its
// port recorded so the proxy finds it.
func (r *reconciler) keepLiveAppsRunning(ctx context.Context) error {
	// Containers are listed before releases: a deploy promoted in between then
	// shows as a new release whose container looks stopped, which resuming
	// leaves alone, instead of an old release whose container is gone, which
	// would be recreated.
	runningPorts, err := containers.RunningPorts(ctx)
	if err != nil {
		return err
	}
	releases, err := deploys.SettledLiveReleases(ctx, r.db)
	if err != nil {
		return err
	}

	for _, release := range releases {
		hostPort, isRunning := runningPorts[release.Container]
		if !isRunning {
			r.startLiveApp(ctx, release)
			continue
		}

		hasMovedPort := hostPort != 0 && hostPort != release.HostPort
		if hasMovedPort {
			slog.Info("recording a live app's new port", "app", release.AppName, "port", hostPort)
			if err := deploys.RecordContainer(ctx, r.db, release.DeployID, release.Container, hostPort); err != nil {
				slog.Error("record live app's port", "app", release.AppName, "error", err)
			}
		}
	}

	return nil
}

// startLiveApp restarts the release unless a deploy replaced it or started
// for its app since the releases were listed: restarting other apps can take
// a while.
func (r *reconciler) startLiveApp(ctx context.Context, release deploys.Release) {
	isSettled, err := deploys.IsSettledLive(ctx, r.db, release.DeployID)
	if err != nil {
		slog.Error("check live app", "app", release.AppName, "error", err)
		return
	}
	if !isSettled {
		return
	}

	lastRefused, wasRefused := r.refusedAt[release.DeployID]
	if wasRefused && r.now().Sub(lastRefused) < refusedRetryInterval {
		return
	}

	slog.Info("starting a live app that wasn't running", "app", release.AppName, "container", release.Container)
	err = r.restartRelease(ctx, release)
	if err != nil {
		slog.Error("start live app", "app", release.AppName, "error", err)
	}
	if errors.Is(err, keys.ErrSourceNotApproved) {
		r.refusedAt[release.DeployID] = r.now()
	} else {
		delete(r.refusedAt, release.DeployID)
	}

	problem := startProblem(err)
	if err := deploys.RecordStartProblem(context.WithoutCancel(ctx), r.db, release.AppID, problem); err != nil {
		slog.Error("record why a live app can't start", "app", release.AppName, "error", err)
	}
}

// startProblem is what the app's page says while its values can't be had. Any
// other failure isn't the admin's to act on from there.
func startProblem(err error) string {
	switch {
	case errors.Is(err, keys.ErrSourceNotApproved):
		return "This app can't start: its image's source isn't the one approved for its variables. Approve the source and redeploy"
	case errors.Is(err, keys.ErrKeyNotLoaded):
		return "This app can't start: some of its variables were sealed with a master key that was lost. Set them again and redeploy"
	case errors.Is(err, keys.ErrUnavailable):
		return "This app can't start until the key service is running, because its variables are only kept there. Tero keeps trying. Check `systemctl status tero-keys` on the server"
	default:
		return ""
	}
}

// restartRelease resumes the release's container, or recreates it from the
// release's image when the container is gone. An app's variables are only in
// memory, so after a reboot they come from the key service first. An app that
// merely exited still has them, and needs no key service to start.
func (r *reconciler) restartRelease(ctx context.Context, release deploys.Release) error {
	// The row is written where the dashboard can reach it.
	if !containers.IsReleaseOf(release.AppName, release.DeployID, release.Container, release.Image) {
		return fmt.Errorf("the live release names container %q and image %q, which the agent didn't make for app %s",
			release.Container, release.Image, release.AppName)
	}

	var values map[string]string
	hasValues := containers.HasEnvFile(release.Container)
	if !hasValues {
		released, err := r.releaseValues(ctx, release)
		if err != nil {
			return err
		}
		values = released
		if len(values) > 0 {
			if err := containers.WriteEnvFile(release.Container, values); err != nil {
				return err
			}
		}
	}

	if err := containers.Resume(ctx, release.Container); err == nil {
		return nil
	}

	if hasValues {
		released, err := r.releaseValues(ctx, release)
		if err != nil {
			return err
		}
		values = released
	}

	container, hostPort, err := containers.Start(ctx, release.AppName, release.DeployID, release.Image, values)
	if err != nil {
		if container != "" {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
			defer cancel()
			if removeErr := containers.Remove(cleanupCtx, container, ""); removeErr != nil {
				slog.Warn("remove a container that didn't start", "container", container, "error", removeErr)
			}
		}
		return err
	}

	// The container already runs on the new port, so the port is recorded
	// even when the agent is stopping.
	return deploys.RecordContainer(context.WithoutCancel(ctx), r.db, release.DeployID, container, hostPort)
}

// releaseValues always asks the key service, since has_variables can be
// written by the dashboard. The flag only lets an app with no variables start
// while the key service can't answer.
func (r *reconciler) releaseValues(ctx context.Context, release deploys.Release) (map[string]string, error) {
	if r.variables == nil {
		if release.HasVariables {
			return nil, keys.ErrUnavailable
		}
		return nil, nil
	}

	released, err := variables.ForImage(ctx, r.variables, release.AppName, release.Image, release.DeployID)
	if errors.Is(err, keys.ErrUnavailable) && !release.HasVariables {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return released.Map(), nil
}

// removeLeftovers removes BuildKit daemons, app containers and app images
// that a previous agent process left behind when it stopped mid-deploy. Only
// the containers and images of live releases are kept.
func removeLeftovers(ctx context.Context, db *sql.DB) error {
	releases, err := deploys.LiveReleases(ctx, db)
	if err != nil {
		return err
	}
	liveDeployIDs := map[string]bool{}
	liveImages := map[string]bool{}
	liveContainers := map[string]bool{}
	for _, release := range releases {
		liveDeployIDs[strconv.FormatInt(release.DeployID, 10)] = true
		liveImages[release.Image] = true
		liveContainers[release.Container] = true
	}

	buildkitNames, err := podman.Run(ctx, "ps", "--all", "--filter", "name=^tero-buildkit-", "--format", "{{.Names}}")
	if err != nil {
		return err
	}
	for _, name := range strings.Fields(buildkitNames) {
		if err := podman.RemoveContainer(ctx, name); err != nil {
			return err
		}
	}

	appContainers, err := podman.Run(ctx, "ps", "--all", "--filter", "label="+containers.AppLabel,
		"--format", fmt.Sprintf(`{{.Names}} {{index .Labels "%s"}}`, containers.DeployLabel))
	if err != nil {
		return err
	}
	for _, line := range strings.Split(appContainers, "\n") {
		name, deployID, hasDeployID := strings.Cut(strings.TrimSpace(line), " ")
		if !hasDeployID || liveDeployIDs[deployID] {
			continue
		}
		if err := podman.RemoveContainer(ctx, name); err != nil {
			return err
		}
	}

	if err := containers.RemoveEnvFilesExcept(liveContainers); err != nil {
		return err
	}

	return removeLeftoverImages(ctx, liveImages)
}

// removeLeftoverImages removes app images no live release uses, such as the
// previous release's image when the agent stopped before removing it.
func removeLeftoverImages(ctx context.Context, liveImages map[string]bool) error {
	images, err := podman.Run(ctx, "images", "--format", "{{.Repository}}:{{.Tag}}")
	if err != nil {
		return err
	}

	for _, image := range strings.Fields(images) {
		isAppImage := strings.HasPrefix(image, build.ImagePrefix)
		if !isAppImage || liveImages[image] {
			continue
		}
		if _, err := podman.Run(ctx, "rmi", "--ignore", image); err != nil {
			return err
		}
	}

	return nil
}
