package variables

import (
	"slices"
	"testing"
)

// build-pipeline: Build prints a value
func TestMaskedValues(t *testing.T) {
	released := Released{
		{Name: "SHORT", Value: "true"},
		{Name: "LONG", Value: "a-long-plain-value"},
		{Name: "SEALED", Value: "abc", IsSealed: true},
		{Name: "PEM", Value: "-----BEGIN KEY-----\nline-one-secret\nab\n", IsSealed: true},
	}

	masked := released.Masked()
	for _, want := range []string{"a-long-plain-value", "abc", "line-one-secret", "-----BEGIN KEY-----"} {
		if !slices.Contains(masked, want) {
			t.Errorf("%q isn't masked: %q", want, masked)
		}
	}
	for _, unwanted := range []string{"true", "ab"} {
		if slices.Contains(masked, unwanted) {
			t.Errorf("%q is masked, which would ruin the log", unwanted)
		}
	}
}
