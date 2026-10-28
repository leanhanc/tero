// Package breach is `sudo tero assume-breach`: the one command for a server
// that may be compromised. It stops the dashboard, the agent and every other
// process running as tero or tero-agent, so nothing an attacker left in
// memory survives, moves aside what either user could have planted to run
// later, replaces everything the dashboard could have read or used
// (sessions, trusted browsers, certificates and their keys, the login and
// passkeys), rotates the master key, and tells the admin what to do outside
// the server. Apps are down until tero-agent's user manager and the
// dashboard start again.
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
	"time"

	"github.com/leanhanc/tero/internal/auth"
	"github.com/leanhanc/tero/internal/build"
	"github.com/leanhanc/tero/internal/githubapp"
	"github.com/leanhanc/tero/internal/keyfiles"
	"github.com/leanhanc/tero/internal/keys"
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

const plan = `This resets everything a compromised dashboard could have read or planted:

  - stops the dashboard, the agent, the apps and every other process running as
    tero or tero-agent; apps are down until they start again, usually a few minutes
  - moves aside units and cron jobs something running as either user added
  - ends every dashboard session and forgets every trusted browser
  - deletes the dashboard's and apps' certificates and keys, so new ones are issued
  - replaces the master key
  - deletes the GitHub connection; you then delete the App on GitHub and connect again
  - resets the dashboard password, TOTP and passkeys, and prints a new setup link

Type yes to continue: `

// Run performs the reset. Once the dashboard is stopped, it is started again
// whatever happens next.
func Run(ctx context.Context, opts Options) (err error) {
	if !opts.IsConfirmed {
		fmt.Fprint(opts.Out, plan)
		answer, _ := bufio.NewReader(opts.In).ReadString('\n')
		if strings.TrimSpace(answer) != "yes" {
			return ErrNotConfirmed
		}
	}

	if _, err := opts.Run.Run(ctx, "systemctl", "stop", dashboardService); err != nil {
		return err
	}
	// Deferred starts run in reverse: the agent's user manager starts before
	// the dashboard, so the agent is back when the dashboard queues deploys.
	// The build cache is cleared in between, once Podman can run again.
	defer start(ctx, opts, dashboardService, &err)
	defer clearBuildCache(ctx, opts, &err)

	// Each user's slice holds every session of that user, cron jobs
	// included, and tero-agent's also holds its user manager (the agent,
	// builds and apps); /run/user/<uid> goes with user-runtime-dir.
	// Starting tero-agent's user manager brings both back for it. tero has
	// no user manager to start.
	for _, user := range opts.users() {
		uid := strconv.Itoa(user.uid)
		if _, err := opts.Run.Run(ctx, "systemctl", "stop", "user-"+uid+".slice", "user-runtime-dir@"+uid+".service"); err != nil {
			return err
		}
	}
	defer start(ctx, opts, "user@"+strconv.Itoa(opts.AgentUID)+".service", &err)

	aside, err := quarantineDir(opts.Root)
	if err != nil {
		return err
	}
	// The crontabs go first, so cron can't start a job after the kill.
	moved, err := quarantine(opts.Root, aside, crontabs(opts.users()))
	if err != nil {
		return fmt.Errorf("move aside the crontabs: %w", err)
	}
	// Anything left running as either user (a process started outside its
	// slice) could keep acting as the dashboard or the agent once the reset
	// is done. With none left, nothing can change their files while they are
	// checked and moved.
	for _, user := range opts.users() {
		if err := stopProcesses(ctx, opts, user); err != nil {
			return err
		}
	}
	units, err := quarantine(opts.Root, aside, plantPaths(opts.Root, opts.users()))
	if err != nil {
		return fmt.Errorf("move aside planted units: %w", err)
	}
	moved = append(moved, units...)

	link, gitHubApp, err := reset(ctx, opts)
	if err != nil {
		return err
	}

	report(ctx, opts, aside, moved, link, gitHubApp)
	return nil
}

