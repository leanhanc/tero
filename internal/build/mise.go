package build

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/railwayapp/railpack/core/mise"
)

// Railpack runs mise from a fixed directory under /tmp, which the planner
// must be able to write to. A planner taken over through one app's
// repository could replace the binary there and run its code in the next
// app's plan, with that app's source and build variables. So the agent keeps
// its own copy of mise, checked against a pinned hash, out of the planner's
// reach, and every plan starts in a new directory with a fresh copy of it.

// miseSHA256 pins the mise binary of each Railpack release's mise version,
// by architecture. The hashes are of mise/bin/mise inside the release
// archives that mise's SHASUMS256.txt lists. A Railpack update that moves to
// another mise version fails TestMiseVersionIsPinned until it is added here.
var miseSHA256 = map[string]map[string]string{
	"2026.10.8": {
		"amd64": "043dc6b9077db988269ca2f85059851cb09fc080355350021363e95a5e227f84",
		"arm64": "d9c5447ad5084b7bf9705ef2913dfad97268235f15849a51e88a21ede7e7204a",
	},
}

// misePlatforms are mise's release names for each architecture.
var misePlatforms = map[string]string{"amd64": "linux-x64-musl", "arm64": "linux-arm64-musl"}

// miseReleaseBase is a var so tests can serve the archive locally.
var miseReleaseBase = "https://github.com/jdx/mise/releases/download"

// maxMiseSize bounds the archive and the binary read from it.
const maxMiseSize = 256 << 20

// miseBinaryName is the file name Railpack looks for in its mise directory.
func miseBinaryName() string {
	return "mise-" + mise.Version
}

// pristineMiseDir is where the agent keeps its checked copy: in its own home,
// which the planner can't reach.
func pristineMiseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(home, ".cache", "tero", "mise"), nil
}

// ensurePristineMise returns the path of the agent's copy of mise, downloading
// it if it is missing and refusing one whose hash doesn't match the pin.
func ensurePristineMise(ctx context.Context) (string, error) {
	want, err := pinnedMiseHash()
	if err != nil {
		return "", err
	}
	dir, err := pristineMiseDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, miseBinaryName())

	got, err := fileSHA256(path)
	if err == nil && got == want {
		return path, nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := downloadMise(ctx, dir, path, want); err != nil {
		return "", fmt.Errorf("download mise %s for the build planner: %w", mise.Version, err)
	}

	return path, nil
}

func pinnedMiseHash() (string, error) {
	want := miseSHA256[mise.Version][runtime.GOARCH]
	if want == "" {
		return "", fmt.Errorf("no pinned hash for mise %s on %s", mise.Version, runtime.GOARCH)
	}

	return want, nil
}

// downloadMise extracts mise from its release archive into dir and moves it to
// path only once its hash matches want.
func downloadMise(ctx context.Context, dir, path, want string) error {
	platform := misePlatforms[runtime.GOARCH]
	url := fmt.Sprintf("%s/v%s/mise-v%s-%s.tar.gz", miseReleaseBase, mise.Version, mise.Version, platform)

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub answered %s", response.Status)
	}

	gzipReader, err := gzip.NewReader(io.LimitReader(response.Body, maxMiseSize))
	if err != nil {
		return err
	}
	archive := tar.NewReader(gzipReader)
	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return errors.New("the archive has no mise/bin/mise")
		}
		if err != nil {
			return err
		}
		if header.Name == "mise/bin/mise" && header.Typeflag == tar.TypeReg {
			return writeCheckedFile(dir, path, io.LimitReader(archive, maxMiseSize), want)
		}
	}
}

// writeCheckedFile writes content to a new file in dir and moves it to path
// only when its hash is want.
func writeCheckedFile(dir, path string, content io.Reader, want string) error {
	temporary, err := os.CreateTemp(dir, ".mise-*")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())

	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(temporary, hash), content)
	closeErr := temporary.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return err
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != want {
		return fmt.Errorf("its hash is %s, not the pinned %s", got, want)
	}
	if err := os.Chmod(temporary.Name(), 0o700); err != nil {
		return err
	}

	return os.Rename(temporary.Name(), path)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hash.Sum(nil)), nil
}

// preparePlanDirectory empties Railpack's mise directory and puts a fresh copy
// of the agent's mise in it, so nothing an earlier plan left there is used.
// Builds run one at a time, so no other plan is using it. A directory another
// user created there is never used: Railpack would run the binary it finds.
func preparePlanDirectory(pristine string) error {
	if err := removePlanDirectory(); err != nil {
		return err
	}

	// Mkdir fails if another user created the directory since the removal.
	if err := os.Mkdir(miseRoot(), 0o700); err != nil {
		return fmt.Errorf("create %s: %w", miseRoot(), err)
	}
	if err := os.Mkdir(mise.InstallDir, 0o700); err != nil {
		return err
	}

	return copyExecutable(pristine, filepath.Join(mise.InstallDir, miseBinaryName()))
}

// removePlanDirectory deletes Railpack's mise directory, if the agent owns it.
// The planner can chmod what it left there so that it can't be deleted, which
// would stop every later build, so the directory is first moved aside under a
// new name, then made writable again and deleted as far as it can be.
func removePlanDirectory() error {
	parent := miseRoot()
	info, err := os.Lstat(parent)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}

	stat, hasOwner := info.Sys().(*syscall.Stat_t)
	isOwnDirectory := info.IsDir() && hasOwner && int(stat.Uid) == os.Getuid()
	if !isOwnDirectory {
		return fmt.Errorf("%s exists but doesn't belong to the agent's user; remove it and redeploy", parent)
	}

	aside := fmt.Sprintf("%s.old-%d", parent, time.Now().UnixNano())
	if err := os.Rename(parent, aside); err != nil {
		return err
	}
	removeOwnTree(aside)

	return nil
}

// removeOwnTree deletes path, making each directory in it writable and
// searchable first. It never follows a link, and leaves whatever it can't
// delete: path was already moved out of the way.
func removeOwnTree(path string) {
	info, err := os.Lstat(path)
	if err != nil {
		return
	}
	if info.IsDir() {
		_ = os.Chmod(path, 0o700)
		entries, _ := os.ReadDir(path)
		for _, entry := range entries {
			removeOwnTree(filepath.Join(path, entry.Name()))
		}
	}
	_ = os.Remove(path)
}

func copyExecutable(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()

	output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)

	return errors.Join(copyErr, output.Close())
}

// miseRoot is the directory Railpack keeps mise in, and the only one the
// planner may write to.
func miseRoot() string {
	return strings.TrimSuffix(mise.InstallDir, "/mise")
}
