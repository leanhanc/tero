// Package containers starts and checks app containers under tero's rootless
// Podman, with the ADR 0001 rules: no capabilities, no privilege gain, a user
// namespace slice of their own, and a network namespace of their own that
// can't reach other apps. Each app's PORT is published on loopback only, for the built-in
// proxy.
package containers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/leanhanc/tero/internal/podman"
)

// containerPort is the PORT every app listens on inside its container.
const containerPort = 8080

// Labels mark Tero's app containers so the agent can find leftovers.
const (
	AppLabel    = "tero.app"
	DeployLabel = "tero.deploy"
)

// Name is the container of one deploy of an app.
func Name(appName string, deployID int64) string {
	return fmt.Sprintf("tero-app-%s-%d", appName, deployID)
}

// startTimeout bounds podman run. Starting takes seconds, but before it
// Podman copies the image's file ownership into the container's user
// namespace, which takes minutes on a slow disk.
const startTimeout = 10 * time.Minute

// ErrStartTimedOut means Podman didn't get the container running in time.
var ErrStartTimedOut = fmt.Errorf("Your app's container took longer than %d minutes to start and was stopped. The server's disk may be too slow or too busy",
	int(startTimeout.Minutes()))

// Start runs a deploy's image and returns the container name and the
// loopback port its PORT is published on. When podman run fails, the name
// is still returned, since a container may have been half created.
func Start(ctx context.Context, appName string, deployID int64, image string) (string, int, error) {
	network, err := isolatedNetwork(ctx)
	if err != nil {
		return "", 0, err
	}

	hostPort, err := freeLoopbackPort()
	if err != nil {
		return "", 0, err
	}

	name := Name(appName, deployID)
	if err := podman.RemoveContainer(ctx, name); err != nil {
		return "", 0, err
	}

	startCtx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()

	scope := scopeName(name)
	_, err = podman.RunInOwnScope(startCtx, scope, "run", "--detach", "--name", name,
		"--label", AppLabel+"="+appName,
		"--label", DeployLabel+"="+strconv.FormatInt(deployID, 10),
		"--network", network,
		"--cap-drop=ALL",
		"--security-opt", "no-new-privileges",
		"--userns=auto",
		"--log-driver", "k8s-file", "--log-opt", "max-size=10mb",
		"--env", "PORT="+strconv.Itoa(containerPort),
		"--publish", fmt.Sprintf("127.0.0.1:%d:%d", hostPort, containerPort),
		image)
	if err != nil && startCtx.Err() != nil {
		// Killing podman leaves its children running in the scope, holding
		// the storage locks the cleanup needs.
		killCtx, cancelKill := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancelKill()
		if killErr := podman.KillScope(killCtx, scope); killErr != nil {
			slog.Warn("kill a container start that didn't finish", "container", name, "error", killErr)
		}
		if ctx.Err() == nil {
			return name, 0, ErrStartTimedOut
		}
	}
	if err != nil {
		return name, 0, fmt.Errorf("start the app's container: %w", err)
	}

	return name, hostPort, nil
}

// ExitedError means the app's process ended before it answered.
type ExitedError struct {
	ExitCode int
}

func (e ExitedError) Error() string {
	return fmt.Sprintf("Your app exited with code %d before it answered on PORT. Its last output is at the end of the log", e.ExitCode)
}

// ErrNeverAnswered means the app kept running but never answered HTTP.
var ErrNeverAnswered = errors.New("Your app didn't answer HTTP requests within 60 seconds. Make sure it listens on the port in the PORT environment variable")

