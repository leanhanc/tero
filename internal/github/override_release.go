//go:build !e2e

package github

// testAPIOverride is only active in e2e builds. Release binaries always talk
// to api.github.com.
func testAPIOverride() (string, bool) {
	return "", false
}
