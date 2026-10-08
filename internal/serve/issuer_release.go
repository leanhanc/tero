//go:build !e2e

package serve

// letsEncrypt is the only certificate authority release binaries use.
const letsEncrypt = "https://acme-v02.api.letsencrypt.org/directory"

func acmeIssuer() map[string]any {
	return map[string]any{"module": "acme", "ca": letsEncrypt}
}
