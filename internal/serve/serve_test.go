package serve

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestBuildCaddyConfigServesOnlyTheDashboardDomain(t *testing.T) {
	cfg, err := buildCaddyConfig(caddySettings{Domain: "dash.example.com", Issuer: map[string]any{"module": "acme"}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.Admin == nil || !cfg.Admin.Disabled {
		t.Fatal("the Caddy admin API must be disabled")
	}

	apps, err := json.Marshal(cfg.AppsRaw)
	if err != nil {
		t.Fatal(err)
	}

	for _, expected := range []string{`"dash.example.com"`, `":443"`, `Strict-Transport-Security`} {
		if !strings.Contains(string(apps), expected) {
			t.Errorf("config is missing %s", expected)
		}
	}
}

func TestBuildCaddyConfigSetsStrictCSP(t *testing.T) {
	cfg, err := buildCaddyConfig(caddySettings{Domain: "dash.example.com", Issuer: map[string]any{"module": "acme"}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	apps, _ := json.Marshal(cfg.AppsRaw)
	if strings.Contains(string(apps), "unsafe-inline") || strings.Contains(string(apps), "unsafe-eval") {
		t.Error("the CSP allows inline or eval'd code")
	}
	for _, expected := range []string{"default-src 'self'", "frame-ancestors 'none'", `"tero_dashboard"`} {
		if !strings.Contains(string(apps), expected) {
			t.Errorf("config is missing %s", expected)
		}
	}
}

func TestBuildCaddyConfig_v5_0_0_3_4_1_HSTSCoversSubdomainsForAYear(t *testing.T) {
	cfg, err := buildCaddyConfig(caddySettings{Domain: "dash.example.com", Issuer: map[string]any{"module": "acme"}}, nil)
	if err != nil {
		t.Fatal(err)
	}

	apps, _ := json.Marshal(cfg.AppsRaw)
	if !strings.Contains(string(apps), "max-age=31536000; includeSubDomains") {
		t.Error("HSTS is missing a one-year max-age with includeSubDomains")
	}
}

// domains: Dashboard API on an app host / No route before live
func TestBuildCaddyConfigRoutesAppsApartFromTheDashboard(t *testing.T) {
	routes := []appRoute{
		{Host: "api.203.0.113.10.sslip.io", Upstream: "127.0.0.1:41000"},
		{Host: "web.203.0.113.10.sslip.io"},
	}
	cfg, err := buildCaddyConfig(caddySettings{Domain: "dash.example.com", Issuer: map[string]any{"module": "acme"}}, routes)
	if err != nil {
		t.Fatal(err)
	}

	var apps struct {
		HTTP struct {
			Servers map[string]struct {
				Routes []struct {
					Match  []struct{ Host []string } `json:"match"`
					Handle []map[string]any          `json:"handle"`
				} `json:"routes"`
			} `json:"servers"`
		} `json:"http"`
	}
	encoded, _ := json.Marshal(cfg.AppsRaw)
	if err := json.Unmarshal(encoded, &apps); err != nil {
		t.Fatal(err)
	}

	handlers := map[string][]string{}
	for _, route := range apps.HTTP.Servers["dashboard"].Routes {
		for _, handle := range route.Handle {
			handlers[route.Match[0].Host[0]] = append(handlers[route.Match[0].Host[0]], handle["handler"].(string))
		}
	}

	if got := handlers["api.203.0.113.10.sslip.io"]; !slices.Equal(got, []string{"reverse_proxy"}) {
		t.Errorf("live app handlers = %v, want only reverse_proxy", got)
	}
	if got := handlers["web.203.0.113.10.sslip.io"]; !slices.Equal(got, []string{"static_response"}) {
		t.Errorf("undeployed app handlers = %v, want only the not-deployed page", got)
	}
	if !strings.Contains(string(encoded), `"api.203.0.113.10.sslip.io","web.203.0.113.10.sslip.io"`) {
		t.Error("app hosts are missing from the certificate subjects")
	}
}
