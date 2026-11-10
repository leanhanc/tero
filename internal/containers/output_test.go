package containers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Audit finding TERO-07: Podman kept app output on disk.
func TestOutputStaysOnTmpfsAndLeftoversGo(t *testing.T) {
	dir := t.TempDir()
	original := runtimeDir
	runtimeDir = func() string { return dir }
	t.Cleanup(func() { runtimeDir = original })

	kept, left := Name("shop", 1), Name("shop", 2)
	keptPath, err := outputPath(kept)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(keptPath, dir+string(filepath.Separator)) {
		t.Errorf("output path %q is outside the runtime directory", keptPath)
	}
	for _, invalid := range []string{"", "../../x", "tero-app-shop-1/../../x", "other"} {
		if _, err := outputPath(invalid); err == nil {
			t.Errorf("gave a path for %q", invalid)
		}
	}

	if err := prepareOutputDir(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(outputDir())
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("output directory: %v, %v", info, err)
	}
	leftPath, _ := outputPath(left)
	for _, path := range []string{keptPath, leftPath} {
		if err := os.WriteFile(path, []byte("output"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := RemoveOutputsExcept(map[string]bool{kept: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keptPath); err != nil {
		t.Errorf("kept output removed: %v", err)
	}
	if _, err := os.Stat(leftPath); !os.IsNotExist(err) {
		t.Errorf("leftover output kept: %v", err)
	}
	if err := RemoveOutput(kept); err != nil {
		t.Fatal(err)
	}
	if err := RemoveOutput(kept); err != nil {
		t.Errorf("removing missing output: %v", err)
	}
}
