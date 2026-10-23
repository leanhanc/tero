// Command tero is the whole of Tero: `sudo tero init` sets up a server once,
// `tero serve` is the long-running service systemd starts as the tero user,
// and `tero keys` is the key service systemd starts as tero-keys.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/leanhanc/tero/internal/agent"
	"github.com/leanhanc/tero/internal/breach"
	"github.com/leanhanc/tero/internal/config"
	"github.com/leanhanc/tero/internal/deployer"
	"github.com/leanhanc/tero/internal/deploylog"
	"github.com/leanhanc/tero/internal/hostfs"
	"github.com/leanhanc/tero/internal/initflow"
	"github.com/leanhanc/tero/internal/keyfiles"
	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/loginlink"
	"github.com/leanhanc/tero/internal/rootcheck"
	"github.com/leanhanc/tero/internal/serve"
	"github.com/leanhanc/tero/internal/store"
	"github.com/leanhanc/tero/internal/sysexec"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// usage lists only the commands people run. systemd starts the service with
// `tero serve`, the agent with `tero agent` and the key service with `tero
// keys`; nobody runs those by hand.
const usage = `Usage:
  sudo tero init [--domain DOMAIN]   Set up this server as a Tero host
  sudo tero reset-login              Reset the dashboard login
  sudo tero rotate-key               Replace the master key
  sudo tero assume-breach [--yes]    Lock everything down after a suspected breach
  tero version                       Print the version
  tero help                          Show this list
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, capitalize(err.Error()))
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	// A bare `tero` is a request for help, not a mistake.
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}

	command, commandArgs := args[0], args[1:]
	switch command {
	case "init":
		return runInit(ctx, commandArgs)
	case "serve":
		return serve.Run(ctx)
	case "agent":
		return runAgent(ctx)
	case "keys":
		return keys.Run(ctx)
	case "reset-login":
		return runResetLogin(ctx)
	case "rotate-key":
		return runRotateKey(ctx)
	case "assume-breach":
		return runAssumeBreach(ctx, commandArgs)
	case loginlink.Command:
		return loginlink.RunChild(ctx, commandArgs)
	case "version", "--version":
		fmt.Println(version)
		return nil
	case "help", "--help", "-h":
		fmt.Print(usage)
		return nil
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("Unknown command %q", command)
	}
}

func runInit(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	domain := flags.String("domain", "", "dashboard domain; defaults to <public-ip>.nip.io")
	if err := flags.Parse(args); err != nil {
		return err
	}

	return initflow.Run(ctx, initflow.Options{
		Version:       version,
		Domain:        *domain,
		IsInteractive: isTerminal(os.Stdin),
		In:            os.Stdin,
		Out:           os.Stdout,
	})
}

func runResetLogin(ctx context.Context) error {
	if err := rootcheck.Require("reset-login"); err != nil {
		return err
	}
	if _, err := config.LoadMarker(hostfs.Host); err != nil {
		return err
	}

	// A key that no longer opens stops the key service; only the login
	// depends on it, and this replaces the login anyway.
	isKeyReplaced, err := keyfiles.Host().Recover(ctx)
	if err != nil {
		return fmt.Errorf("start the key service: %w", err)
	}
	if isKeyReplaced {
		fmt.Printf("The master key no longer opened, so a new one was sealed. The old one is kept in %s.\n\n", keyfiles.UnreadableDir)
	}

	link, err := loginlink.Run(ctx, loginlink.Reset)
	if err != nil {
		return fmt.Errorf("reset the dashboard login: %w", err)
	}

	fmt.Printf("The dashboard login was reset. Open this link to set it up again:\n\n  %s\n", link)
	return nil
}

func runRotateKey(ctx context.Context) error {
	if err := rootcheck.Require("rotate-key"); err != nil {
		return err
	}
	if _, err := config.LoadMarker(hostfs.Host); err != nil {
		return err
	}

	version, err := keyfiles.Host().Rotate(ctx)
	if err != nil {
		return fmt.Errorf("replace the master key: %w", err)
	}

	fmt.Printf("The master key was replaced (version %d).\n", version)
	return nil
}

func runAssumeBreach(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("assume-breach", flag.ContinueOnError)
	isConfirmed := flags.Bool("yes", false, "don't ask for confirmation")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if err := rootcheck.Require("assume-breach"); err != nil {
		return err
	}
	cfg, err := config.Load(hostfs.Host)
	if err != nil {
		return err
	}
	tero, err := user.Lookup("tero")
	if err != nil {
		return fmt.Errorf("find the tero user: %w", err)
	}
	teroUID, err := strconv.Atoi(tero.Uid)
	if err != nil {
		return err
	}

	return breach.Run(ctx, breach.Options{
		Domain:        cfg.DashboardDomain,
		IsConfirmed:   *isConfirmed,
		In:            os.Stdin,
		Out:           os.Stdout,
		Root:          "/",
		Run:           sysexec.Host{},
		Keys:          keyfiles.Host(),
		TeroUID:       teroUID,
		ClearSessions: loginlink.ClearSessions,
	})
}

func runAgent(ctx context.Context) error {
	db, err := store.Open(ctx, store.Path)
	if err != nil {
		return err
	}
	defer db.Close()

	pipeline := deployer.Pipeline{DB: db, LogDir: deploylog.Dir, WorkDir: deployer.WorkDir, Now: time.Now}
	return agent.Run(ctx, agent.Options{DB: db, Pipeline: pipeline})
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// capitalize upper-cases the first letter of a message shown to the user.
// Wrapped Go errors start lowercase by convention.
func capitalize(message string) string {
	first, size := utf8.DecodeRuneInString(message)
	return string(unicode.ToUpper(first)) + message[size:]
}
