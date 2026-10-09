// Package loginlink issues dashboard setup links for root CLI commands
// (`sudo tero init` and `sudo tero reset-login`). The database belongs to the
// tero user, so root never opens it: it runs this binary again as tero, which
// does the work and prints the link.
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
	"github.com/leanhanc/tero/internal/journal"
	"github.com/leanhanc/tero/internal/store"
)

// Command is the hidden subcommand the child runs. It is not in the usage
// text: only root CLI commands call it.
const Command = "_login-link"

// Action is what the child does.
type Action string

const (
	// Issue replaces any unused setup link with a new one.
	Issue Action = "issue"
	// Reset clears the admin login, ends all sessions and issues a new link.
	Reset Action = "reset"
)

// Run performs action as the tero user and returns the setup link. The
// caller must be root.
func Run(ctx context.Context, action Action) (string, error) {
	tero, err := user.Lookup("tero")
	if err != nil {
		return "", fmt.Errorf("find the tero user: %w", err)
	}
	uid, _ := strconv.ParseUint(tero.Uid, 10, 32)
	gid, _ := strconv.ParseUint(tero.Gid, 10, 32)

	executable, err := os.Executable()
	if err != nil {
		return "", err
	}

	var stdout, stderr bytes.Buffer
	child := exec.CommandContext(ctx, executable, Command, string(action))
	child.Dir = "/"
	child.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	child.Stdout, child.Stderr = &stdout, &stderr
	child.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{}},
	}

	if err := child.Run(); err != nil {
		return "", fmt.Errorf("%w\n%s", err, strings.TrimSpace(stderr.String()))
	}

	return strings.TrimSpace(stdout.String()), nil
}

// RunChild is the child side: it runs as tero, performs action and prints the
// link on stdout.
func RunChild(ctx context.Context, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: tero " + Command + " issue|reset")
	}

	// Matches the service's UMask, so the database's journal files are
	// private whichever process creates them.
	syscall.Umask(0o077)

	cfg, err := config.Load(hostfs.Host)
	if err != nil {
		return err
	}

	db, err := store.Open(ctx, store.Path)
	if err != nil {
		return err
	}
	defer db.Close()

	service := auth.New(db, auth.Options{Domain: cfg.DashboardDomain, Journal: journal.Send})

	var link string
	switch Action(args[0]) {
	case Issue:
		link, err = service.IssueSetupLink(ctx, "")
	case Reset:
		link, err = service.ResetLogin(ctx)
	default:
		return fmt.Errorf("unknown action %q", args[0])
	}
	if err != nil {
		return err
	}

	fmt.Println(link)
	return nil
}
