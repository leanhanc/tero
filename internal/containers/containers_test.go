package containers

import (
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/leanhanc/tero/internal/keys"
)

func TestParseRunningPortsReadsPublishedPort(t *testing.T) {
	output := `[
		{"Names": ["tero-app-api-7"], "Ports": [{"host_ip": "127.0.0.1", "container_port": 8080, "host_port": 41234, "range": 1, "protocol": "tcp"}]},
		{"Names": ["tero-app-web-3"], "Ports": null}
	]`

	ports, err := parseRunningPorts(output)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]int{"tero-app-api-7": 41234, "tero-app-web-3": 0}
	if !reflect.DeepEqual(ports, want) {
		t.Fatalf("ports = %v, want %v", ports, want)
	}
}

func TestParseRunningPortsAcceptsNoContainers(t *testing.T) {
	for _, output := range []string{"", "[]"} {
		ports, err := parseRunningPorts(output)
		if err != nil || len(ports) != 0 {
			t.Fatalf("parseRunningPorts(%q) = %v, %v; want no ports", output, ports, err)
		}
	}
}

func TestParseImageCommand(t *testing.T) {
	cases := []struct {
		output string
		want   []string
	}{
		{`["/bin/bash","-c"]|["node index.js"]`, []string{"/bin/bash", "-c", "node index.js"}},
		{`null|["/app"]`, []string{"/app"}},
		{`["/app"]|null` + "\n", []string{"/app"}},
	}
	for _, testCase := range cases {
		got, err := parseImageCommand(testCase.output)
		if err != nil {
			t.Errorf("%s: %v", testCase.output, err)
		}
		if !slices.Equal(got, testCase.want) {
			t.Errorf("%s = %v, want %v", testCase.output, got, testCase.want)
		}
	}
}

// app-variables: Image from another source
func TestImageWithoutALabelHasNoSource(t *testing.T) {
	for _, label := range []string{"", "<no value>", "not json"} {
		if source := parseSourceLabel(label); source != (keys.AppSource{}) {
			t.Errorf("label %q gave source %+v", label, source)
		}
	}
}

// process-isolation: File removed, Not visible to other processes
func TestEnvFile(t *testing.T) {
	dir := t.TempDir()
	original := runtimeDir
	runtimeDir = func() string { return dir }
	t.Cleanup(func() { runtimeDir = original })

	kept, left := Name("shop", 1), Name("shop", 2)
	if err := WriteEnvFile(kept, map[string]string{"A": "1"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteEnvFile(left, map[string]string{"A": "1"}); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"", "../../x", "tero-app-shop-1/../../x", "other"} {
		if err := WriteEnvFile(invalid, nil); err == nil {
			t.Errorf("wrote a file for %q", invalid)
		}
		if HasEnvFile(invalid) {
			t.Errorf("HasEnvFile(%q) = true", invalid)
		}
	}

	info, err := os.Stat(envDir())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("directory mode = %o", info.Mode().Perm())
	}
	if !HasEnvFile(kept) {
		t.Error("the file wasn't written")
	}

	if err := RemoveEnvFilesExcept(map[string]bool{kept: true}); err != nil {
		t.Fatal(err)
	}
	if !HasEnvFile(kept) || HasEnvFile(left) {
		t.Error("leftovers weren't removed correctly")
	}
	if err := RemoveEnvFile(kept); err != nil {
		t.Fatal(err)
	}
	if err := RemoveEnvFile(kept); err != nil {
		t.Errorf("removing a missing file: %v", err)
	}
}

// app-variables: Image from another source
func TestOnlyTheAppsOwnImagesRun(t *testing.T) {
	cases := map[string]bool{
		"localhost/tero/shop:deploy-12":  true,
		"localhost/tero/other:deploy-12": false,
		"docker.io/evil/x:latest":        false,
		"localhost/tero/shop:latest":     false,
		"localhost/tero/shop:deploy-":    false,
	}
	for image, want := range cases {
		if got := IsAppImage("shop", image); got != want {
			t.Errorf("%s = %v, want %v", image, got, want)
		}
	}
}
