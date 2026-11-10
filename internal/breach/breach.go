// Package breach is `sudo tero assume-breach`: the one command for a server
// that may be compromised. It stops the dashboard, the agent and every other
// process running as tero or tero-agent, so nothing an attacker left in
// memory survives, moves aside what either user could have planted to run
// later, replaces everything the dashboard could have read or used
// (sessions, trusted browsers, certificates and their keys, the login and
// passkeys), rotates the master key, and tells the admin what to do outside
// the server. It also deletes every image, container and cache in
// tero-agent's Podman store, which something running as tero-agent could have
// changed, so apps stay down until the admin redeploys them from source.
package breach

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/leanhanc/tero/internal/auth"
	"github.com/leanhanc/tero/internal/deploys"
	"github.com/leanhanc/tero/internal/githubapp"
	"github.com/leanhanc/tero/internal/keyfiles"
	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/store"
	"github.com/leanhanc/tero/internal/sysexec"
)

const (
	dashboardService = "tero.service"
	// caddyStorage holds the dashboard's and apps' certificates, their
	// private keys and the ACME account key.
	caddyStorage = "/var/lib/tero/caddy"
	// teroHome and agentHome are the users' home directories. Their parent
	// belongs to root, so neither user can replace its home with a link.
	teroHome   = "/var/lib/tero"
	agentHome  = "/var/lib/tero-agent"
	crontabDir = "/var/spool/cron/crontabs"
	// QuarantineDir keeps what was moved out of those places, readable only
	// by root.
	QuarantineDir = "/var/lib/tero-breach"
)

// plantDirs are the directories under a user's home where something running
// as that user could put what its user manager or Podman would run or load
// later: units, environment.d, user.conf, Podman's containers.conf (runtime,
// conmon, hooks), registries and policies. Tero never puts anything there:
// the agent's unit is in /etc/systemd/user.
var plantDirs = []string{
	".config",
	".local/share/systemd/user",
	".local/share/containers/systemd",
}

// ErrNotConfirmed means the admin didn't type yes.
var ErrNotConfirmed = errors.New("nothing was changed")

// Options are the inputs to Run.
type Options struct {
	Domain string
	// IsConfirmed skips the prompt (--yes).
	IsConfirmed bool
	In          io.Reader
	Out         io.Writer
	// Root is prefixed to host paths; tests point it at a temporary
	// directory.
	Root string
	Run  sysexec.Runner
	Keys keyfiles.Files
	// TeroUID and AgentUID are the uids of tero and tero-agent, whose
	// sessions, user managers and processes are stopped.
	TeroUID  int
	AgentUID int
}

// serviceUser is one of the users the reset clears.
type serviceUser struct {
	name string
	uid  int
	home string
}

func (o Options) users() []serviceUser {
	return []serviceUser{
		{"tero", o.TeroUID, teroHome},
		{"tero-agent", o.AgentUID, agentHome},
	}
}

// reportVariables lists what to rotate at its provider, and each app's approved
// source, since someone holding a confirmed session could have approved their
// own. It never prints a value: root's verb doesn't return any.
func reportVariables(out io.Writer, summary []keys.AppVariablesSummary) {
	if len(summary) == 0 {
		return
	}

	fmt.Fprintln(out, "  - Rotate these variables at their providers, then replace them in the dashboard, and check each app's source:")
	for _, app := range summary {
		fmt.Fprintf(out, "      %s, from %s (%s)\n", app.App, app.Source.Repository, sourceLocation(app.Source))
		for _, variable := range app.Variables {
			label := ""
			if variable.IsSealed {
				label = " (sealed)"
			}
			fmt.Fprintf(out, "        %s%s\n", variable.Name, label)
		}
	}
}

func sourceLocation(source keys.AppSource) string {
	if source.RootDirectory == "" {
		return "branch " + source.Branch
	}

	return "branch " + source.Branch + ", folder " + source.RootDirectory
}

const plan = `This resets everything a compromised dashboard could have read or planted:

  - stops the dashboard, the agent, the apps and every other process running as
    tero or tero-agent
  - deletes every app's image and container and the build cache:
    apps stay down until you redeploy them
  - moves aside units and cron jobs something running as either user added, and
    deletes their temporary files
  - ends every dashboard session and forgets every trusted browser
  - deletes the dashboard's and apps' certificates and keys, so new ones are issued
  - replaces the master key, keeping apps' variables and listing them to rotate
  - deletes the GitHub connection; you then delete the App on GitHub and connect again
  - resets the dashboard password, TOTP and passkeys, and prints a new setup link

If a step fails, the dashboard, the agent and the apps stay stopped until this
runs to the end.

Type yes to continue: `

