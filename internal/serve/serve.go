// Package serve runs the long-lived Tero service: an embedded Caddy that
// serves the dashboard over HTTPS on the domain chosen at init, getting and
// renewing its certificate automatically.
package serve

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caddyserver/caddy/v2"
	_ "github.com/caddyserver/caddy/v2/modules/standard"

	"github.com/leanhanc/tero/internal/config"
	"github.com/leanhanc/tero/internal/hostfs"
)

// stateDir is the service's StateDirectory; it is the only place the service
// can write.
const stateDir = "/var/lib/tero"

const placeholderPage = `<!doctype html>
<html lang="en">
<meta charset="utf-8">
<title>Tero</title>
<h1>Tero is running</h1>
<p>The dashboard will appear here.</p>
</html>
`

// Run serves until ctx is cancelled.
func Run(ctx context.Context) error {
	cfg, err := config.Load(hostfs.Host)
	if err != nil {
		return err
	}

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
		"Strict-Transport-Security": {"max-age=31536000"},
		"X-Content-Type-Options":    {"nosniff"},
		"Referrer-Policy":           {"no-referrer"},
		"Content-Security-Policy":   {"default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"},
	}

	dashboardRoute := map[string]any{
		"match": []any{map[string]any{"host": []string{domain}}},
		"handle": []any{
			map[string]any{"handler": "headers", "response": map[string]any{"set": securityHeaders}},
			map[string]any{
				"handler":     "static_response",
				"status_code": 200,
				"headers":     map[string][]string{"Content-Type": {"text/html; charset=utf-8"}},
				"body":        placeholderPage,
			},
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
