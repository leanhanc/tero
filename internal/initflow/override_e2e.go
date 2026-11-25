//go:build e2e

package initflow

import "os"

// skippedStep names a step an e2e run leaves out on purpose, to prove the
// suite's Lynis minimum fails a server that isn't fully hardened.
func skippedStep() string {
	return os.Getenv("TERO_E2E_SKIP_STEP")
}
