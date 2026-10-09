package serve

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildCaddyConfigServesOnlyTheDashboardDomain(t *testing.T) {
	cfg, err := buildCaddyConfig("dash.example.com", map[string]any{"module": "acme"})
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
	cfg, err := buildCaddyConfig("dash.example.com", map[string]any{"module": "acme"})
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
