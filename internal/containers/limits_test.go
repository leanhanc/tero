package containers

import (
	"slices"
	"testing"
)

// Audit finding (2026-11-10): app containers had no memory or process limits.
func TestAppsGetMemoryAndProcessLimits(t *testing.T) {
	args := limitArgs()
	for _, flag := range []string{"--memory", "--memory-swap", "--pids-limit"} {
		if !slices.Contains(args, flag) {
			t.Errorf("limit flags %v have no %s", args, flag)
		}
	}
	if got := appMemoryLimit(4 << 30); got != 2<<30 {
		t.Errorf("limit on 4 GiB = %d, want 2 GiB", got)
	}
	if got := appMemoryLimit(0); got != minimumAppMemory {
		t.Errorf("limit with unknown memory = %d, want the minimum", got)
	}
}
