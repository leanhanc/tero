// Package hostfs reads and writes host files relative to a root directory:
// "/" on a real server, a temporary directory in unit tests.
package hostfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// FS is a view of the host filesystem rooted at Root.
type FS struct {
	Root string
}

// Host is the real filesystem.
var Host = FS{Root: "/"}

// Path maps an absolute host path into this FS.
func (f FS) Path(hostPath string) string {
	return filepath.Join(f.Root, hostPath)
}

// ReadFile reads a host file.
func (f FS) ReadFile(hostPath string) ([]byte, error) {
	return os.ReadFile(f.Path(hostPath))
}

// Exists reports whether a host path exists.
func (f FS) Exists(hostPath string) bool {
	_, err := os.Stat(f.Path(hostPath))
	return err == nil
}

// WriteFile atomically writes a host file with the given mode, creating
// parent directories (mode 0755) as needed. It reports whether the contents
// or mode changed, so callers can skip reloads when nothing did.
func (f FS) WriteFile(hostPath string, data []byte, mode fs.FileMode) (bool, error) {
	target := f.Path(hostPath)

	isUnchanged, err := hasContentsAndMode(target, data, mode)
	if err != nil {
		return false, err
	}
	if isUnchanged {
		return false, nil
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return false, fmt.Errorf("create directory for %s: %w", hostPath, err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(target), ".tero-*")
	if err != nil {
		return false, fmt.Errorf("write %s: %w", hostPath, err)
	}
	defer os.Remove(temporary.Name())

	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		return false, fmt.Errorf("write %s: %w", hostPath, err)
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return false, fmt.Errorf("chmod %s: %w", hostPath, err)
	}
	if err := temporary.Close(); err != nil {
		return false, fmt.Errorf("write %s: %w", hostPath, err)
	}
	if err := os.Rename(temporary.Name(), target); err != nil {
		return false, fmt.Errorf("write %s: %w", hostPath, err)
	}

	return true, nil
}

func hasContentsAndMode(path string, data []byte, mode fs.FileMode) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	existing, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}

	isSameMode := info.Mode().Perm() == mode.Perm()
	return isSameMode && string(existing) == string(data), nil
}
