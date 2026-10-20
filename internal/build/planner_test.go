package build

import (
	"context"
	"errors"
	"testing"
	"time"
)

// build-pipeline: No start command
func TestDescribePlanFailureNamesTheStartCommandFixes(t *testing.T) {
	err := describePlanFailure([]string{"No start command detected. Specify a start command: https://railpack.com/config/file#deploy"})
	if !errors.Is(err, errNoStartCommand) {
		t.Fatalf("err = %v, want errNoStartCommand", err)
	}
}

// build-pipeline: Railpack cannot plan
func TestDescribePlanFailureKeepsRailpacksReason(t *testing.T) {
	err := describePlanFailure([]string{"Railpack could not determine how to build the app."})

	var failure Failure
	if !errors.As(err, &failure) || failure.Reason != "Railpack couldn't work out how to build this app: Railpack could not determine how to build the app." {
		t.Fatalf("err = %v", err)
	}
}

func TestMemoryLimitIsAtLeastOneGiB(t *testing.T) {
	if limit := memoryLimit(); limit < minimumMemoryLimit {
		t.Fatalf("memoryLimit() = %d", limit)
	}
}

// build-pipeline: Build too long. Build runs under a 30-minute deadline;
// whatever fails once it passes is reported as the timeout.
func TestExplainContextErrorReportsTheTimeout(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	if err := explainContextError(ctx, errors.New("rpc error: context deadline exceeded")); !errors.Is(err, errTimedOut) {
		t.Fatalf("err = %v, want errTimedOut", err)
	}
	if buildTimeout != 30*time.Minute {
		t.Fatalf("buildTimeout = %s, want 30m", buildTimeout)
	}
}
