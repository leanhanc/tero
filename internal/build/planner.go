package build

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/railwayapp/railpack/core"
	"github.com/railwayapp/railpack/core/app"
	"github.com/railwayapp/railpack/core/mise"
	"github.com/railwayapp/railpack/core/plan"
)

// noStartCommandLog is the start of the message Railpack logs when it finds
// no way to start the app.
const noStartCommandLog = "No start command detected"

// planBuild asks Railpack for a build plan of the app in sourceDir and
// writes Railpack's log lines to the build log.
func planBuild(sourceDir string, log io.Writer) (*plan.BuildPlan, error) {
	if err := ensureMiseDirectory(); err != nil {
		return nil, err
	}

	source, err := app.NewApp(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("open the app for Railpack: %w", err)
	}

	result, err := core.GenerateBuildPlan(source, app.NewEnvironment(nil), &core.GenerateBuildPlanOptions{
		ErrorMissingStartCommand: true,
	})
	if err != nil {
		return nil, fmt.Errorf("plan the build: %w", err)
	}

	messages := make([]string, 0, len(result.Logs))
	for _, entry := range result.Logs {
		messages = append(messages, entry.Msg)
		fmt.Fprintf(log, "railpack: %s\n", entry.Msg)
	}

	if !result.Success {
		return nil, describePlanFailure(messages)
	}

	return result.Plan, nil
}

func describePlanFailure(messages []string) error {
	for _, message := range messages {
		if strings.HasPrefix(message, noStartCommandLog) {
			return errNoStartCommand
		}
	}

	reason := "Railpack couldn't work out how to build this app"
	if len(messages) > 0 {
		reason += ": " + strings.Join(messages, " ")
	}

	return Failure{Reason: reason}
}

// ensureMiseDirectory makes sure the fixed, host-wide directory Railpack
// downloads mise into belongs to the agent. Railpack runs the binary it
// finds there, so a directory planted by another user must never be used.
func ensureMiseDirectory() error {
	parent := strings.TrimSuffix(mise.InstallDir, "/mise")

	info, err := os.Lstat(parent)
	if errors.Is(err, os.ErrNotExist) {
		return os.Mkdir(parent, 0o700)
	}
	if err != nil {
		return err
	}

	stat, hasOwner := info.Sys().(*syscall.Stat_t)
	isOwnDirectory := info.IsDir() && info.Mode()&os.ModeSymlink == 0 && hasOwner && int(stat.Uid) == os.Getuid()
	if !isOwnDirectory {
		return fmt.Errorf("%s exists but doesn't belong to the agent's user; remove it and redeploy", parent)
	}

	return os.Chmod(parent, 0o700)
}
