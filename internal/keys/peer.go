package keys

import "errors"

// Caller is who is on the other end of a connection.
type Caller string

const (
	CallerDashboard Caller = "dashboard"
	CallerRoot      Caller = "root"
)

// errUnknownCaller means the peer is none of the three callers.
var errUnknownCaller = errors.New("the caller is neither the dashboard nor root")

// dashboardCgroup is the cgroup of tero.service, fixed by the unit file init
// writes. Only the dashboard runs there, and nothing running as tero can move
// itself in: that needs write access to the root cgroup. The agent is not a
// caller: anything running as tero under its user manager can join the
// agent's cgroup, since the user manager's subtree is delegated to tero.
const dashboardCgroup = "/system.slice/tero.service"

// peers tells callers apart for one server.
type peers struct {
	teroUID int
	// dashboard is the dashboard's cgroup: dashboardCgroup on a server.
	dashboard string
}

// callerFor matches the whole path: a cgroup tero creates elsewhere whose
// name ends the same way doesn't count.
func (p peers) callerFor(cgroup string) (Caller, error) {
	if cgroup == p.dashboard {
		return CallerDashboard, nil
	}

	return "", errUnknownCaller
}
