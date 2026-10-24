package deployer

import (
	"fmt"
	"testing"

	"github.com/leanhanc/tero/internal/containers"
)

// e2e finding: a container start that hung left the deploy on "starting"
// with no reason. It now fails with one the admin can act on.
func TestStartTimeoutIsTheAdminsToAct(t *testing.T) {
	reason, isActionable := actionableReason(fmt.Errorf("start: %w", containers.ErrStartTimedOut))
	if !isActionable || reason != containers.ErrStartTimedOut.Error() {
		t.Fatalf("actionableReason = %q, %v, want the start timeout", reason, isActionable)
	}
}
