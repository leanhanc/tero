// Package loginlink issues dashboard setup links for root CLI commands
// (`sudo tero init`, `sudo tero reset-login` and `sudo tero assume-breach`).
// The admin's credentials and setup links belong to the key service, which
// answers root directly. Sessions and trusted browsers are in the dashboard's
// database, which belongs to the tero user, so root never opens it: it runs
// this binary again as tero to clear them.
package loginlink

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"

	"github.com/leanhanc/tero/internal/auth"
	"github.com/leanhanc/tero/internal/config"
	"github.com/leanhanc/tero/internal/hostfs"
	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/store"
)

// Command is the hidden subcommand the child runs. It is not in the usage
// text: only root CLI commands call it.
const Command = "_login-link"

// Action is what Run does.
type Action string

const (
	// Issue replaces any unused setup link with a new one.
	Issue Action = "issue"
	// Reset clears the admin login, ends all sessions and issues a new link.
	Reset Action = "reset"
)

// Run performs action and returns the setup link. The caller must be root.
func Run(ctx context.Context, action Action) (string, error) {
	cfg, err := config.Load(hostfs.Host)
	if err != nil {
		return "", err
	}
	client := keys.NewClient(keys.SocketPath)

	if action == Reset {
		if err := ClearSessions(ctx); err != nil {
			return "", fmt.Errorf("end the dashboard's sessions: %w", err)
		}
		if err := client.ResetLogin(ctx); err != nil {
			return "", fmt.Errorf("reset the login in the key service: %w", err)
		}
	}

	token, err := client.IssueSetupLink(ctx)
	if err != nil {
		return "", fmt.Errorf("issue a setup link: %w", err)
	}

	return auth.SetupLink(cfg.DashboardDomain, token), nil
}

// ClearSessions ends every dashboard session and forgets trusted browsers and
// login throttles, in a child process running as tero.
func ClearSessions(ctx context.Context) error {
	tero, err := user.Lookup("tero")
	if err != nil {
		return fmt.Errorf("find the tero user: %w", err)
	}
	uid, _ := strconv.ParseUint(tero.Uid, 10, 32)
	gid, _ := strconv.ParseUint(tero.Gid, 10, 32)

	executable, err := os.Executable()
	if err != nil {
		return err
	}

	var stderr bytes.Buffer
	child := exec.CommandContext(ctx, executable, Command)
	child.Dir = "/"
	child.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	child.Stderr = &stderr
	child.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{}},
	}

	if err := child.Run(); err != nil {
		return fmt.Errorf("%w\n%s", err, strings.TrimSpace(stderr.String()))
	}

	return nil
}

// RunChild is the child side: it runs as tero and clears the dashboard's
// sessions, trusted browsers and throttles.
func RunChild(ctx context.Context, args []string) error {
	if len(args) != 0 {
		return errors.New("usage: tero " + Command)
	}

	// Matches the service's UMask, so the database's journal files are
	// private whichever process creates them.
	syscall.Umask(0o077)

	db, err := store.Open(ctx, store.Path)
	if err != nil {
		return err
	}
	defer db.Close()

	return auth.New(db, auth.Options{}).ResetLogin(ctx)
}
