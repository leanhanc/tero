package initflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/leanhanc/tero/internal/config"
	"github.com/leanhanc/tero/internal/platform"
	"github.com/leanhanc/tero/internal/rootcheck"
)

// binaryPath is where the install script puts tero. The systemd unit runs it
// from there, and only root can replace it.
const binaryPath = "/usr/local/bin/tero"

// checkPreflight runs every check that can refuse init. None of them change
// the host.
func checkPreflight(ctx context.Context, h host) error {
	if err := rootcheck.Require("init"); err != nil {
		return err
	}

	if err := checkPlatform(); err != nil {
		return err
	}

	if err := checkBinaryLocation(); err != nil {
		return err
	}

	if err := checkNotInitialized(h); err != nil {
		return err
	}

	return checkSSHLockout(ctx, h)
}

func checkPlatform() error {
	current, err := platform.Detect()
	if err != nil {
		return err
	}

	if current.IsTested() {
		return nil
	}

	return fmt.Errorf("Tero hasn't been tested on %s yet, so init stopped without changing anything.\nTested platforms:\n%s", current, platform.TestedList())
}

func checkBinaryLocation() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}

	resolved, err := filepath.EvalSymlinks(executable)
	if err != nil {
		return err
	}

	if resolved == binaryPath {
		return nil
	}

	return fmt.Errorf("tero is running from %s, but the Tero service runs it from %s.\nInstall it with the install script, or copy it there with:\n\n  sudo install -m 0755 %s %s", resolved, binaryPath, resolved, binaryPath)
}

func checkNotInitialized(h host) error {
	marker, err := config.LoadMarker(h.files)
	if errors.Is(err, config.ErrNotInitialized) {
		return nil
	}
	if err != nil {
		return err
	}

	return fmt.Errorf("This server is already a Tero host (set up %s), so init stopped without changing anything.\nDashboard: https://%s\n\nIf you lost access to the dashboard, run:\n\n  sudo tero reset-login", marker.CompletedAt.Format("2006-01-02"), marker.Domain)
}
