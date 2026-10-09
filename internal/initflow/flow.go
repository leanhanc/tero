// Package initflow implements `sudo tero init`: it checks the host, then turns
// it into a hardened Tero host in one run.
//
// Every check runs before any change, so a refused run leaves the server as it
// was. Every step is idempotent, so a run that failed halfway can be repeated;
// only a completed init refuses to run again.
package initflow

import (
	"context"
	"fmt"
	"io"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/leanhanc/tero/internal/config"
	"github.com/leanhanc/tero/internal/hostfs"
	"github.com/leanhanc/tero/internal/loginlink"
	"github.com/leanhanc/tero/internal/sysexec"
)

// Options are the inputs to init.
type Options struct {
	Version string

	// Domain is the dashboard domain from --domain. When empty, init asks for
	// one if it is attached to a terminal, and otherwise uses <ip>.sslip.io.
	Domain        string
	IsInteractive bool
	In            io.Reader
	Out           io.Writer
}

// host is what every check and step acts on.
type host struct {
	run   sysexec.Runner
	files hostfs.FS
	out   io.Writer
}

// Run performs `tero init` on the real host.
func Run(ctx context.Context, opts Options) error {
	h := host{
		run:   sysexec.Host{Env: []string{"DEBIAN_FRONTEND=noninteractive", "LC_ALL=C"}},
		files: hostfs.Host,
		out:   opts.Out,
	}

	if err := checkPreflight(ctx, h); err != nil {
		return err
	}

	domain, err := resolveDomain(ctx, opts)
	if err != nil {
		return err
	}
	// Init stays quiet until the dashboard is up: anything the admin needs to
	// see or decide belongs there. Step titles only appear when a step fails.
	fmt.Fprintln(h.out, "Setting up this server. This takes a few minutes...")

	for _, s := range steps(domain) {
		if err := s.apply(ctx, h); err != nil {
			return fmt.Errorf("Setup stopped while %s.\n\n%s\n\nFix the problem above and run `sudo tero init` again. Finished steps are skipped", s.title, capitalize(err.Error()))
		}
	}

	return finish(ctx, h, opts.Version, domain)
}

func finish(ctx context.Context, h host, version, domain string) error {
	hasCertificate := waitForCertificate(ctx, domain, 3*time.Minute)

	link, err := loginlink.Run(ctx, loginlink.Issue)
	if err != nil {
		return fmt.Errorf("Setup stopped while creating the dashboard setup link.\n\n%s\n\nRun `sudo tero init` again. Finished steps are skipped", capitalize(err.Error()))
	}

	marker := config.Marker{Version: version, Domain: domain, CompletedAt: time.Now().UTC()}
	if err := config.SaveMarker(h.files, marker); err != nil {
		return err
	}

	if !hasCertificate {
		fmt.Fprintf(h.out, "\nThe page may take a few minutes to load: %s has to point to this server's IP address first.\n", domain)
	}
	// The link is the last line, so it is easy to find and copy.
	fmt.Fprintf(h.out, "\nDone. Open this link to set up your dashboard login:\n\n  %s\n", link)

	return nil
}

// capitalize upper-cases the first letter of an error shown at the start of a
// line. Wrapped Go errors start lowercase by convention.
func capitalize(message string) string {
	first, size := utf8.DecodeRuneInString(message)
	return string(unicode.ToUpper(first)) + message[size:]
}
