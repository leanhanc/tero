//go:build e2e

package portcheck

import "os"

// testOverride points end-to-end builds at a fake connect-back service on the
// test server, since the test's public IP is made up.
func testOverride() (string, bool) {
	url := os.Getenv("TERO_E2E_PORT_PROBE")
	return url, url != ""
}
