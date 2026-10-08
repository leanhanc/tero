package initflow

import (
	"context"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/leanhanc/tero/internal/hostfs"
)

// sudoGroups are the groups Ubuntu's default sudoers grants full sudo to.
var sudoGroups = []string{"sudo", "admin"}

// checkSSHLockout refuses init when turning off SSH password login would leave
// nobody able to log in: at least one user who can run sudo needs an
// authorized SSH key.
func checkSSHLockout(ctx context.Context, h host) error {
	if !hasSSHServer(h.files) {
		return nil
	}

	keyFilePatterns, err := authorizedKeysPatterns(ctx, h)
	if err != nil {
		return err
	}

	candidates, err := sudoCapableAccounts(h.files, os.Getenv("SUDO_USER"))
	if err != nil {
		return err
	}

	for _, candidate := range candidates {
		if hasAuthorizedKey(h.files, candidate, keyFilePatterns) {
			return nil
		}
	}

	return fmt.Errorf(`Init turns off SSH password login, and no user who can run sudo has an SSH key yet.
Init stopped without changing anything, so you can't get locked out.

Add your public key for your sudo user, check that you can log in with it, then run init again:

  # on your computer
  ssh-copy-id <user>@<this-server>

  # back on the server
  sudo tero init`)
}

func hasSSHServer(files hostfs.FS) bool {
	return files.Exists("/usr/sbin/sshd")
}

// authorizedKeysPatterns returns sshd's effective AuthorizedKeysFile setting,
// e.g. [".ssh/authorized_keys", ".ssh/authorized_keys2"].
func authorizedKeysPatterns(ctx context.Context, h host) ([]string, error) {
	effectiveConfig, err := h.run.Run(ctx, "sshd", "-T")
	if err != nil {
		return nil, fmt.Errorf("read the SSH server's settings: %w", err)
	}

	for _, line := range strings.Split(effectiveConfig, "\n") {
		value, isKeysFileLine := strings.CutPrefix(line, "authorizedkeysfile ")
		if isKeysFileLine {
			return strings.Fields(value), nil
		}
	}

	return []string{".ssh/authorized_keys", ".ssh/authorized_keys2"}, nil
}

// sudoCapableAccounts lists the user who ran sudo plus every member of the
// sudo groups.
func sudoCapableAccounts(files hostfs.FS, sudoUser string) ([]account, error) {
	accounts, err := readAccounts(files)
	if err != nil {
		return nil, err
	}

	groups, err := readGroups(files)
	if err != nil {
		return nil, err
	}

	names := []string{}
	if sudoUser != "" && sudoUser != "root" {
		names = append(names, sudoUser)
	}
	for _, groupName := range sudoGroups {
		names = append(names, groups[groupName].members...)
	}

	var candidates []account
	for _, name := range names {
		candidate, isKnown := accounts[name]
		isDuplicate := slices.ContainsFunc(candidates, func(existing account) bool { return existing.name == name })
		if isKnown && !isDuplicate {
			candidates = append(candidates, candidate)
		}
	}

	return candidates, nil
}

func hasAuthorizedKey(files hostfs.FS, candidate account, patterns []string) bool {
	for _, pattern := range patterns {
		keysPath := expandKeysPattern(pattern, candidate)
		data, err := files.ReadFile(keysPath)
		if err == nil && containsKey(string(data)) {
			return true
		}
	}

	return false
}

// expandKeysPattern applies sshd's %h, %u and %% tokens. Relative paths are
// relative to the user's home directory.
func expandKeysPattern(pattern string, candidate account) string {
	replacer := strings.NewReplacer("%%", "%", "%h", candidate.home, "%u", candidate.name)
	expanded := replacer.Replace(pattern)

	if path.IsAbs(expanded) {
		return expanded
	}

	return path.Join(candidate.home, expanded)
}

func containsKey(authorizedKeys string) bool {
	for _, line := range strings.Split(authorizedKeys, "\n") {
		trimmed := strings.TrimSpace(line)
		isKeyLine := trimmed != "" && !strings.HasPrefix(trimmed, "#")
		if isKeyLine {
			return true
		}
	}

	return false
}
