// Package rootcheck guards the subcommands that must run as root. Root work
// is only ever reachable through `sudo tero ...` on the server itself.
package rootcheck

import (
	"fmt"
	"os"
)

// Require returns an error telling the user to re-run with sudo when the
// current process is not root. command is the subcommand as typed, e.g.
// "init" or "reset-login".
func Require(command string) error {
	isRoot := os.Geteuid() == 0
	if isRoot {
		return nil
	}

	return fmt.Errorf("tero %s changes system settings and must run as root.\nRun it again with sudo:\n\n  sudo tero %s", command, command)
}
