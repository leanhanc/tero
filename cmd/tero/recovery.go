package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/leanhanc/tero/internal/backup"
	"github.com/leanhanc/tero/internal/config"
	"github.com/leanhanc/tero/internal/hostfs"
	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/recovery"
	"github.com/leanhanc/tero/internal/rootcheck"
	"github.com/leanhanc/tero/internal/store"
)

// maxBackupBytes bounds the file restore reads, well above any backup Tero
// makes.
const maxBackupBytes = 128 << 20

func runNewRecoveryKey() error {
	if err := rootcheck.Require("new-recovery-key"); err != nil {
		return err
	}
	if _, err := config.LoadMarker(hostfs.Host); err != nil {
		return err
	}

	key, err := recovery.Install(hostfs.Host)
	if err != nil {
		return err
	}

	fmt.Print(recovery.Announcement(key.Secret))
	fmt.Println("\nBackups made before now still need the recovery key you had when they were made.")
	return nil
}

func runBackup(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("backup", flag.ContinueOnError)
	output := flags.String("output", "", "where to write the backup; defaults to tero-backup-<date>.age in this folder")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := rootcheck.Require("backup"); err != nil {
		return err
	}
	cfg, err := config.Load(hostfs.Host)
	if err != nil {
		return err
	}
	recipient, err := recovery.LoadRecipient(hostfs.Host)
	if err != nil {
		return err
	}
	db, err := store.Open(ctx, store.Path)
	if err != nil {
		return err
	}
	defer db.Close()

	now := time.Now()
	created, err := backup.Create(ctx, db, keys.NewClient(keys.SocketPath), recipient, cfg.DashboardDomain, now)
	if err != nil {
		return err
	}

	path := *output
	if path == "" {
		path = "tero-backup-" + now.UTC().Format("20060102-150405") + ".age"
	}
	if err := writeBackupFile(path, created.File); err != nil {
		return err
	}

	reportBackup(os.Stdout, path, created)
	return nil
}

// writeBackupFile never replaces an existing file, and gives the new one to
// the admin who ran sudo, so they can copy it off the server. It is
// encrypted, so its mode only keeps other local users from deleting it.
func writeBackupFile(path string, contents []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("%s already exists. Choose another name with --output", path)
	}
	if err != nil {
		return err
	}
	if _, err := file.Write(contents); err != nil {
		file.Close()
		os.Remove(path)
		return err
	}
	if err := giveToSudoUser(file); err != nil {
		file.Close()
		os.Remove(path)
		return err
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return err
	}

	return nil
}

// giveToSudoUser changes the open file's owner, never whatever the path
// names by now: someone who can write the folder could have swapped it for a
// link.
func giveToSudoUser(file *os.File) error {
	uid, uidErr := strconv.Atoi(os.Getenv("SUDO_UID"))
	gid, gidErr := strconv.Atoi(os.Getenv("SUDO_GID"))
	if uidErr != nil || gidErr != nil {
		return nil
	}

	return file.Chown(uid, gid)
}

func reportBackup(out io.Writer, path string, created backup.Created) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = path
	}

	fmt.Fprintf(out, "Backed up %d apps and their variables to:\n\n  %s\n\n", len(created.Apps), absolute)
	if len(created.Skipped) > 0 {
		fmt.Fprintf(out, "These variables were left out, since the master key that sealed them was lost. Set them again in the dashboard after restoring:\n")
		for _, name := range created.Skipped {
			fmt.Fprintf(out, "  %s\n", name)
		}
		fmt.Fprintln(out)
	}
	fmt.Fprintln(out, "Copy it off this server. It opens only with your recovery key, on a new server with `sudo tero restore`.")
}

func runRestore(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	isConfirmed := flags.Bool("yes", false, "don't ask for confirmation")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("Name the backup file: sudo tero restore <file>")
	}
	if err := rootcheck.Require("restore"); err != nil {
		return err
	}
	if _, err := config.LoadMarker(hostfs.Host); err != nil {
		return err
	}

	file, err := readBackupFile(flags.Arg(0))
	if err != nil {
		return err
	}
	in := bufio.NewReader(os.Stdin)
	secret, err := readRecoveryKey(in)
	if err != nil {
		return err
	}
	contents, err := backup.Open(secret, file)
	if err != nil {
		return err
	}

	describeBackup(os.Stdout, contents)
	if !*isConfirmed && !confirm(in) {
		return errors.New("Nothing was restored")
	}

	db, err := store.Open(ctx, store.Path)
	if err != nil {
		return err
	}
	defer db.Close()

	restored, err := backup.Restore(ctx, db, keys.NewClient(keys.SocketPath), secret, contents, time.Now())
	if err != nil {
		return err
	}

	reportRestore(os.Stdout, restored)
	return nil
}

func readBackupFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	contents, err := io.ReadAll(io.LimitReader(file, maxBackupBytes+1))
	if err != nil {
		return nil, err
	}
	if len(contents) > maxBackupBytes {
		return nil, backup.ErrInvalidBackup
	}

	return contents, nil
}

// readRecoveryKey asks for the key without echoing it on a terminal, so it
// stays out of the scrollback, and reads one line otherwise.
func readRecoveryKey(in *bufio.Reader) (string, error) {
	if !isTerminal(os.Stdin) {
		line, err := in.ReadString('\n')
		if err != nil && line == "" {
			return "", errors.New("Pass the recovery key on standard input")
		}
		return strings.TrimSpace(line), nil
	}

	fmt.Print("Recovery key: ")
	typed, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(typed)), nil
}

func describeBackup(out io.Writer, contents backup.Contents) {
	fmt.Fprintf(out, "This backup was made on %s for %s. It restores these apps and their variables, and deploys each one:\n\n",
		contents.CreatedAt.Format("2006-01-02 15:04 UTC"), contents.Domain)
	for _, app := range contents.Apps {
		location := app.Branch
		if app.RootDirectory != "" {
			location += ", folder " + app.RootDirectory
		}
		fmt.Fprintf(out, "  %s, from %s (branch %s)\n", app.Name, app.Repository, location)
	}
	fmt.Fprintln(out)
}

func confirm(in *bufio.Reader) bool {
	fmt.Print("Type yes to restore: ")
	answer, _ := in.ReadString('\n')
	return strings.TrimSpace(answer) == "yes"
}

func reportRestore(out io.Writer, restored backup.Restored) {
	count := 0
	for _, app := range restored.Variables.Apps {
		count += len(app.Variables)
	}

	fmt.Fprintf(out, "Restored %d apps and %d variables. Each app is queued to deploy.\n\n", len(restored.Apps), count)
	fmt.Fprintln(out, "Apps from private repositories can't deploy until you connect GitHub again in Settings; redeploy them once it's connected.")
}
