//go:build !e2e

package initflow

// skippedStep is only active in e2e builds. Release binaries run every step.
func skippedStep() string {
	return ""
}
