//go:build !e2e

package portcheck

// testOverride is only active in e2e builds. Release binaries always ask the
// real service.
func testOverride() (string, bool) {
	return "", false
}
