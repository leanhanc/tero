package build

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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

// PlanCommand is the hidden subcommand of the tero binary that plans a build.
// Railpack opens whatever paths the repository leads it to, through symlinks
// or "../" in its config files, so the agent never plans in its own process:
// the child confines itself to the app's folder first, and other apps'
// variables, logs and checkouts stay out of its reach (OWASP ASVS
// v5.0.0-5.3.2).
const PlanCommand = "plan-build"

type planRequest struct {
	SourceDir string            `json:"sourceDir"`
	Variables map[string]string `json:"variables"`
}

type planResponse struct {
	Plan     *plan.BuildPlan `json:"plan,omitempty"`
	Messages []string        `json:"messages"`
	Success  bool            `json:"success"`
	Error    string          `json:"error,omitempty"`
}

// planBuild asks Railpack, in a confined child process, for a build plan of
// the app in sourceDir and writes Railpack's log lines to the build log.
// Railpack lists variables in the plan by name only, as secrets, and reads
// RAILPACK_* ones as its own settings.
func planBuild(ctx context.Context, sourceDir string, variables map[string]string, log io.Writer) (*plan.BuildPlan, error) {
	if err := ensureMiseDirectory(); err != nil {
		return nil, err
	}

	response, err := runPlanCommand(ctx, planRequest{SourceDir: sourceDir, Variables: variables})
	if err != nil {
		return nil, err
	}

	for _, message := range response.Messages {
		fmt.Fprintf(log, "railpack: %s\n", message)
	}
	if response.Error != "" {
		return nil, fmt.Errorf("plan the build: %s", response.Error)
	}
	if !response.Success {
		return nil, describePlanFailure(response.Messages)
	}

	return response.Plan, nil
}

// maxPlannerErrorBytes keeps the end of the child's error output, where the
// reason it stopped is.
const maxPlannerErrorBytes = 2048

func runPlanCommand(ctx context.Context, request planRequest) (planResponse, error) {
	executable, err := os.Executable()
	if err != nil {
		return planResponse{}, err
	}
	encodedRequest, err := json.Marshal(request)
	if err != nil {
		return planResponse{}, err
	}

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, executable, PlanCommand)
	cmd.Stdin = bytes.NewReader(encodedRequest)
	cmd.Stderr = &stderr
	// Nothing of the agent's own environment reaches the child. Railpack
	// downloads mise through a temporary directory, and the child can only
	// write inside the mise directory.
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + miseRoot(), "TMPDIR=" + miseRoot()}

	output, err := cmd.Output()
	if err != nil {
		errorOutput := stderr.Bytes()
		errorOutput = errorOutput[max(0, len(errorOutput)-maxPlannerErrorBytes):]
		return planResponse{}, fmt.Errorf("plan the build: %w: %s", err, strings.TrimSpace(string(errorOutput)))
	}

	var response planResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return planResponse{}, fmt.Errorf("read the build plan: %w", err)
	}

	return response, nil
}

// RunPlanCommand is `tero plan-build`. It reads a planRequest from in,
// confines this process to the app's folder before Railpack reads anything,
// and writes the planResponse to out.
func RunPlanCommand(in io.Reader, out io.Writer) error {
	var request planRequest
	if err := json.NewDecoder(in).Decode(&request); err != nil {
		return fmt.Errorf("read the plan request: %w", err)
	}
	if err := confineToSource(request.SourceDir); err != nil {
		return fmt.Errorf("confine the planner to the app's folder: %w", err)
	}

	return json.NewEncoder(out).Encode(generatePlan(request))
}

func generatePlan(request planRequest) planResponse {
	source, err := app.NewApp(request.SourceDir)
	if err != nil {
		return planResponse{Error: "open the app for Railpack: " + err.Error()}
	}

	variables := request.Variables
	result, err := core.GenerateBuildPlan(source, app.NewEnvironment(&variables), &core.GenerateBuildPlanOptions{
		ErrorMissingStartCommand: true,
	})
	if err != nil {
		return planResponse{Error: err.Error()}
	}

	messages := make([]string, 0, len(result.Logs))
	for _, entry := range result.Logs {
		messages = append(messages, entry.Msg)
	}

	return planResponse{Plan: result.Plan, Messages: messages, Success: result.Success}
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
	parent := miseRoot()

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

// miseRoot is the directory Railpack keeps mise in, and the only one the
// planner may write to.
func miseRoot() string {
	return strings.TrimSuffix(mise.InstallDir, "/mise")
}
