package build

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/railwayapp/railpack/core"
	"github.com/railwayapp/railpack/core/app"
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
	pristine, err := ensurePristineMise(ctx)
	if err != nil {
		return nil, err
	}
	if err := preparePlanDirectory(pristine); err != nil {
		return nil, err
	}
	defer removePlanDirectory()

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

	// The planner runs in a systemd scope of its own, killed once it is
	// done, so nothing it started outlives the plan to tamper with the next
	// one. env -i keeps the agent's own environment from the planner, which
	// can only write inside the mise directory.
	scope := fmt.Sprintf("tero-plan-%d.scope", time.Now().UnixNano())
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "systemd-run", "--user", "--scope", "--quiet", "--collect", "--unit="+scope,
		"/usr/bin/env", "-i", "PATH=/usr/bin:/bin", "HOME="+miseRoot(), "TMPDIR="+miseRoot(), executable, PlanCommand)
	cmd.Stdin = bytes.NewReader(encodedRequest)
	cmd.Stderr = &stderr
	cmd.Env = []string{"PATH=/usr/bin:/bin", "XDG_RUNTIME_DIR=" + os.Getenv("XDG_RUNTIME_DIR")}
	cmd.WaitDelay = 10 * time.Second

	output, err := cmd.Output()
	if stopErr := stopPlanScope(scope); stopErr != nil {
		return planResponse{}, stopErr
	}
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

// stopPlanScope kills whatever is left in the planner's scope and checks
// that the scope is gone. A scope with nothing left in it is already gone,
// so a failed kill only matters if the scope is still there.
func stopPlanScope(scope string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_ = exec.CommandContext(ctx, "systemctl", "--user", "kill", "--signal=SIGKILL", scope).Run()
	for {
		output, err := exec.CommandContext(ctx, "systemctl", "--user", "show", "--property=ActiveState", "--value", scope).Output()
		state := strings.TrimSpace(string(output))
		if err == nil && state != "active" && state != "deactivating" {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("stop the build planner's scope %s: it is still %s", scope, state)
		case <-time.After(200 * time.Millisecond):
		}
	}
}
