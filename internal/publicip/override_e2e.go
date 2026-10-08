//go:build e2e

package publicip

import (
	"net/netip"
	"os"
)

// testOverride lets the end-to-end suite pick the public IP, because the test
// VM sits behind NAT on a laptop.
func testOverride() (netip.Addr, bool) {
	addr, err := netip.ParseAddr(os.Getenv("TERO_E2E_PUBLIC_IP"))
	return addr, err == nil
}
