// Package keyfiles manages the sealed master key files for root commands:
// sealing a new version, rotating to it, and setting aside files that no
// longer open. Each file is one version, sealed by systemd-creds and opened
// only by systemd when it starts the key service.
package keyfiles

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/sysexec"
)

const (
	// Dir is loaded whole by tero-keys.service's LoadCredentialEncrypted=.
	Dir = "/etc/tero/keys"
	// newDir holds a file while it is written, so Dir never has a partial
	// one, which would stop the key service from starting.
	newDir = "/etc/tero/keys.new"
	// UnreadableDir keeps files that stopped opening. They are never deleted.
	UnreadableDir = "/etc/tero/keys-unreadable"

	keyService = "tero-keys.service"
	keySocket  = "tero-keys.socket"
)

// Each file is named by its version. LoadCredentialEncrypted=kek:<dir>
// names the credential from a file kek_<file name>, so the name sealed into
// the file is kek_<version>.
var fileName = regexp.MustCompile(`^[1-9][0-9]{0,8}$`)

func credentialName(version int) string { return "kek_" + strconv.Itoa(version) }

// Files acts on the key files and the key service.
type Files struct {
	// Root is prefixed to every path; tests point it at a temporary
	// directory.
	Root string
	Run  sysexec.Runner
	// Seal encrypts key into path with systemd-creds under name. Tests
	// replace it.
	Seal func(ctx context.Context, name string, key []byte, path string) error
	// Opens reports whether systemd-creds can decrypt the file at path.
	Opens func(ctx context.Context, name, path string) bool
	Keys  *keys.Client
}

// Host is Files on the real server.
func Host() Files {
	return Files{
		Run:   sysexec.Host{},
		Seal:  sealWithSystemd,
		Opens: opensWithSystemd,
		Keys:  keys.NewClient(keys.SocketPath),
	}
}

func (f Files) path(hostPath string) string { return filepath.Join(f.Root, hostPath) }

// Versions lists the key versions on disk, oldest first.
func (f Files) Versions() ([]int, error) {
	return versionsIn(f.path(Dir))
}

func versionsIn(dir string) ([]int, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var versions []int
	for _, entry := range entries {
		if fileName.MatchString(entry.Name()) {
			version, _ := strconv.Atoi(entry.Name())
			versions = append(versions, version)
		}
	}
	slices.Sort(versions)

	return versions, nil
}

// nextVersion is one past every version ever sealed, including those set
// aside as unreadable, so a version number never names two keys and a value
// sealed with a lost key is reported as such instead of failing to open
// under a new key with the same number.
func (f Files) nextVersion() (int, error) {
	highest := 0
	versions, err := f.Versions()
	if err != nil {
		return 0, err
	}
	if len(versions) > 0 {
		highest = versions[len(versions)-1]
	}

	asides, err := os.ReadDir(f.path(UnreadableDir))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return 0, err
	}
	for _, aside := range asides {
		setAside, err := versionsIn(filepath.Join(f.path(UnreadableDir), aside.Name()))
		if err != nil {
			return 0, err
		}
		if len(setAside) > 0 {
			highest = max(highest, setAside[len(setAside)-1])
		}
	}

	return highest + 1, nil
}

// SealNext generates a new 256-bit master key, seals it as the next version
// and moves it into Dir in one rename. The plaintext only exists in this
// process and in systemd-creds' input.
func (f Files) SealNext(ctx context.Context) (int, error) {
	version, err := f.nextVersion()
	if err != nil {
		return 0, err
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return 0, err
	}
	defer clear(key)

	for _, dir := range []string{Dir, newDir} {
		if err := os.MkdirAll(f.path(dir), 0o700); err != nil {
			return 0, err
		}
	}

	name := credentialName(version)
	staged := filepath.Join(f.path(newDir), strconv.Itoa(version))
	os.Remove(staged)
	if err := f.Seal(ctx, name, key, staged); err != nil {
		return 0, fmt.Errorf("seal the master key: %w", err)
	}
	// A file systemd can't open would stop the key service from starting.
	if !f.Opens(ctx, name, staged) {
		os.Remove(staged)
		return 0, errors.New("systemd-creds can't decrypt the master key it just sealed")
	}
	if err := syncFile(staged); err != nil {
		return 0, err
	}
	if err := os.Rename(staged, filepath.Join(f.path(Dir), strconv.Itoa(version))); err != nil {
		return 0, err
	}

	return version, syncFile(f.path(Dir))
}

func syncFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	return file.Sync()
}