// Run performs the reset. The agent and the dashboard start again only once
// every step has succeeded: after a failure they stay stopped, so nothing a
// compromise left behind serves again before the admin has run the reset to
// the end.
func Run(ctx context.Context, opts Options) error {
	if !opts.IsConfirmed {
		fmt.Fprint(opts.Out, plan)
		answer, _ := bufio.NewReader(opts.In).ReadString('\n')
		if strings.TrimSpace(answer) != "yes" {
			return ErrNotConfirmed
		}
	}

	if err := holdServices(opts); err != nil {
		return err
	}
	if _, err := opts.Run.Run(ctx, "systemctl", "stop", dashboardService); err != nil {
		return keepStopped(err)
	}
	aside, moved, err := stopAndQuarantine(ctx, opts)
	if err != nil {
		return keepStopped(err)
	}

	result, err := reset(ctx, opts)
	if err != nil {
		return keepStopped(err)
	}
	if err := emptyAgentStore(ctx, opts); err != nil {
		return keepStopped(err)
	}
	if err := os.Remove(filepath.Join(opts.Root, ResetHoldPath)); err != nil {
		return keepStopped(fmt.Errorf("release the dashboard and the agent: %w", err))
	}

	report(ctx, opts, aside, moved, result)
	if err := startServices(ctx, opts); err != nil {
		return fmt.Errorf("the reset finished, but Tero didn't start again: %w\n\nStart it with `sudo systemctl start tero` once the problem is fixed", err)
	}
	return nil
}

// keepStopped explains that a failed reset leaves Tero stopped.
func keepStopped(err error) error {
	return fmt.Errorf("%w\n\nThe dashboard and the agent stay stopped. Fix the problem above and run `sudo tero assume-breach` again", err)
}

// stopAndQuarantine stops everything running as tero or tero-agent and moves
// aside what either could have planted, returning the quarantine directory and
// the paths moved there.
func stopAndQuarantine(ctx context.Context, opts Options) (string, []string, error) {
	// Each user's slice holds every session of that user, cron jobs
	// included, and tero-agent's also holds its user manager (the agent,
	// builds and apps); /run/user/<uid> goes with user-runtime-dir.
	for _, user := range opts.users() {
		uid := strconv.Itoa(user.uid)
		if _, err := opts.Run.Run(ctx, "systemctl", "stop", "user-"+uid+".slice", "user-runtime-dir@"+uid+".service"); err != nil {
			return "", nil, err
		}
	}

	aside, err := quarantineDir(opts.Root)
	if err != nil {
		return "", nil, err
	}
	// The crontabs go first, so cron can't start a job after the kill.
	moved, err := quarantine(opts.Root, aside, crontabs(opts.users()))
	if err != nil {
		return "", nil, fmt.Errorf("move aside the crontabs: %w", err)
	}
	// Anything left running as either user (a process started outside its
	// slice) could keep acting as the dashboard or the agent once the reset
	// is done. With none left, nothing can change their files while they are
	// checked and moved.
	for _, user := range opts.users() {
		if err := stopProcesses(ctx, opts, user); err != nil {
			return "", nil, err
		}
	}
	units, err := quarantine(opts.Root, aside, plantPaths(opts.Root, opts.users()))
	if err != nil {
		return "", nil, fmt.Errorf("move aside planted units: %w", err)
	}
	if err := removeTemporaryFiles(opts); err != nil {
		return "", nil, fmt.Errorf("delete temporary files: %w", err)
	}

	return aside, append(moved, units...), nil
}

// temporaryDirs are shared directories where tero and tero-agent can leave
// files a later run would use, such as the mise binary the build planner runs
// from /tmp/railpack. Ubuntu empties them only at boot.
var temporaryDirs = []string{"/tmp", "/var/tmp", "/dev/shm"}

// removeTemporaryFiles deletes every entry in temporaryDirs that belongs to
// tero or tero-agent. Nothing runs as either user by now, so nothing changes
// between the check and the removal, and RemoveAll never follows a link.
func removeTemporaryFiles(opts Options) error {
	uids := map[uint32]bool{}
	for _, user := range opts.users() {
		uids[uint32(user.uid)] = true
	}

	for _, dir := range temporaryDirs {
		entries, err := os.ReadDir(filepath.Join(opts.Root, dir))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}

		for _, entry := range entries {
			path := filepath.Join(opts.Root, dir, entry.Name())
			info, err := os.Lstat(path)
			if err != nil {
				return err
			}
			stat, hasOwner := info.Sys().(*syscall.Stat_t)
			if !hasOwner || !uids[stat.Uid] {
				continue
			}
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		}
	}

	return nil
}

// ResetHoldPath, while it exists, keeps the agent and the dashboard from
// starting: both units have ConditionPathExists=! on it. It sits in a
// directory neither service can write, and survives a reboot, so a reset that
// stopped halfway never lets either serve before it has run to the end.
const ResetHoldPath = "/etc/tero/reset-in-progress"

