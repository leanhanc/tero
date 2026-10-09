// Package platform identifies the host OS and CPU architecture and checks
// them against the platforms Tero is tested on.
package platform

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"strings"
)

// Platform is a host's OS release and Go architecture name.
type Platform struct {
	OSID      string // os-release ID, e.g. "ubuntu"
	VersionID string // os-release VERSION_ID, e.g. "26.04"
	PrettyOS  string // os-release PRETTY_NAME, for messages
	Arch      string // Go architecture name: "amd64", "arm64", ...
}

type testedPlatform struct {
	osID      string
	versionID string
	name      string
	arches    []string
}

// tested lists every OS release and architecture the end-to-end suite runs
// on. Adding one means adding it here and running the suite on it. amd64
// joins once the suite runs on amd64 servers.
var tested = []testedPlatform{
	{osID: "ubuntu", versionID: "26.04", name: "Ubuntu 26.04 LTS", arches: []string{"arm64"}},
	{osID: "ubuntu", versionID: "24.04", name: "Ubuntu 24.04 LTS", arches: []string{"arm64"}},
}

// Detect reads the running host's platform.
func Detect() (Platform, error) {
	osRelease, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return Platform{}, fmt.Errorf("read /etc/os-release: %w", err)
	}

	return Parse(string(osRelease), runtime.GOARCH), nil
}

// Parse builds a Platform from the contents of /etc/os-release and a Go
// architecture name.
func Parse(osRelease, arch string) Platform {
	fields := parseOSRelease(osRelease)

	return Platform{
		OSID:      fields["ID"],
		VersionID: fields["VERSION_ID"],
		PrettyOS:  fields["PRETTY_NAME"],
		Arch:      arch,
	}
}

// IsTested reports whether Tero is tested on this OS release and architecture.
func (p Platform) IsTested() bool {
	for _, candidate := range tested {
		isSameRelease := candidate.osID == p.OSID && candidate.versionID == p.VersionID
		if isSameRelease && contains(candidate.arches, p.Arch) {
			return true
		}
	}

	return false
}

// String names the platform for messages, e.g. "Ubuntu 24.04.3 LTS (arm64)".
func (p Platform) String() string {
	osName := p.PrettyOS
	if osName == "" {
		osName = strings.TrimSpace(p.OSID + " " + p.VersionID)
	}
	if osName == "" {
		osName = "unknown OS"
	}

	return fmt.Sprintf("%s (%s)", osName, p.Arch)
}

// TestedList describes the tested platforms for error messages, one per line.
func TestedList() string {
	lines := make([]string, 0, len(tested))
	for _, candidate := range tested {
		lines = append(lines, fmt.Sprintf("  - %s on %s", candidate.name, strings.Join(candidate.arches, " or ")))
	}

	return strings.Join(lines, "\n")
}

func parseOSRelease(contents string) map[string]string {
	fields := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		key, value, hasEquals := strings.Cut(line, "=")
		if !hasEquals || strings.HasPrefix(line, "#") {
			continue
		}
		fields[key] = strings.Trim(value, `"'`)
	}

	return fields
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}

	return false
}
