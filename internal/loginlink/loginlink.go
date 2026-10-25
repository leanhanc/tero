// Package loginlink issues dashboard setup links for root CLI commands
// (`sudo tero init` and `sudo tero reset-login`). The admin's credentials,
// sessions and setup links all belong to the key service, which answers root
// directly.
package loginlink

import (
	"context"
	"fmt"

	"github.com/leanhanc/tero/internal/auth"
	"github.com/leanhanc/tero/internal/config"
	"github.com/leanhanc/tero/internal/hostfs"
	"github.com/leanhanc/tero/internal/keys"
)

// Action is what Run does.
type Action string

const (
	// Issue replaces any unused setup link with a new one.
	Issue Action = "issue"
	// Reset clears the admin login, passkeys and every session, and issues a
	// new link.
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
		// The caller already says it was resetting the login.
		if err := client.ResetLogin(ctx); err != nil {
			return "", err
		}
	}

	token, err := client.IssueSetupLink(ctx)
	if err != nil {
		return "", fmt.Errorf("issue a setup link: %w", err)
	}

	return auth.SetupLink(cfg.DashboardDomain, token), nil
}
