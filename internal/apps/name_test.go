package apps

import (
	"strings"
	"testing"
)

// apps: Invalid name
func TestCheckName(t *testing.T) {
	for _, name := range []string{"a", "api", "my-app-2", "abcdefghijklmnopqrstuvwxyz1234"} {
		if err := CheckName(name); err != nil {
			t.Errorf("CheckName(%q) = %v", name, err)
		}
	}
	for _, name := range []string{"", "Api", "2api", "api-", "my.app", "my_app", "abcdefghijklmnopqrstuvwxyz12345", "-api"} {
		if err := CheckName(name); err == nil {
			t.Errorf("CheckName(%q) accepted", name)
		}
	}
}

// apps: Display names
func TestCleanDisplayName(t *testing.T) {
	cases := map[string]string{
		"My Shop API":           "My Shop API",
		"  Tienda  ":            "Tienda",
		"Café ☕":                "Café ☕",
		strings.Repeat("é", 60): strings.Repeat("é", 60),
	}
	for input, want := range cases {
		if got, err := CleanDisplayName(input); err != nil || got != want {
			t.Errorf("CleanDisplayName(%q) = %q, %v, want %q", input, got, err, want)
		}
	}
	for _, displayName := range []string{"", "   ", "two\nlines", "tab\there", "line\u2028separator", "\u202eipa-pohs", strings.Repeat("a", 61)} {
		if _, err := CleanDisplayName(displayName); err == nil {
			t.Errorf("CleanDisplayName(%q) accepted", displayName)
		}
	}
}
