//go:build e2e

package serve

import "os"

// acmeIssuer points end-to-end builds at the Pebble test CA running inside the
// test VM, which has no public address for Let's Encrypt to reach. Release
// builds can't be pointed anywhere but Let's Encrypt.
func acmeIssuer() map[string]any {
	return map[string]any{
		"module":                  "acme",
		"ca":                      os.Getenv("TERO_E2E_ACME_CA"),
		"trusted_roots_pem_files": []string{os.Getenv("TERO_E2E_ACME_ROOT")},
	}
}