// Rotate seals a new master key, has the key service re-seal everything with
// it and removes the older files. Every step leaves files that open every
// stored value: a file is only removed once no value names its version.
func (f Files) Rotate(ctx context.Context) (int, error) {
	newest, err := f.SealNext(ctx)
	if err != nil {
		return 0, err
	}
	if err := f.restartKeyService(ctx); err != nil {
		return 0, err
	}

	inUse, err := f.Keys.RotateKey(ctx)
	if err != nil {
		return 0, fmt.Errorf("re-seal the stored values with the new key: %w", err)
	}

	versions, err := f.Versions()
	if err != nil {
		return 0, err
	}
	for _, version := range versions {
		isOld := version < newest && !slices.Contains(inUse, version)
		if isOld {
			if err := os.Remove(filepath.Join(f.path(Dir), strconv.Itoa(version))); err != nil {
				return 0, err
			}
		}
	}

	return newest, f.restartKeyService(ctx)
}

func (f Files) restartKeyService(ctx context.Context) error {
	_, err := f.Run.Run(ctx, "systemctl", "restart", keyService)
	return err
}

// Recover makes sure the key service answers. When systemd couldn't start it
// because a key file no longer decrypts (a cleared TPM, a disk moved to
// another server), it moves the unreadable files to UnreadableDir, seals a
// new key if none is left and starts the service again. It reports whether
// it replaced the key. Only the admin's credentials depend on the key, and
// the caller resets those anyway.
func (f Files) Recover(ctx context.Context) (bool, error) {
	if _, err := f.Keys.Status(ctx); !errors.Is(err, keys.ErrUnavailable) {
		return false, err
	}

	isCredentialFailure, err := f.failedOnCredentials(ctx)
	if err != nil {
		return false, err
	}
	if !isCredentialFailure {
		return false, errors.New("the key service isn't running, and not because of its key. See `journalctl -u tero-keys`")
	}

	if err := f.setAsideUnreadable(ctx); err != nil {
		return false, err
	}
	versions, err := f.Versions()
	if err != nil {
		return false, err
	}
	if len(versions) == 0 {
		if _, err := f.SealNext(ctx); err != nil {
			return false, err
		}
	}

	if _, err := f.Run.Run(ctx, "systemctl", "reset-failed", keyService, keySocket); err != nil {
		return false, err
	}
	if _, err := f.Run.Run(ctx, "systemctl", "restart", keySocket); err != nil {
		return false, err
	}
	if _, err := f.Keys.Status(ctx); err != nil {
		return false, fmt.Errorf("the key service still doesn't answer: %w", err)
	}

	return true, nil
}

// exitCredentials is the status systemd gives a service whose credentials
// couldn't be set up (EXIT_CREDENTIALS).
const exitCredentials = "243"

func (f Files) failedOnCredentials(ctx context.Context) (bool, error) {
	output, err := f.Run.Run(ctx, "systemctl", "show", keyService, "--property=Result,ExecMainStatus")
	if err != nil {
		return false, err
	}

	properties := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		name, value, _ := strings.Cut(strings.TrimSpace(line), "=")
		properties[name] = value
	}

	// After repeated failed starts systemd reports start-limit-hit instead
	// of exit-code, but keeps the status of the last run.
	return properties["ExecMainStatus"] == exitCredentials, nil
}

func (f Files) setAsideUnreadable(ctx context.Context) error {
	versions, err := f.Versions()
	if err != nil {
		return err
	}

	aside := filepath.Join(f.path(UnreadableDir), time.Now().UTC().Format("20060102T150405Z"))
	for _, version := range versions {
		path := filepath.Join(f.path(Dir), strconv.Itoa(version))
		if f.Opens(ctx, credentialName(version), path) {
			continue
		}

		if err := os.MkdirAll(aside, 0o700); err != nil {
			return err
		}
		if err := os.Rename(path, filepath.Join(aside, strconv.Itoa(version))); err != nil {
			return err
		}
	}

	return nil
}

// HasTPM reports whether systemd-creds seals with a TPM2 on this server.
func (f Files) HasTPM(ctx context.Context) bool {
	_, err := f.Run.Run(ctx, "systemd-creds", "has-tpm2", "--quiet")
	return err == nil
}

// sealWithSystemd runs systemd-creds encrypt with the key on stdin: TPM2 plus
// the host key when the server has a TPM2, the host key otherwise, and no
// PCR policy, so kernel and firmware updates don't lock the key.
func sealWithSystemd(ctx context.Context, name string, key []byte, path string) error {
	command := exec.CommandContext(ctx, "/usr/bin/systemd-creds", "encrypt",
		"--with-key=auto", "--tpm2-pcrs=", "--name="+name, "-", path)
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	command.Stdin = bytes.NewReader(key)

	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w\n%s", err, strings.TrimSpace(string(output)))
	}

	return os.Chmod(path, 0o600)
}

// opensWithSystemd decrypts path and throws the plaintext away.
func opensWithSystemd(ctx context.Context, name, path string) bool {
	command := exec.CommandContext(ctx, "/usr/bin/systemd-creds", "decrypt", "--name="+name, path, "-")
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}

	return command.Run() == nil
}
