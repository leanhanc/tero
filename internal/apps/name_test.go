package apps

import "testing"

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

func TestSuggestName(t *testing.T) {
	cases := map[string]string{
		"tero":        "tero",
		"My_Cool.API": "my-cool-api",
		"2048-game":   "app-2048-game",
		"___":         "",
		"a-very-long-repository-name-that-goes-on": "a-very-long-repository-name-th",
	}
	for input, want := range cases {
		if got := suggestName(input); got != want {
			t.Errorf("suggestName(%q) = %q, want %q", input, got, want)
		}
	}
}
