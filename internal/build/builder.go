package build

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/client/llb"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/secrets/secretsprovider"
	"github.com/moby/buildkit/util/progress/progressui"
	"github.com/railwayapp/railpack/buildkit"
	"github.com/railwayapp/railpack/core/plan"
	"github.com/tonistiigi/fsutil"

	"github.com/leanhanc/tero/internal/containers"
	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/podman"
)

const buildTimeout = 30 * time.Minute

// ImagePrefix starts the name of every app image Tero builds.
const ImagePrefix = "localhost/tero/"

// Request is one image to build.
type Request struct {
	AppName  string
	DeployID int64
	// SourceDir is the app's root directory inside the checkout.
	SourceDir string
	// WorkDir holds the image tarball between the build and `podman load`.
	WorkDir string
	Log     io.Writer
	// Source is recorded in the image, for the key service's check when the
	// image is run.
	Source keys.AppSource
	// Variables are the values build steps get, as secret mounts, so they
	// stay out of the image's layers.
	Variables map[string]string
}

// Build plans and builds the app and loads the image into tero-agent's
// Podman store, returning the image name. Errors the admin can act on are Failures.
func Build(ctx context.Context, req Request) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()

	fmt.Fprintln(req.Log, "==> Planning the build with Railpack")
	buildPlan, err := planBuild(req.SourceDir, req.Variables, req.Log)
	if err != nil {
		return "", err
	}

	fmt.Fprintln(req.Log, "==> Building the image")
	imageName := fmt.Sprintf("%s%s:deploy-%d", ImagePrefix, req.AppName, req.DeployID)
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
	buildkitDaemon, err := startDaemon(ctx, req.DeployID, req.WorkDir)
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

	// The cache key keeps cache mounts such as ~/.npm apart per app and source,
	// so a build can't plant what another app's, or this app's from another
	// source, installs. Layers are content-addressed and stay shared.
	state, image, err := buildkit.ConvertPlanToLLB(buildPlan, buildkit.ConvertPlanOptions{
		BuildPlatform: platform,
		CacheKey:      cacheKey(req.AppName, req.Source),
		SecretsHash:   hashVariables(req.Variables),
	})
	if err != nil {
		return fmt.Errorf("convert the plan to LLB: %w", err)
	}
	if err := labelSource(image, req.Source); err != nil {
		return err
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
		// Railpack plans mount variables as secrets by name.
		Session: []session.Attachable{secretsprovider.FromMap(secretMounts(req.Variables))},
		Exports: []client.ExportEntry{{
			Type:   client.ExporterDocker,
			Attrs:  map[string]string{"name": imageName, "containerimage.config": string(imageConfig)},
			Output: func(map[string]string) (io.WriteCloser, error) { return tarball, nil },
		}},
	}

	return solveWithProgress(ctx, buildkitClient, definition, solveOpt, req.Log)
}

// labelSource records what the image was built from. The image's own
// configuration is set here, after the build, so a build can't choose it.
func labelSource(image *buildkit.Image, source keys.AppSource) error {
	label, err := json.Marshal(source)
	if err != nil {
		return err
	}
	if image.Config.Labels == nil {
		image.Config.Labels = map[string]string{}
	}
	image.Config.Labels[containers.SourceLabel] = string(label)

	return nil
}

func cacheKey(appName string, source keys.AppSource) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%q %q %q %q", appName, source.Repository, source.Branch, source.RootDirectory))
	return hex.EncodeToString(sum[:8])
}

func secretMounts(variables map[string]string) map[string][]byte {
	mounts := make(map[string][]byte, len(variables))
	for name, value := range variables {
		mounts[name] = []byte(value)
	}

	return mounts
}

// hashVariables changes when any value does, so the steps that use a variable
// run again rather than coming from the cache. Empty with no variables.
func hashVariables(variables map[string]string) string {
	if len(variables) == 0 {
		return ""
	}

	hash := sha256.New()
	for _, name := range slices.Sorted(maps.Keys(variables)) {
		fmt.Fprintf(hash, "%d:%s=%d:%s\n", len(name), name, len(variables[name]), variables[name])
	}

	return hex.EncodeToString(hash.Sum(nil))
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
