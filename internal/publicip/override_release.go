//go:build !e2e

package publicip

import "net/netip"

// testOverride is only active in e2e builds. Release binaries always detect
// the real address.
func testOverride() (netip.Addr, bool) {
	return netip.Addr{}, false
}
