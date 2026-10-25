// Package breach is `sudo tero assume-breach`: the one command for a server
// that may be compromised. It stops the dashboard and every other process
// running as tero, so nothing an attacker left in memory survives, moves
// aside what something running as tero could have planted to run later,
// replaces everything the dashboard could have read or used (sessions,
// trusted browsers, certificates and their keys, the login and passkeys),
// rotates the
// master key, and tells the admin what to do outside the server. Apps are
// down until tero's user manager and the dashboard start again.
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
	"github.com/leanhanc/tero/internal/keyfiles"
	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/sysexec"
)

const (
	dashboardService = "tero.service"
	// caddyStorage holds the dashboard's and apps' certificates, their
	// private keys and the ACME account key.
	caddyStorage = "/var/lib/tero/caddy"
	// teroHome is tero's home directory. Its parent belongs to root, so tero
	// can't replace it with a link.
	teroHome    = "/var/lib/tero"
	teroCrontab = "/var/spool/cron/crontabs/tero"
	// QuarantineDir keeps what was moved out of those places, readable only
	// by root.
	QuarantineDir = "/var/lib/tero-breach"
)

// plantDirs are the directories under teroHome where something running as
// tero could put units that its user manager or Podman would start later.
// Tero never puts anything there: the agent's unit is in /etc/systemd/user.
var plantDirs = []string{
	".config/systemd/user",
	".config/systemd/user.control",
	".local/share/systemd/user",
	".config/containers/systemd",
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
	// TeroUID is the tero user's uid, whose user manager and processes are
	// stopped.
	TeroUID int
}

const plan = `This resets everything a compromised dashboard could have read or planted:

  - stops the dashboard, the agent, the apps and every other process running as tero;
    apps are down until they start again, usually a few minutes
  - moves aside units and cron jobs something running as tero added
  - ends every dashboard session and forgets every trusted browser
  - deletes the dashboard's and apps' certificates and keys, so new ones are issued
  - replaces the master key
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
	// Deferred starts run in reverse: the user manager starts before the
	// dashboard, so the agent is back when the dashboard asks for deploys.
	defer start(ctx, opts, dashboardService, &err)

	// tero's slice holds the user manager (the agent, builds and apps) and
	// every session, cron jobs included; /run/user/<uid> goes with
	// user-runtime-dir. Starting the user manager brings both back.
	uid := strconv.Itoa(opts.TeroUID)
	if _, err := opts.Run.Run(ctx, "systemctl", "stop", "user-"+uid+".slice", "user-runtime-dir@"+uid+".service"); err != nil {
		return err
	}
	defer start(ctx, opts, "user@"+uid+".service", &err)

	aside, err := quarantineDir(opts.Root)
	if err != nil {
		return err
	}
	// The crontab goes first, so cron can't start a job after the kill.
	moved, err := quarantine(opts.Root, aside, []string{teroCrontab})
	if err != nil {
		return fmt.Errorf("move aside tero's crontab: %w", err)
	}
	// Anything left running as tero (a process started outside its slice)
	// could keep acting as the dashboard once the reset is done. With none
	// left, nothing can change tero's files while they are checked and
	// moved.
	if err := stopTeroProcesses(ctx, opts); err != nil {
		return err
	}
	units, err := quarantine(opts.Root, aside, plantPaths(opts.Root))
	if err != nil {
		return fmt.Errorf("move aside planted units: %w", err)
	}
	moved = append(moved, units...)

	link, err := reset(ctx, opts)
	if err != nil {
		return err
	}

	report(ctx, opts, aside, moved, link)
	return nil
}

func start(ctx context.Context, opts Options, unit string, err *error) {
	_, startErr := opts.Run.Run(context.WithoutCancel(ctx), "systemctl", "start", unit)
	*err = errors.Join(*err, startErr)
}

// stopTeroProcesses kills every process left running as tero and checks
// none is left. pgrep exits 1 when nothing matches; any other failure means
// the check didn't happen.
func stopTeroProcesses(ctx context.Context, opts Options) error {
	uid := strconv.Itoa(opts.TeroUID)
	for range 3 {
		opts.Run.Run(ctx, "pkill", "--signal", "KILL", "--uid", uid)

		_, err := opts.Run.Run(ctx, "pgrep", "--uid", uid)
		var exit interface{ ExitCode() int }
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return nil
		}
		if err != nil {
			return fmt.Errorf("check for processes running as tero: %w", err)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}

	return errors.New("processes running as tero didn't stop; see `ps -u tero`")
}

func reset(ctx context.Context, opts Options) (string, error) {
	if err := os.RemoveAll(filepath.Join(opts.Root, caddyStorage)); err != nil {
		return "", fmt.Errorf("delete the certificates: %w", err)
	}

	client := opts.Keys.Keys
	if err := client.ResetLogin(ctx); err != nil {
		return "", fmt.Errorf("reset the login: %w", err)
	}
	if _, err := opts.Keys.Rotate(ctx); err != nil {
		return "", fmt.Errorf("replace the master key: %w", err)
	}

	token, err := client.IssueSetupLink(ctx)
	if err != nil {
		return "", fmt.Errorf("issue a setup link: %w", err)
	}
	if err := client.AppendEvent(ctx, keys.EventBreachReset, ""); err != nil {
		return "", err
	}

	return auth.SetupLink(opts.Domain, token), nil
}

func report(ctx context.Context, opts Options, aside string, moved []string, link string) {
	out := opts.Out
	fmt.Fprintln(out, "\nThe server was reset. Outside the server:")
	fmt.Fprintln(out, "  - Install the latest Tero if this may have come from a bug in Tero")
	fmt.Fprintln(out, "  - Change the old dashboard password anywhere else you used it")
	fmt.Fprintln(out, "  - Remove the old Tero entry from your authenticator app, and the old Tero passkeys from your devices")
	fmt.Fprintln(out, "  - Check the SSH keys in ~/.ssh/authorized_keys of every user on this server")

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

// plantPaths returns, for each plant directory that holds something, the
// path to move: the directory itself, or the first part of its path that
// isn't a real directory. A link is moved as a link, so a planted link to
// /root/.ssh moves the link and never what it points to. It runs once
// nothing runs as tero, so nothing changes between the check and the move.
func plantPaths(root string) []string {
	var found []string
	for _, dir := range plantDirs {
		path := teroHome
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
