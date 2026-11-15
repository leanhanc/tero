package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/railwayapp/railpack/core/mise"
)

// build-pipeline: mise pinned. Railpack's mise version has a pinned hash on
// both released architectures.
func TestMiseVersionIsPinned(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		if miseSHA256[mise.Version][arch] == "" {
			t.Errorf("no pinned hash for mise %s on %s", mise.Version, arch)
		}
		if misePlatforms[arch] == "" {
			t.Errorf("no mise platform for %s", arch)
		}
	}
}

func TestEnsurePristineMiseRefusesAnotherBinary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	planted := []byte("#!/bin/sh\necho planted\n")
	serveMiseArchive(t, planted)

	if _, err := ensurePristineMise(context.Background()); err == nil {
		t.Fatal("a binary with another hash was accepted")
	}
	dir, _ := pristineMiseDir()
	if _, err := os.Stat(filepath.Join(dir, miseBinaryName())); !os.IsNotExist(err) {
		t.Fatalf("the refused binary was kept: %v", err)
	}
}

func TestEnsurePristineMiseReplacesAChangedCopy(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	binary := []byte("the real mise")
	sum := sha256.Sum256(binary)
	pinTestHash(t, hex.EncodeToString(sum[:]))
	serveMiseArchive(t, binary)

	dir, _ := pristineMiseDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, miseBinaryName())
	if err := os.WriteFile(path, []byte("changed"), 0o700); err != nil {
		t.Fatal(err)
	}

	got, err := ensurePristineMise(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	content, _ := os.ReadFile(got)
	if !bytes.Equal(content, binary) {
		t.Fatalf("copy = %q, want the pinned binary", content)
	}
}

func pinTestHash(t *testing.T, hash string) {
	t.Helper()
	saved := miseSHA256
	miseSHA256 = map[string]map[string]string{mise.Version: {"amd64": hash, "arm64": hash}}
	t.Cleanup(func() { miseSHA256 = saved })
}

func serveMiseArchive(t *testing.T, binary []byte) {
	t.Helper()
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	tarWriter.WriteHeader(&tar.Header{Name: "mise/bin/mise", Mode: 0o755, Size: int64(len(binary)), Typeflag: tar.TypeReg})
	tarWriter.Write(binary)
	tarWriter.Close()
	gzipWriter.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(archive.Bytes())
	}))
	t.Cleanup(server.Close)
	saved := miseReleaseBase
	miseReleaseBase = server.URL
	t.Cleanup(func() { miseReleaseBase = saved })
}

// Review finding: the planner could chmod its directory so that it couldn't
// be deleted, which would stop every later build.
func TestRemoveOwnTreeDeletesALockedTree(t *testing.T) {
	root := filepath.Join(t.TempDir(), "railpack")
	locked := filepath.Join(root, "mise", "cache")
	if err := os.MkdirAll(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(locked, "file"), []byte("x"), 0o600)
	os.Chmod(locked, 0)
	os.Chmod(filepath.Join(root, "mise"), 0o500)
	os.Chmod(root, 0)

	removeOwnTree(root)

	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("the locked tree is still there: %v", err)
	}
}
