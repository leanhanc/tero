// Package serve runs the long-lived Tero service: an embedded Caddy that
// serves the dashboard over HTTPS on the domain chosen at init, getting and
// renewing its certificate automatically.
package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/caddyserver/caddy/v2"
	_ "github.com/caddyserver/caddy/v2/modules/standard"

	"github.com/leanhanc/tero/internal/auth"
	"github.com/leanhanc/tero/internal/config"
	"github.com/leanhanc/tero/internal/dashboard"
	"github.com/leanhanc/tero/internal/hostfs"
	"github.com/leanhanc/tero/internal/journal"
	"github.com/leanhanc/tero/internal/password"
	"github.com/leanhanc/tero/internal/store"
)

// stateDir is the service's StateDirectory; it is the only place the service
// can write.
const stateDir = "/var/lib/tero"

// contentSecurityPolicy allows only the dashboard's own files: no inline
// scripts or styles and no third-party origins.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; " +
	"connect-src 'self'; font-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// Run serves until ctx is cancelled.
func Run(ctx context.Context) error {
	cfg, err := config.Load(hostfs.Host)
	if err != nil {
		return err
	}

	db, err := store.Open(ctx, store.Path)
	if err != nil {
		return err
	}
	defer db.Close()

	service := auth.New(db, auth.Options{
		Domain:    cfg.DashboardDomain,
		Passwords: password.Policy{Breaches: password.NewPwnedPasswords()},
		Journal:   journal.Send,
	})
	dashboard.Activate(dashboard.NewHandler(service, cfg.DashboardDomain))
	go pruneHourly(ctx, service)

	caddyConfig, err := buildCaddyConfig(cfg.DashboardDomain, acmeIssuer())
	if err != nil {
		return err
	}

	if err := caddy.Run(caddyConfig); err != nil {
		return fmt.Errorf("start the web server: %w", err)
	}

	<-ctx.Done()
	return caddy.Stop()
}

// buildCaddyConfig serves the dashboard domain over HTTPS only. Caddy's
// automatic HTTPS adds the port 80 listener that answers ACME challenges and
// redirects everything else to HTTPS.
func buildCaddyConfig(domain string, issuer map[string]any) (*caddy.Config, error) {
	securityHeaders := map[string][]string{
		"Strict-Transport-Security":    {"max-age=31536000"},
		"X-Content-Type-Options":       {"nosniff"},
		"Referrer-Policy":              {"no-referrer"},
		"Content-Security-Policy":      {contentSecurityPolicy},
		"Cross-Origin-Opener-Policy":   {"same-origin"},
		"Cross-Origin-Resource-Policy": {"same-origin"},
		"Permissions-Policy":           {"camera=(), microphone=(), geolocation=(), payment=(), usb=()"},
	}

	dashboardRoute := map[string]any{
		"match": []any{map[string]any{"host": []string{domain}}},
		"handle": []any{
			map[string]any{"handler": "headers", "response": map[string]any{"set": securityHeaders, "deferred": true}},
			map[string]any{"handler": "tero_dashboard"},
		},
		"terminal": true,
	}

	raw := map[string]any{
		"admin":   map[string]any{"disabled": true},
		"storage": map[string]any{"module": "file_system", "root": stateDir + "/caddy"},
		"apps": map[string]any{
			"http": map[string]any{
				"servers": map[string]any{
					"dashboard": map[string]any{
						"listen": []string{":443"},
						"routes": []any{dashboardRoute},
					},
				},
			},
			"tls": map[string]any{
				"automation": map[string]any{
					"policies": []any{map[string]any{
						"subjects": []string{domain},
						"issuers":  []any{issuer},
					}},
				},
			},
		},
	}

	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}

	var caddyConfig caddy.Config
	if err := json.Unmarshal(encoded, &caddyConfig); err != nil {
		return nil, err
	}

	return &caddyConfig, nil
}

// pruneHourly keeps the login tables small while the service runs.
func pruneHourly(ctx context.Context, service *auth.Service) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		if err := service.Prune(ctx); err != nil && ctx.Err() == nil {
			slog.Error("pruning the login tables failed", "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
