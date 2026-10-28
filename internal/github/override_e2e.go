//go:build e2e

package github

import (
	"os"
	"strings"
)

// e2eAPIFile holds the address of the end-to-end suite's fake GitHub in the
// test VM, when the suite started one.
const e2eAPIFile = "/opt/tero-e2e/github-api"

func testAPIOverride() (string, bool) {
	address, err := os.ReadFile(e2eAPIFile)
	if err != nil {
		return "", false
	}

	return strings.TrimSpace(string(address)), true
}
