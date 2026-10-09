// Package sysexec runs host commands for init. Steps go through Runner so
// unit tests can record commands instead of running them.
package sysexec

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Runner runs a command and returns its combined output.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (string, error)
}

// systemDirs is the only PATH commands are found in and run with. The
// caller's PATH and environment are ignored, so `sudo -E` or a user-writable
// directory in PATH can't swap in a different apt-get or sshd.
var systemDirs = []string{"/usr/sbin", "/usr/bin", "/sbin", "/bin"}

// Host runs commands on the real host with a fixed environment: a system
// PATH plus Env.
type Host struct {
	Env []string
}

// Run runs name with args. On failure the error includes the command and its
// output, so init can show the user what went wrong.
func (h Host) Run(ctx context.Context, name string, args ...string) (string, error) {
	executable, err := findSystemCommand(name)
	if err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Env = append([]string{"PATH=" + strings.Join(systemDirs, ":")}, h.Env...)
	// Commands get their own process group, so Ctrl-C in the terminal reaches
	// only init, which then stops them with SIGTERM rather than SIGKILL.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 30 * time.Second

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	err = cmd.Run()
	trimmedOutput := strings.TrimSpace(output.String())
	if err != nil {
		commandLine := strings.Join(append([]string{name}, args...), " ")
		return trimmedOutput, fmt.Errorf("`%s` failed (%w)\n%s", commandLine, err, trimmedOutput)
	}

	return trimmedOutput, nil
}

// findSystemCommand resolves name in systemDirs only.
func findSystemCommand(name string) (string, error) {
	if filepath.IsAbs(name) {
		return name, nil
	}

	for _, dir := range systemDirs {
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		isExecutable := err == nil && !info.IsDir() && info.Mode()&0o111 != 0
		if isExecutable {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("`%s` is not installed in %s", name, strings.Join(systemDirs, ", "))
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
