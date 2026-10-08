// Command tero is the whole of Tero: `sudo tero init` sets up a server once,
// and `tero serve` is the long-running service systemd starts as the tero
// user.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"unicode"
	"unicode/utf8"

	"github.com/leanhanc/tero/internal/initflow"
	"github.com/leanhanc/tero/internal/rootcheck"
	"github.com/leanhanc/tero/internal/serve"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `Usage:
  sudo tero init [--domain DOMAIN]   Set up this server as a Tero host
  sudo tero reset-login              Reset the dashboard login
  tero serve                         Run the Tero service (started by systemd)
  tero version                       Print the version
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
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return errors.New("Missing command")
	}

	command, commandArgs := args[0], args[1:]
	switch command {
	case "init":
		return runInit(ctx, commandArgs)
	case "serve":
		return serve.Run(ctx)
	case "reset-login":
		return runResetLogin()
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
	domain := flags.String("domain", "", "dashboard domain; defaults to <public-ip>.sslip.io")
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

func runResetLogin() error {
	if err := rootcheck.Require("reset-login"); err != nil {
		return err
	}

	return errors.New("Resetting the dashboard login isn't available yet")
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
