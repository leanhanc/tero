package portcheck

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/leanhanc/tero/internal/hostfs"
)

var serverIP = netip.MustParseAddr("203.0.113.10")

// fakeService answers like echoip, with body for every port.
func fakeService(t *testing.T, answer func(port string) (int, string)) Prober {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		port := strings.TrimPrefix(r.URL.Path, "/port/")
		status, body := answer(port)
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	return Prober{BaseURL: server.URL, PublicIP: serverIP, Client: server.Client()}
}

func TestCheck(t *testing.T) {
	tests := map[string]struct {
		status int
		body   func(port string) string
		want   Status
	}{
		"open":   {200, func(port string) string { return `{"ip":"203.0.113.10","port":` + port + `,"reachable":true}` }, Open},
		"closed": {200, func(port string) string { return `{"ip":"203.0.113.10","port":` + port + `,"reachable":false}` }, Closed},
		"another address": {200, func(port string) string {
			return `{"ip":"198.51.100.7","port":` + port + `,"reachable":false}`
		}, Unchecked},
		"another port":     {200, func(string) string { return `{"ip":"203.0.113.10","port":22,"reachable":false}` }, Unchecked},
		"not an address":   {200, func(port string) string { return `{"ip":"nope","port":` + port + `,"reachable":true}` }, Unchecked},
		"bad json":         {200, func(string) string { return `<html>` }, Unchecked},
		"ipv4 mapped ipv6": {200, func(port string) string { return `{"ip":"::ffff:203.0.113.10","port":` + port + `,"reachable":true}` }, Open},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			prober := fakeService(t, func(port string) (int, string) { return test.status, test.body(port) })

			results := prober.Check(context.Background(), WebPorts)

			for index, result := range results {
				if result.Port != WebPorts[index] || result.Status != test.want {
					t.Errorf("got %+v, want port %d %v", result, WebPorts[index], test.want)
				}
			}
		})
	}
}

func TestCheckRateLimited(t *testing.T) {
	tests := map[string]struct {
		retryAfter string
		want       Status
	}{
		"retried after a short wait": {"0", Open},
		"long wait not retried":      {"3600", Unchecked},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests == 1 {
					w.Header().Set("Retry-After", test.retryAfter)
					w.WriteHeader(http.StatusTooManyRequests)
					return
				}
				w.Write([]byte(`{"ip":"203.0.113.10","port":80,"reachable":true}`))
			}))
			t.Cleanup(server.Close)
			prober := Prober{BaseURL: server.URL, PublicIP: serverIP, Client: server.Client()}

			results := prober.Check(context.Background(), []int{80})

			if results[0].Status != test.want {
				t.Errorf("got %v, want %v", results[0].Status, test.want)
			}
		})
	}
}

func TestCheckServiceDown(t *testing.T) {
	prober := Prober{BaseURL: "http://127.0.0.1:1", PublicIP: serverIP, Client: ipv4Client()}

	results := prober.Check(context.Background(), WebPorts)

	if !AllHaveStatus(results, Unchecked) {
		t.Errorf("got %+v, want every port unchecked", results)
	}
}

func TestDetectProvider(t *testing.T) {
	tests := map[string]struct {
		vendor, assetTag string
		want             Provider
	}{
		"aws":          {"Amazon EC2", "", AWS},
		"google cloud": {"Google", "", GoogleCloud},
		"azure":        {"Microsoft Corporation", azureAssetTag, Azure},
		"hyper-v":      {"Microsoft Corporation", "", UnknownProvider},
		"oracle cloud": {"QEMU", "OracleCloud.com", OracleCloud},
		"aws on xen":   {"Xen", "", AWS},
		"unknown":      {"Hetzner", "", UnknownProvider},
		"no dmi":       {"", "", UnknownProvider},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			files := hostfs.FS{Root: t.TempDir()}
			writeDMI(t, files, "sys_vendor", test.vendor)
			writeDMI(t, files, "chassis_asset_tag", test.assetTag)
			if test.vendor == "Xen" {
				writeDMI(t, files, "bios_version", "4.11.amazon")
			}

			if got := DetectProvider(files); got != test.want {
				t.Errorf("got %v, want %v", got, test.want)
			}
		})
	}
}

func writeDMI(t *testing.T, files hostfs.FS, name, value string) {
	t.Helper()
	if value == "" {
		return
	}
	target := files.Path("/sys/class/dmi/id/" + name)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(value+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWriteBothOpen(t *testing.T) {
	var out bytes.Buffer

	Write(&out, []Result{{80, Open}, {443, Open}}, AWS, Style{HasColor: true})

	if out.Len() != 0 {
		t.Errorf("printed %q, want nothing", out.String())
	}
}

func TestWriteClosedPort(t *testing.T) {
	var out bytes.Buffer

	Write(&out, []Result{{80, Closed}, {443, Open}}, AWS, Style{})

	got := out.String()
	for _, want := range []string{"❌ Port 80 is closed", "✅ Port 443 is open", "To open port 80 on AWS", "type HTTP,", docsURL} {
		if !strings.Contains(got, want) {
			t.Errorf("output lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "HTTPS,") {
		t.Errorf("help asks to open 443, which is open:\n%s", got)
	}
}

func TestWriteUnchecked(t *testing.T) {
	var out bytes.Buffer

	Write(&out, []Result{{80, Unchecked}, {443, Unchecked}}, GoogleCloud, Style{})

	got := out.String()
	if !strings.Contains(got, "Couldn't check whether port 80") || !strings.Contains(got, "Couldn't check whether port 443") {
		t.Errorf("output doesn't say the ports weren't checked:\n%s", got)
	}
	if strings.Contains(got, "To open") {
		t.Errorf("printed provider help without a closed port:\n%s", got)
	}
}

func TestWriteColor(t *testing.T) {
	results := []Result{{80, Closed}, {443, Unchecked}}

	var plain, colored bytes.Buffer
	Write(&plain, results, UnknownProvider, Style{})
	Write(&colored, results, UnknownProvider, Style{HasColor: true})

	if strings.Contains(plain.String(), "\033[") {
		t.Errorf("plain output has escape sequences: %q", plain.String())
	}
	if !strings.Contains(colored.String(), red) {
		t.Errorf("colored output has no red: %q", colored.String())
	}
}

func TestProviderHelpNamesOnlyClosedPorts(t *testing.T) {
	for _, provider := range []Provider{UnknownProvider, AWS, GoogleCloud, Azure, OracleCloud} {
		help := strings.Join(providerHelp(provider, []int{443}), "\n")
		if strings.Contains(help, "80") {
			t.Errorf("provider %d help mentions port 80:\n%s", provider, help)
		}
		if !strings.Contains(help, "443") {
			t.Errorf("provider %d help doesn't mention port 443:\n%s", provider, help)
		}
	}
}
