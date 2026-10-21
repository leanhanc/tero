//go:build !e2e

package apps

// testDomain is only active in e2e builds. Release binaries always put apps
// on nip.io.
func testDomain() (string, bool) {
	return "", false
}
