//go:build !linux

package keys

import (
	"errors"
	"net"
)

// identify refuses every caller: telling callers apart needs SO_PEERCRED,
// pidfds and cgroup v2, and tero-keys only runs on Linux servers.
func (p peers) identify(conn *net.UnixConn) (Caller, error) {
	return "", errors.New("the key service only runs on Linux")
}