// holdServices writes ResetHoldPath before anything is stopped.
func holdServices(opts Options) error {
	holdPath := filepath.Join(opts.Root, ResetHoldPath)
	if err := os.MkdirAll(filepath.Dir(holdPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(holdPath, nil, 0o644); err != nil {
		return fmt.Errorf("hold the dashboard and the agent back: %w", err)
	}

	return nil
}

// emptyAgentStore starts tero-agent's user manager, with the agent still held
// back, since Podman needs it, and empties the agent's store.
func emptyAgentStore(ctx context.Context, opts Options) error {
	userManager := "user@" + strconv.Itoa(opts.AgentUID) + ".service"
	if _, err := opts.Run.Run(ctx, "systemctl", "start", userManager); err != nil {
		return err
	}
	if err := resetAgentStorage(ctx, opts); err != nil {
		return fmt.Errorf("delete the apps' images and the build cache: %w", err)
	}

	return nil
}

// startServices starts the agent, then the dashboard, so the agent is back
// when the dashboard queues deploys. tero has no user manager to start.
func startServices(ctx context.Context, opts Options) error {
	if _, err := opts.Run.Run(ctx, "systemctl", "--user", agentManager, "start", agentService); err != nil {
		return err
	}
	_, err := opts.Run.Run(ctx, "systemctl", "start", dashboardService)
	return err
}

const (
	agentManager = "--machine=tero-agent@.host"
	agentService = "tero-agent.service"
)

// resetAgentStorage empties tero-agent's Podman store: app images and
// containers, BuildKit's image and its cache volume. Something running as
// tero-agent could have changed any of them, to serve its own code as an app
// or to poison later builds.
func resetAgentStorage(ctx context.Context, opts Options) error {
	_, err := opts.Run.Run(ctx, "systemd-run", agentManager, "--user", "--wait", "--quiet", "podman", "system", "reset", "--force")
	return err
}

// failLiveReleases marks every live release failed in the shared database,
// since its image goes with the agent's store: the agent then doesn't try to
// start it, and the dashboard asks for a redeploy.
func failLiveReleases(ctx context.Context, opts Options) error {
	db, err := store.Open(ctx, filepath.Join(opts.Root, store.Path))
	if err != nil {
		return fmt.Errorf("open the apps' database: %w", err)
	}
	defer db.Close()

	if _, err := deploys.FailLiveReleases(ctx, db, time.Now()); err != nil {
		return fmt.Errorf("mark the apps' releases failed: %w", err)
	}

	return nil
}

func crontabs(users []serviceUser) []string {
	var paths []string
	for _, user := range users {
		paths = append(paths, filepath.Join(crontabDir, user.name))
	}

	return paths
}

// stopProcesses kills every process left running as user and checks none is
// left. pgrep exits 1 when nothing matches; any other failure means the
// check didn't happen.
func stopProcesses(ctx context.Context, opts Options, user serviceUser) error {
	uid := strconv.Itoa(user.uid)
	for range 3 {
		opts.Run.Run(ctx, "pkill", "--signal", "KILL", "--uid", uid)

		_, err := opts.Run.Run(ctx, "pgrep", "--uid", uid)
		var exit interface{ ExitCode() int }
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return nil
		}
		if err != nil {
			return fmt.Errorf("check for processes running as %s: %w", user.name, err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}

	return fmt.Errorf("processes running as %s didn't stop; see `ps -u %s`", user.name, user.name)
}

// resetResult is what the report needs from the reset.
type resetResult struct {
	link      string
	gitHubApp keys.GitHubApp
	variables []keys.AppVariablesSummary
}

// reset replaces everything the dashboard and the agent could have read or
// used. Apps' variables stay, since apps need them to come back: the report
// lists them to rotate at their providers.
func reset(ctx context.Context, opts Options) (resetResult, error) {
	if err := os.RemoveAll(filepath.Join(opts.Root, caddyStorage)); err != nil {
		return resetResult{}, fmt.Errorf("delete the certificates: %w", err)
	}

	client := opts.Keys.Keys
	if err := client.ResetLogin(ctx); err != nil {
		return resetResult{}, fmt.Errorf("reset the login: %w", err)
	}

	// The agent could have read the App's private key while it connected,
	// so the App itself has to go, not just its copy here.
	gitHubApp, err := client.GitHubStatus(ctx)
	if err != nil {
		return resetResult{}, fmt.Errorf("read the GitHub connection: %w", err)
	}
	if err := client.ForgetGitHub(ctx); err != nil {
		return resetResult{}, fmt.Errorf("delete the GitHub connection: %w", err)
	}

	if err := failLiveReleases(ctx, opts); err != nil {
		return resetResult{}, err
	}

	variables, err := client.VariablesSummary(ctx)
	if err != nil {
		return resetResult{}, fmt.Errorf("list the apps' variables: %w", err)
	}
	// A lost master key mustn't stop the response to a breach.
	if err := client.MarkLostVariables(ctx); err != nil {
		return resetResult{}, fmt.Errorf("mark lost variables: %w", err)
	}

	if _, err := opts.Keys.Rotate(ctx); err != nil {
		return resetResult{}, fmt.Errorf("replace the master key: %w", err)
	}

	token, err := client.IssueSetupLink(ctx)
	if err != nil {
		return resetResult{}, fmt.Errorf("issue a setup link: %w", err)
	}
	if err := client.AppendEvent(ctx, keys.EventBreachReset, ""); err != nil {
		return resetResult{}, err
	}

	return resetResult{link: auth.SetupLink(opts.Domain, token), gitHubApp: gitHubApp, variables: variables}, nil
}

func report(ctx context.Context, opts Options, aside string, moved []string, result resetResult) {
	out := opts.Out
	fmt.Fprintln(out, "\nThe server was reset. Outside the server:")
	fmt.Fprintln(out, "  - Install the latest Tero if this may have come from a bug in Tero")
	fmt.Fprintln(out, "  - Change the old dashboard password anywhere else you used it")
	fmt.Fprintln(out, "  - Remove the old Tero entry from your authenticator app, and the old Tero passkeys from your devices")
	if result.gitHubApp.IsConnected {
		fmt.Fprintf(out, "  - Delete the GitHub App %s at %s, then connect GitHub again from Settings\n",
			result.gitHubApp.Slug, githubapp.SettingsPage(result.gitHubApp))
	}
	fmt.Fprintln(out, "  - Check the SSH keys in ~/.ssh/authorized_keys of every user on this server")
	fmt.Fprintln(out, "  - Redeploy every app from the dashboard: its image was deleted, so it is built again from source")
	reportVariables(out, result.variables)

	for _, path := range moved {
		fmt.Fprintf(out, "  - Check %s, moved to %s: Tero never creates it, so something else put it there\n",
			path, filepath.Join(hostPath(opts.Root, aside), path))
	}

	if !opts.Keys.HasTPM(ctx) {
		fmt.Fprintln(out, "\nThis server has no TPM, so the master key is sealed with a key stored on its disk.")
		fmt.Fprintln(out, "A copy of the disk taken before now can still open the new master key.")
	}

	fmt.Fprintf(out, "\nOpen this link to set up your dashboard login again:\n\n  %s\n", result.link)
}

// quarantineDir creates a new directory under QuarantineDir for this run.
// The random suffix keeps two runs in the same second apart.
func quarantineDir(root string) (string, error) {
	if err := os.MkdirAll(filepath.Join(root, QuarantineDir), 0o700); err != nil {
		return "", err
	}

	return os.MkdirTemp(filepath.Join(root, QuarantineDir), time.Now().UTC().Format("20060102T150405Z")+"-")
}

// plantPaths returns, for each plant directory in each user's home that
// holds something, the path to move: the directory itself, or the first part
// of its path that isn't a real directory. A link is moved as a link, so a
// planted link to /root/.ssh moves the link and never what it points to. It
// runs once nothing runs as either user, so nothing changes between the
// check and the move.
func plantPaths(root string, users []serviceUser) []string {
	var found []string
	for _, user := range users {
		found = append(found, plantPathsIn(root, user.home)...)
	}

	return found
}

func plantPathsIn(root, home string) []string {
	var found []string
	for _, dir := range plantDirs {
		path := home
		isReal := true
		for _, part := range strings.Split(dir, "/") {
			path = filepath.Join(path, part)
			info, err := os.Lstat(filepath.Join(root, path))
			if errors.Is(err, os.ErrNotExist) {
				isReal = false
				break
			}
			if err != nil || !info.IsDir() {
				if !slices.Contains(found, path) {
					found = append(found, path)
				}
				isReal = false
				break
			}
		}

		if isReal {
			entries, _ := os.ReadDir(filepath.Join(root, path))
			if len(entries) > 0 {
				found = append(found, path)
			}
		}
	}

	return found
}

// quarantine moves each path that exists into aside, keeping its path. The
// parents of every path are root's or were checked by plantPaths, and rename
// never follows the last part of a path, so only the path itself moves.
func quarantine(root, aside string, paths []string) ([]string, error) {
	var moved []string
	for _, path := range paths {
		if _, err := os.Lstat(filepath.Join(root, path)); errors.Is(err, os.ErrNotExist) {
			continue
		}

		target := filepath.Join(aside, path)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return nil, err
		}
		if err := os.Rename(filepath.Join(root, path), target); err != nil {
			return nil, err
		}
		moved = append(moved, path)
	}

	return moved, nil
}

// hostPath is path without the Root prefix tests use.
func hostPath(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}

	return "/" + relative
}