// WaitReady polls the app until any HTTP response comes back. A TCP connect
// isn't enough: rootless port forwarding accepts connections on the host
// side before the app listens.
func WaitReady(ctx context.Context, container string, hostPort int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("http://127.0.0.1:%d/", hostPort)
	httpClient := &http.Client{
		Timeout: 2 * time.Second,
		// A redirect is already an answer.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	for {
		if answersHTTP(ctx, httpClient, url) {
			return nil
		}

		state, err := inspectState(ctx, container)
		if err != nil {
			return err
		}
		if !state.Running {
			return ExitedError{ExitCode: state.ExitCode}
		}

		if time.Now().After(deadline) {
			return ErrNeverAnswered
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// LastOutput returns the container's last lines of output, from both stdout
// and stderr, where crashes usually print.
func LastOutput(ctx context.Context, container string, lines int) string {
	output, err := podman.RunCombined(ctx, "logs", "--tail", strconv.Itoa(lines), container)
	if err != nil {
		return ""
	}

	return output
}

// IsRunning reports whether the container exists and is running.
func IsRunning(ctx context.Context, container string) bool {
	state, err := inspectState(ctx, container)
	return err == nil && state.Running
}

// RunningPorts returns the loopback port of every running app container,
// keyed by container name, from a single podman call.
func RunningPorts(ctx context.Context) (map[string]int, error) {
	output, err := podman.Run(ctx, "ps", "--filter", "label="+AppLabel, "--format", "json")
	if err != nil {
		return nil, err
	}

	return parseRunningPorts(output)
}

// psEntry is the part of podman ps's JSON output Tero reads. Podman 4 and 5
// both write ports with these keys; Podman 3, which Tero doesn't support,
// names them differently, so every port reads as 0 and none is recorded.
type psEntry struct {
	Names []string
	Ports []struct {
		HostPort      int `json:"host_port"`
		ContainerPort int `json:"container_port"`
	}
}

func parseRunningPorts(output string) (map[string]int, error) {
	ports := map[string]int{}
	if output == "" {
		return ports, nil
	}

	var entries []psEntry
	if err := json.Unmarshal([]byte(output), &entries); err != nil {
		return nil, fmt.Errorf("read podman's container list: %w", err)
	}

	for _, entry := range entries {
		for _, name := range entry.Names {
			ports[name] = publishedPort(entry)
		}
	}

	return ports, nil
}

// publishedPort is the host port the app's PORT is published on, or 0 when
// it isn't published.
func publishedPort(entry psEntry) int {
	for _, port := range entry.Ports {
		if port.ContainerPort == containerPort {
			return port.HostPort
		}
	}

	return 0
}

// Resume starts an existing, stopped container again.
func Resume(ctx context.Context, container string) error {
	_, err := podman.RunInOwnScope(ctx, scopeName(container), "start", container)
	return err
}

// scopeName is unique per start, since a scope can't be reused while the
// previous one is still being collected.
func scopeName(container string) string {
	return fmt.Sprintf("%s-%d.scope", container, time.Now().UnixNano())
}

// Stop stops a container and keeps it, so it can be resumed.
func Stop(ctx context.Context, container string) error {
	_, err := podman.Run(ctx, "stop", "--ignore", "--time", "10", container)
	return err
}

// Remove removes a container and, when given, its image.
func Remove(ctx context.Context, container, image string) error {
	if err := podman.RemoveContainer(ctx, container); err != nil {
		return err
	}
	if image == "" {
		return nil
	}

	_, err := podman.Run(ctx, "rmi", "--ignore", image)
	return err
}

type containerState struct {
	Running  bool
	ExitCode int
}

func inspectState(ctx context.Context, container string) (containerState, error) {
	output, err := podman.Run(ctx, "inspect", "--format", "{{json .State}}", container)
	if err != nil {
		return containerState{}, err
	}

	var state containerState
	if err := json.Unmarshal([]byte(output), &state); err != nil {
		return containerState{}, fmt.Errorf("read the state of %s: %w", container, err)
	}

	return state, nil
}

func answersHTTP(ctx context.Context, httpClient *http.Client, url string) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}

	response, err := httpClient.Do(request)
	if err != nil {
		return false
	}
	response.Body.Close()

	return true
}

// isolatedNetwork is the network mode that gives a container a network
// namespace of its own with no route to the host's loopback, where other
// apps' ports are published. Podman 5 uses pasta and Podman 4 slirp4netns,
// each told not to map the host into the container. Podman networks aren't
// used: on Podman 4 their shared rootless namespace hides the networks
// stored under tero's home in /var/lib.
func isolatedNetwork(ctx context.Context) (string, error) {
	networkMu.Lock()
	defer networkMu.Unlock()

	// Only a known mode is kept: a failed podman version call is retried on
	// the next start instead of failing every deploy until the agent restarts.
	if networkMode != "" {
		return networkMode, nil
	}

	version, err := podman.Run(ctx, "version", "--format", "{{.Client.Version}}")
	if err != nil {
		return "", err
	}

	major, _, _ := strings.Cut(version, ".")
	networkMode = "slirp4netns:allow_host_loopback=false"
	if majorVersion, _ := strconv.Atoi(major); majorVersion >= 5 {
		networkMode = "pasta:--no-map-gw"
	}

	return networkMode, nil
}

var (
	networkMu   sync.Mutex
	networkMode string
)

// freeLoopbackPort asks the kernel for an unused port. Another process could
// take it before Podman binds it; the start then fails and the deploy says so.
func freeLoopbackPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port, nil
}
