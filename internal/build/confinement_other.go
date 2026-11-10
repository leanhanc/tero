//go:build !linux

package build

import "errors"

// confineToSource refuses to plan: only Linux's Landlock can confine the
// planner to the app's folder.
func confineToSource(string) error {
	return errors.New("planning a build needs Linux's Landlock")
}