func start(ctx context.Context, opts Options, unit string, err *error) {
	_, startErr := opts.Run.Run(context.WithoutCancel(ctx), "systemctl", "start", unit)
	*err = errors.Join(*err, startErr)
}

// clearBuildCache removes BuildKit's cache volume and image, which
// something running as tero-agent could have poisoned for later builds.
// Apps' own images stay, so they keep serving; the report says to redeploy.
func clearBuildCache(ctx context.Context, opts Options, err *error) {
	asAgent := []string{"--machine=tero-agent@.host", "--user", "--wait", "--quiet", "podman"}
	for _, args := range [][]string{
		{"volume", "rm", "--force", build.CacheVolume},
		{"rmi", "--force", build.BuildKitImage},
	} {
		_, runErr := opts.Run.Run(context.WithoutCancel(ctx), "systemd-run", append(asAgent, args...)...)
		*err = errors.Join(*err, runErr)
	}
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

// reset replaces everything the dashboard and the agent could have read or
// used, and returns the new setup link and the GitHub App it disconnected,
// if any.
func reset(ctx context.Context, opts Options) (string, keys.GitHubApp, error) {
	if err := os.RemoveAll(filepath.Join(opts.Root, caddyStorage)); err != nil {
		return "", keys.GitHubApp{}, fmt.Errorf("delete the certificates: %w", err)
	}

	client := opts.Keys.Keys
	if err := client.ResetLogin(ctx); err != nil {
		return "", keys.GitHubApp{}, fmt.Errorf("reset the login: %w", err)
	}

	// The agent could have read the App's private key while it connected,
	// so the App itself has to go, not just its copy here.
	gitHubApp, err := client.GitHubStatus(ctx)
	if err != nil {
		return "", keys.GitHubApp{}, fmt.Errorf("read the GitHub connection: %w", err)
	}
	if err := client.ForgetGitHub(ctx); err != nil {
		return "", keys.GitHubApp{}, fmt.Errorf("delete the GitHub connection: %w", err)
	}

	if _, err := opts.Keys.Rotate(ctx); err != nil {
		return "", keys.GitHubApp{}, fmt.Errorf("replace the master key: %w", err)
	}

	token, err := client.IssueSetupLink(ctx)
	if err != nil {
		return "", keys.GitHubApp{}, fmt.Errorf("issue a setup link: %w", err)
	}
	if err := client.AppendEvent(ctx, keys.EventBreachReset, ""); err != nil {
		return "", keys.GitHubApp{}, err
	}

	return auth.SetupLink(opts.Domain, token), gitHubApp, nil
}

func report(ctx context.Context, opts Options, aside string, moved []string, link string, gitHubApp keys.GitHubApp) {
	out := opts.Out
	fmt.Fprintln(out, "\nThe server was reset. Outside the server:")
	fmt.Fprintln(out, "  - Install the latest Tero if this may have come from a bug in Tero")
	fmt.Fprintln(out, "  - Change the old dashboard password anywhere else you used it")
	fmt.Fprintln(out, "  - Remove the old Tero entry from your authenticator app, and the old Tero passkeys from your devices")
	if gitHubApp.IsConnected {
		fmt.Fprintf(out, "  - Delete the GitHub App %s at %s, then connect GitHub again from Settings\n",
			gitHubApp.Slug, githubapp.SettingsPage(gitHubApp))
	}
	fmt.Fprintln(out, "  - Check the SSH keys in ~/.ssh/authorized_keys of every user on this server")
	fmt.Fprintln(out, "  - Redeploy every app, so its image is built again from source")

	for _, path := range moved {
		fmt.Fprintf(out, "  - Check %s, moved to %s: Tero never creates it, so something else put it there\n",
			path, filepath.Join(hostPath(opts.Root, aside), path))
	}

	if !opts.Keys.HasTPM(ctx) {
		fmt.Fprintln(out, "\nThis server has no TPM, so the master key is sealed with a key stored on its disk.")
		fmt.Fprintln(out, "A copy of the disk taken before now can still open the new master key.")
	}

	fmt.Fprintf(out, "\nOpen this link to set up your dashboard login again:\n\n  %s\n", link)
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
