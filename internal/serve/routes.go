package serve

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"time"

	"github.com/caddyserver/caddy/v2"
)

// routePollInterval is how often the service checks for new apps and newly
// live deploys. The agent writes them to the database; the service reloads
// Caddy when they change.
const routePollInterval = time.Second

// notDeployedPage is served on an app's address until its first deploy is
// live, so the address never falls through to the dashboard.
const notDeployedPage = `<!doctype html><meta charset="utf-8"><title>Not deployed yet</title>` +
	`<p>This app isn't deployed yet. If you're its admin, check its deploys in the Tero dashboard.</p>`

// appRoute is one app's address and, once a deploy is live, the loopback
// address its container listens on.
type appRoute struct {
	AppName  string
	Host     string
	HostPort int
	Upstream string
}

// appHost is an app's default address, which never depends on the
// dashboard domain.
func appHost(appName, publicIP string) string {
	return appName + "." + publicIP + ".sslip.io"
}

// loadAppRoutes returns every app's route, sorted by host so that two loads
// of the same state compare equal.
func loadAppRoutes(ctx context.Context, db *sql.DB, publicIP string) ([]appRoute, error) {
	if publicIP == "" {
		return nil, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT apps.name, COALESCE(live.host_port, 0)
		FROM apps LEFT JOIN deploys AS live ON live.app_id = apps.id AND live.state = 'live'
		ORDER BY apps.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var routes []appRoute
	for rows.Next() {
		var name string
		var hostPort int
		if err := rows.Scan(&name, &hostPort); err != nil {
			return nil, err
		}

		route := appRoute{AppName: name, Host: appHost(name, publicIP), HostPort: hostPort}
		if hostPort > 0 {
			route.Upstream = "127.0.0.1:" + strconv.Itoa(hostPort)
		}
		routes = append(routes, route)
	}

	return routes, rows.Err()
}

// watchAppRoutes reloads Caddy whenever the app routes change.
func watchAppRoutes(ctx context.Context, db *sql.DB, settings caddySettings, current []appRoute) {
	ticker := time.NewTicker(routePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		routes, err := loadAppRoutes(ctx, db, settings.PublicIP)
		if err != nil {
			slog.Error("load app routes", "error", err)
			continue
		}
		if slices.Equal(routes, current) {
			continue
		}

		if err := loadCaddy(settings, routes); err != nil {
			slog.Error("reload the web server with new app routes", "error", err)
			continue
		}
		current = routes
		if err := recordServedRoutes(ctx, db, routes); err != nil {
			slog.Error("record the served app routes", "error", err)
		}
	}
}

func loadCaddy(settings caddySettings, routes []appRoute) error {
	caddyConfig, err := buildCaddyConfig(settings, routes)
	if err != nil {
		return err
	}
	if err := caddy.Run(caddyConfig); err != nil {
		return fmt.Errorf("start the web server: %w", err)
	}

	return nil
}

// recordServedRoutes tells the agent which app ports the proxy now sends
// traffic to, replacing what was recorded before.
func recordServedRoutes(ctx context.Context, db *sql.DB, routes []appRoute) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM served_routes"); err != nil {
		return err
	}
	for _, route := range routes {
		if route.HostPort == 0 {
			continue
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO served_routes (app_name, host_port) VALUES (?, ?)", route.AppName, route.HostPort)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}

// appCaddyRoute proxies an app's host to its live container, or serves the
// not-deployed page. App responses get none of the dashboard's headers.
func appCaddyRoute(route appRoute) map[string]any {
	handler := map[string]any{
		"handler":     "static_response",
		"status_code": 503,
		"headers":     map[string][]string{"Content-Type": {"text/html; charset=utf-8"}, "Retry-After": {"30"}},
		"body":        notDeployedPage,
	}
	if route.Upstream != "" {
		handler = map[string]any{
			"handler":   "reverse_proxy",
			"upstreams": []any{map[string]any{"dial": route.Upstream}},
		}
	}

	return map[string]any{
		"match":    []any{map[string]any{"host": []string{route.Host}}},
		"handle":   []any{handler},
		"terminal": true,
	}
}
