// Package apps holds the rules for apps the admin creates.
package apps

import (
	"errors"
	"regexp"
)

// ErrInvalidName explains the naming rule in the new-app form.
var ErrInvalidName = errors.New("Use 1 to 30 lowercase letters, digits and hyphens, starting with a letter and not ending with a hyphen")

// namePattern keeps names valid as the first DNS label of the app's address.
var namePattern = regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,28}[a-z0-9])?$`)

// CheckName returns ErrInvalidName unless name is a valid app name.
func CheckName(name string) error {
	if !namePattern.MatchString(name) {
		return ErrInvalidName
	}

	return nil
}
