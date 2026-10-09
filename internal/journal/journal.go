// Package journal writes structured entries to the systemd journal through
// its native socket, so they carry fields that `journalctl` can filter on.
package journal

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

const socketPath = "/run/systemd/journal/socket"

// Priority levels from syslog(3).
const (
	PriorityWarning = 4
	PriorityNotice  = 5
	PriorityInfo    = 6
)

// Send writes one entry with message and extra fields. Field names must be
// upper case, as the journal requires. Values must be single-line.
func Send(priority int, message string, fields map[string]string) error {
	connection, err := net.Dial("unixgram", socketPath)
	if err != nil {
		return err
	}
	defer connection.Close()

	_, err = connection.Write([]byte(format(priority, message, fields)))
	return err
}

func format(priority int, message string, fields map[string]string) string {
	var entry strings.Builder
	fmt.Fprintf(&entry, "PRIORITY=%d\nSYSLOG_IDENTIFIER=tero\nMESSAGE=%s\n", priority, singleLine(message))

	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		fmt.Fprintf(&entry, "%s=%s\n", name, singleLine(fields[name]))
	}

	return entry.String()
}

func singleLine(value string) string {
	return strings.ReplaceAll(value, "\n", " ")
}
