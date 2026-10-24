package containers

import (
	"reflect"
	"testing"
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
