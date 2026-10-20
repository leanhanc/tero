package agent

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/leanhanc/tero/internal/containers"
	"github.com/leanhanc/tero/internal/deploys"
	"github.com/leanhanc/tero/internal/podman"
)

// keepLiveAppsRunning starts the container of every live release that isn't
// running: after the app's process exits, after a reboot, or if the
// container was removed by hand.
func keepLiveAppsRunning(ctx context.Context, db *sql.DB) error {
	releases, err := deploys.SettledLiveReleases(ctx, db)
	if err != nil {
		return err
	}

	for _, release := range releases {
		if containers.IsRunning(ctx, release.Container) {
			continue
		}

		slog.Info("starting a live app that wasn't running", "app", release.AppName, "container", release.Container)
		if err := restartRelease(ctx, db, release); err != nil {
			slog.Error("start live app", "app", release.AppName, "error", err)
		}
	}

	return nil
}

// restartRelease resumes the release's container, or recreates it from the
// release's image when the container is gone.
func restartRelease(ctx context.Context, db *sql.DB, release deploys.Release) error {
	if err := containers.Resume(ctx, release.Container); err == nil {
		return nil
	}

	container, hostPort, err := containers.Start(ctx, release.AppName, release.DeployID, release.Image)
	if err != nil {
		return err
	}

	return deploys.RecordContainer(ctx, db, release.DeployID, container, hostPort)
}

// removeLeftovers removes BuildKit daemons and app containers that a
// previous agent process left behind when it stopped mid-deploy. Only the
// containers of live releases are kept.
func removeLeftovers(ctx context.Context, db *sql.DB) error {
	releases, err := deploys.LiveReleases(ctx, db)
	if err != nil {
		return err
	}
	liveDeployIDs := map[string]bool{}
	for _, release := range releases {
		liveDeployIDs[strconv.FormatInt(release.DeployID, 10)] = true
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

	return nil
}
