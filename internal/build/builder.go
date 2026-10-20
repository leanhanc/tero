package build

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/secrets/secretsprovider"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/railwayapp/railpack/buildkit"
	"github.com/railwayapp/railpack/core/plan"
	"github.com/tonistiigi/fsutil"

	"github.com/leanhanc/tero/internal/podman"
)

const buildTimeout = 30 * time.Minute

// Request is one image to build.
type Request struct {
	AppName  string
	DeployID int64
	// SourceDir is the app's root directory inside the checkout.
	SourceDir string
	// WorkDir holds the image tarball between the build and `podman load`.
	WorkDir string
	Log     io.Writer
}

// Build plans and builds the app and loads the image into tero's Podman
// store, returning the image name. Errors the admin can act on are Failures.
func Build(ctx context.Context, req Request) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()

	fmt.Fprintln(req.Log, "==> Planning the build with Railpack")
	buildPlan, err := planBuild(req.SourceDir, req.Log)
	if err != nil {
		return "", err
	}

	fmt.Fprintln(req.Log, "==> Building the image")
	imageName := fmt.Sprintf("localhost/tero/%s:deploy-%d", req.AppName, req.DeployID)
	tarballPath := filepath.Join(req.WorkDir, fmt.Sprintf("deploy-%d.tar", req.DeployID))
	defer os.Remove(tarballPath)

	if err := buildTarball(ctx, req, buildPlan, imageName, tarballPath); err != nil {
		return "", err
	}

	if _, err := podman.Run(ctx, "load", "--input", tarballPath); err != nil {
		return "", err
	}

	return imageName, nil
}

// buildTarball runs the build in a throwaway BuildKit daemon, which is
// removed whatever the outcome.
func buildTarball(ctx context.Context, req Request, buildPlan *plan.BuildPlan, imageName, tarballPath string) error {
	buildkitDaemon, err := startDaemon(ctx, req.DeployID)
	if err != nil {
		return explainContextError(ctx, err)
	}
	defer buildkitDaemon.stop()

	solveErr := solve(ctx, buildkitDaemon.client, req, buildPlan, imageName, tarballPath)
	if solveErr == nil {
		return nil
	}
	// The agent is stopping; the deploy is marked interrupted when it starts
	// again.
	if errors.Is(ctx.Err(), context.Canceled) {
		return ctx.Err()
	}

	if buildkitDaemon.hitMemoryLimit(context.WithoutCancel(ctx)) {
		return errOutOfMemory
	}
	if explained := explainContextError(ctx, solveErr); explained != solveErr {
		return explained
	}

	fmt.Fprintf(req.Log, "\n%v\n", solveErr)
	return errBuildFailed
}

// solve converts the Railpack plan to LLB for this host's platform and runs
// it, writing a docker-archive tarball that `podman load` understands.
// Railpack's own BuildWithBuildkitClient isn't used: it pipes into `docker
// load` and always marshals for linux/amd64.
func solve(ctx context.Context, buildkitClient *client.Client, req Request, buildPlan *plan.BuildPlan, imageName, tarballPath string) error {
	platform, err := buildkit.ParsePlatformWithDefaults("")
	if err != nil {
		return err
	}

	state, image, err := buildkit.ConvertPlanToLLB(buildPlan, buildkit.ConvertPlanOptions{BuildPlatform: platform})
	if err != nil {
		return fmt.Errorf("convert the plan to LLB: %w", err)
	}
	imageConfig, err := json.Marshal(image)
	if err != nil {
		return err
	}
	definition, err := state.Marshal(ctx, llb.Platform(platform))
	if err != nil {
		return fmt.Errorf("marshal LLB: %w", err)
	}

	sourceFS, err := fsutil.NewFS(req.SourceDir)
	if err != nil {
		return err
	}
	tarball, err := os.OpenFile(tarballPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer tarball.Close()

	solveOpt := client.SolveOpt{
		LocalMounts: map[string]fsutil.FS{"context": sourceFS},
		// Railpack plans mount secrets by name; apps have none yet.
		Session: []session.Attachable{secretsprovider.FromMap(map[string][]byte{})},
		Exports: []client.ExportEntry{{
			Type:   client.ExporterDocker,
			Attrs:  map[string]string{"name": imageName, "containerimage.config": string(imageConfig)},
			Output: func(map[string]string) (io.WriteCloser, error) { return tarball, nil },
		}},
	}

	return solveWithProgress(ctx, buildkitClient, definition, solveOpt, req.Log)
}

func solveWithProgress(ctx context.Context, buildkitClient *client.Client, definition *llb.Definition, solveOpt client.SolveOpt, log io.Writer) error {
	statusCh := make(chan *client.SolveStatus)
	progressDone := make(chan error, 1)

	go func() {
		display, err := progressui.NewDisplay(log, progressui.PlainMode)
		if err != nil {
			for range statusCh {
			}
			progressDone <- err
			return
		}
		_, err = display.UpdateFrom(context.WithoutCancel(ctx), statusCh)
		progressDone <- err
	}()

	_, solveErr := buildkitClient.Solve(ctx, definition, solveOpt, statusCh)
	<-progressDone

	return solveErr
}

func explainContextError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errTimedOut
	}

	return err
}
