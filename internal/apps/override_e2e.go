//go:build e2e

package apps

import "os"

// testDomain lets the local dev VM put apps under a domain the browser sends
// to the laptop, such as localhost, because the VM's public IP is not
// reachable.
func testDomain() (string, bool) {
	domain := os.Getenv("TERO_E2E_APP_DOMAIN")
	return domain, domain != ""
}
