// Package sysexec runs host commands for init. Steps go through Runner so
// unit tests can record commands instead of running them.
package sysexec

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Runner runs a command and returns its combined output.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// Host runs commands on the real host with a fixed extra environment.
type Host struct {
	Env []string
}

// Run runs name with args. On failure the error includes the command and its
// output, so init can show the user what went wrong.
func (h Host) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(cmd.Environ(), h.Env...)

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err := cmd.Run()
	trimmedOutput := strings.TrimSpace(output.String())
	if err != nil {
		commandLine := strings.Join(append([]string{name}, args...), " ")
		return trimmedOutput, fmt.Errorf("`%s` failed (%w)\n%s", commandLine, err, trimmedOutput)
	}

	return trimmedOutput, nil
}

// Recorder is a Runner for tests: it records every command and returns
// canned output keyed by the full command line.
type Recorder struct {
	Commands []string
	Outputs  map[string]string
	Errors   map[string]error
}

// Run records the command line and returns its canned output or error.
func (r *Recorder) Run(_ context.Context, name string, args ...string) (string, error) {
	commandLine := strings.Join(append([]string{name}, args...), " ")
	r.Commands = append(r.Commands, commandLine)

	return r.Outputs[commandLine], r.Errors[commandLine]
}
