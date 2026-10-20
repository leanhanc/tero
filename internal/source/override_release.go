//go:build !e2e

package source

// testOverride is only active in e2e builds. Release binaries always fetch
// from GitHub.
func testOverride(Repository) (string, bool) {
	return "", false
}
