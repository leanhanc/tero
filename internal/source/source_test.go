package source

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// apps: Not a GitHub URL (v5.0.0-1.3.6). Rejection happens before any
// request is built, so no other host can be contacted.
func TestParseRepository_v5_0_0_1_3_6_OnlyGitHub(t *testing.T) {
	accepted := map[string]Repository{
		"https://github.com/leanhanc/tero":      {"leanhanc", "tero"},
		"https://github.com/leanhanc/tero.git":  {"leanhanc", "tero"},
		"https://github.com/leanhanc/tero/":     {"leanhanc", "tero"},
		" https://github.com/Owner-1/my.repo_x": {"Owner-1", "my.repo_x"},
	}
	for raw, want := range accepted {
		got, err := ParseRepository(raw)
		if err != nil || got != want {
			t.Errorf("ParseRepository(%q) = %+v, %v; want %+v", raw, got, err, want)
		}
	}

	rejected := []string{
		"http://github.com/leanhanc/tero",
		"https://gitlab.com/leanhanc/tero",
		"https://github.com.evil.example/leanhanc/tero",
		"https://user:pass@github.com/leanhanc/tero",
		"https://github.com:8443/leanhanc/tero",
		"https://github.com/leanhanc",
		"https://github.com/leanhanc/tero/tree/main",
		"https://github.com/leanhanc/..",
		"https://github.com/-bad/tero",
		"https://github.com/leanhanc/tero?x=1",
		"git@github.com:leanhanc/tero.git",
		"https://169.254.169.254/latest/meta-data",
		"file:///etc/passwd",
	}
	for _, raw := range rejected {
		if _, err := ParseRepository(raw); !errors.Is(err, ErrNotGitHubURL) {
			t.Errorf("ParseRepository(%q) error = %v, want ErrNotGitHubURL", raw, err)
		}
	}
}

// apps: Path outside the repository
func TestCleanRootDirectory(t *testing.T) {
	accepted := map[string]string{"": "", ".": "", "e2e/fixtures/bun": "e2e/fixtures/bun", "api/": "api", "./api": "api"}
	for input, want := range accepted {
		got, err := cleanRootDirectory(input)
		if err != nil || got != want {
			t.Errorf("cleanRootDirectory(%q) = %q, %v; want %q", input, got, err, want)
		}
	}

	for _, input := range []string{"../", "/etc", "api/../../x", `api\x`} {
		if _, err := cleanRootDirectory(input); !errors.Is(err, ErrFolderNotFound) {
			t.Errorf("cleanRootDirectory(%q) error = %v, want ErrFolderNotFound", input, err)
		}
	}
}

// A symlink in the repository must not let the build read outside the
// checkout.
func TestBuildDirectoryRefusesSymlinkOutOfCheckout(t *testing.T) {
	checkout := t.TempDir()
	if err := os.Mkdir(filepath.Join(checkout, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(checkout, "escape")); err != nil {
		t.Fatal(err)
	}

	if _, err := BuildDirectory(checkout, "api"); err != nil {
		t.Errorf("BuildDirectory(api) = %v", err)
	}
	if _, err := BuildDirectory(checkout, "escape"); !errors.Is(err, ErrFolderNotFound) {
		t.Errorf("BuildDirectory(escape) error = %v, want ErrFolderNotFound", err)
	}
	if _, err := BuildDirectory(checkout, "missing"); !errors.Is(err, ErrFolderNotFound) {
		t.Errorf("BuildDirectory(missing) error = %v, want ErrFolderNotFound", err)
	}
}
