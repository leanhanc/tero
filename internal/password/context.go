package password

import (
	"fmt"
	"strings"
	"unicode"
)

// minContextLabel is the shortest domain label treated as a context word.
// Shorter labels ("io", "app", "dev") are too common to forbid.
const minContextLabel = 4

// ContextWordError means a new password contains a word tied to this server
// (OWASP ASVS v5.0.0-6.1.2, v5.0.0-6.2.11).
type ContextWordError struct {
	Word string
}

func (e ContextWordError) Error() string {
	return fmt.Sprintf("Don't use %q in the password: it's tied to this server", e.Word)
}

// ContextWords are the words a password for the dashboard at domain, on the
// server with publicIP, must not contain: "tero", "admin", the domain, its
// labels of at least minContextLabel characters that aren't all digits, and
// the IP. All are lowercase.
func ContextWords(domain, publicIP string) []string {
	words := []string{"tero", "admin"}
	lowerDomain := strings.ToLower(domain)
	if lowerDomain != "" {
		words = append(words, lowerDomain)
	}

	for _, label := range strings.Split(lowerDomain, ".") {
		isLongEnough := len(label) >= minContextLabel
		if isLongEnough && !isAllDigits(label) {
			words = append(words, label)
		}
	}

	if publicIP != "" {
		words = append(words, publicIP)
	}

	return words
}

// firstContextWord returns the first context word password contains,
// ignoring case.
func firstContextWord(password string, words []string) (string, bool) {
	lowerPassword := strings.ToLower(password)
	for _, word := range words {
		if strings.Contains(lowerPassword, word) {
			return word, true
		}
	}

	return "", false
}

func isAllDigits(label string) bool {
	return strings.IndexFunc(label, func(r rune) bool { return !unicode.IsDigit(r) }) == -1
}
