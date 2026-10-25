// Package serve runs the long-lived Tero service: an embedded Caddy that
// serves the dashboard over HTTPS on the domain chosen at init, getting and
// renewing its certificate automatically.
package serve

import (
	"context"
	"encoding/json"
	"time"

	"github.com/caddyserver/caddy/v2"
	_ "github.com/caddyserver/caddy/v2/modules/standard"

	"github.com/leanhanc/tero/internal/apps"
	"github.com/leanhanc/tero/internal/auth"
	"github.com/leanhanc/tero/internal/config"
	"github.com/leanhanc/tero/internal/dashboard"
	"github.com/leanhanc/tero/internal/deploylog"
	"github.com/leanhanc/tero/internal/hostfs"
	"github.com/leanhanc/tero/internal/journal"
	"github.com/leanhanc/tero/internal/keys"
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

	service := auth.New(auth.Options{
		Keys:      keys.NewClient(keys.SocketPath),
		Domain:    cfg.DashboardDomain,
		PublicIP:  cfg.PublicIP,
		Passwords: password.Policy{Breaches: password.NewPwnedPasswords()},
		Journal:   journal.Send,
	})
	appService := apps.NewService(db, cfg.PublicIP, time.Now)
	dashboard.Activate(dashboard.NewHandler(service, appService, cfg.DashboardDomain, deploylog.Dir))

	settings := caddySettings{Domain: cfg.DashboardDomain, PublicIP: cfg.PublicIP, Issuer: acmeIssuer()}
	routes, err := loadAppRoutes(ctx, db, settings.PublicIP)
	if err != nil {
		return err
	}
	if err := loadCaddy(settings, routes); err != nil {
		return err
	}
	if err := recordServedRoutes(ctx, db, routes); err != nil {
		return err
	}
	go watchAppRoutes(ctx, db, settings, routes)

	<-ctx.Done()
	return caddy.Stop()
}

// caddySettings are the parts of Caddy's config that don't change while the
// service runs.
type caddySettings struct {
	Domain   string
	PublicIP string
	Issuer   map[string]any
}

// buildCaddyConfig serves the dashboard domain and every app's address over
// HTTPS only. Port 80 answers ACME challenges, refuses the dashboard API, and
// redirects everything else to HTTPS.
func buildCaddyConfig(settings caddySettings, appRoutes []appRoute) (*caddy.Config, error) {
	domain := settings.Domain
	securityHeaders := map[string][]string{
		"Strict-Transport-Security":    {"max-age=31536000; includeSubDomains"},
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

	// Caddy's automatic HTTPS adds its redirects to this server after the
	// routes that match a host, so the API refusal wins for /api/ and every
	// other request is still redirected. An API call sent over plain HTTP fails
	// loudly instead of being quietly redirected (OWASP ASVS v5.0.0-4.1.2).
	plainHTTPAPIRoute := map[string]any{
		"match": []any{map[string]any{"host": []string{domain}, "path": []string{"/api/*"}}},
		"handle": []any{map[string]any{
			"handler":     "static_response",
			"status_code": 403,
			"headers":     map[string][]string{"Content-Type": {"application/json"}},
			"body":        `{"error":"The dashboard API is only served over HTTPS"}`,
		}},
		"terminal": true,
	}

	httpsRoutes := []any{dashboardRoute}
	subjects := []string{domain}
	for _, route := range appRoutes {
		httpsRoutes = append(httpsRoutes, appCaddyRoute(route))
		subjects = append(subjects, route.Host)
	}

	raw := map[string]any{
		"admin":   map[string]any{"disabled": true},
		"storage": map[string]any{"module": "file_system", "root": stateDir + "/caddy"},
		"apps": map[string]any{
			"http": map[string]any{
				"servers": map[string]any{
					"dashboard": map[string]any{
						"listen": []string{":443"},
						"routes": httpsRoutes,
					},
					"plain_http": map[string]any{
						"listen": []string{":80"},
						"routes": []any{plainHTTPAPIRoute},
					},
				},
			},
			"tls": map[string]any{
				"automation": map[string]any{
					"policies": []any{map[string]any{
						"subjects": subjects,
						"issuers":  []any{settings.Issuer},
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
