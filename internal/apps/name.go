// Package apps holds the rules for apps the admin creates.
package apps

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ErrInvalidName explains the naming rule in the new-app form.
var ErrInvalidName = errors.New("Use 1 to 30 lowercase letters, digits and hyphens, starting with a letter and not ending with a hyphen")

// ErrInvalidDisplayName explains the display name rule in the dashboard.
var ErrInvalidDisplayName = errors.New("Use 1 to 60 characters, on one line")

// maxDisplayNameLength counts characters, not bytes.
const maxDisplayNameLength = 60

// namePattern keeps names valid as the first DNS label of the app's address.
var namePattern = regexp.MustCompile(`^[a-z](?:[a-z0-9-]{0,28}[a-z0-9])?$`)

// CheckName returns ErrInvalidName unless name is a valid app name.
func CheckName(name string) error {
	if !namePattern.MatchString(name) {
		return ErrInvalidName
	}

	return nil
}

// CleanDisplayName trims a display name and returns ErrInvalidDisplayName
// unless what's left is 1 to 60 characters with no control characters,
// line or paragraph separators, or direction overrides, which could make a
// name span lines or read backwards in the app list.
func CleanDisplayName(displayName string) (string, error) {
	trimmed := strings.TrimSpace(displayName)
	length := utf8.RuneCountInString(trimmed)
	hasDisallowedCharacter := strings.ContainsFunc(trimmed, isDisallowedInDisplayName)

	if length == 0 || length > maxDisplayNameLength || hasDisallowedCharacter {
		return "", ErrInvalidDisplayName
	}

	return trimmed, nil
}

func isDisallowedInDisplayName(character rune) bool {
	return unicode.IsControl(character) ||
		unicode.In(character, unicode.Zl, unicode.Zp, unicode.Bidi_Control)
}
