package portcheck

import (
	"fmt"
	"io"
)

// Style decides how report lines look. Color is only for terminals, and never
// when NO_COLOR is set (https://no-color.org).
type Style struct {
	HasColor bool
}

const (
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	reset  = "\033[0m"
)

// Write prints nothing when every port is open. Otherwise it prints one line
// per port and, when a port is closed, the help to open it.
func Write(out io.Writer, results []Result, provider Provider, style Style) {
	if AllHaveStatus(results, Open) {
		return
	}

	fmt.Fprintln(out)
	var closed []int
	for _, result := range results {
		switch result.Status {
		case Open:
			fmt.Fprintln(out, style.line("✅", green, openLine(result.Port)))
		case Closed:
			closed = append(closed, result.Port)
			fmt.Fprintln(out, style.line("❌", red, closedLine(result.Port)))
		default:
			fmt.Fprintln(out, style.line("⚠️", yellow, uncheckedLine(result.Port)))
		}
	}

	if len(closed) > 0 {
		fmt.Fprintln(out)
		for _, line := range providerHelp(provider, closed) {
			fmt.Fprintln(out, line)
		}
	}
	fmt.Fprintln(out, moreHelpLine())
}

func (s Style) line(emoji, color, text string) string {
	if !s.HasColor {
		return emoji + " " + text
	}
	return emoji + " " + color + text + reset
}

// AllHaveStatus reports whether every port has the given status.
func AllHaveStatus(results []Result, status Status) bool {
	for _, result := range results {
		if result.Status != status {
			return false
		}
	}
	return true
}

// AnyHasStatus reports whether at least one port has the given status.
func AnyHasStatus(results []Result, status Status) bool {
	for _, result := range results {
		if result.Status == status {
			return true
		}
	}
	return false
}
