package initflow

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"syscall"

	"golang.org/x/crypto/ssh"

	"github.com/leanhanc/tero/internal/hostfs"
)

// sudoGroups are the groups Ubuntu's default sudoers grants full sudo to.
var sudoGroups = []string{"sudo", "admin"}

// checkSSHLockout refuses init when turning off SSH password and root login
// could lock out the admin running it. The admin must run init through sudo
// from their own account, and the SSH server must accept a key for that
// account: a real key, in a file the server reads, for a user it lets in.
func checkSSHLockout(ctx context.Context, h host) error {
	if !hasSSHServer(h.files) {
		return nil
	}

	sudoUser := os.Getenv("SUDO_USER")
	isRootLogin := sudoUser == "" || sudoUser == "root"
	if isRootLogin {
		return errors.New(`Init turns off SSH login for root, and you're logged in as root.
Init stopped without changing anything, so you can't get locked out.

Log in as a user who can run sudo and has an SSH key, then run init from there:

  sudo tero init`)
	}

	accounts, err := readAccounts(h.files)
	if err != nil {
		return err
	}
	admin, isKnown := accounts[sudoUser]
	if !isKnown {
		return fmt.Errorf("the user who ran sudo (%s) isn't in /etc/passwd", sudoUser)
	}

	problem, err := sshLoginProblem(ctx, h, admin)
	if err != nil {
		return err
	}
	if problem == "" {
		return nil
	}

	return fmt.Errorf(`Init turns off SSH password login, and the SSH server wouldn't accept a key for %s yet: %s.
Init stopped without changing anything, so you can't get locked out.

Add your public key, check that you can log in with it, then run init again:

  # on your computer
  ssh-copy-id %s@<this-server>

  # back on the server
  sudo tero init`, admin.name, problem, admin.name)
}

func hasSSHServer(files hostfs.FS) bool {
	return files.Exists("/usr/sbin/sshd")
}

// sshLoginProblem explains why the SSH server wouldn't let candidate in with
// a key, or returns "" when it would.
func sshLoginProblem(ctx context.Context, h host, candidate account) (string, error) {
	isNoLoginShell := strings.HasSuffix(candidate.shell, "/nologin") || strings.HasSuffix(candidate.shell, "/false")
	if isNoLoginShell {
		return "their login shell is " + candidate.shell, nil
	}

	settings, err := userSSHSettings(ctx, h, candidate.name)
	if err != nil {
		return "", err
	}
	if settings.first("pubkeyauthentication") == "no" {
		return "key login is turned off for them (PubkeyAuthentication)", nil
	}

	groupNames, err := groupsOf(h.files, candidate)
	if err != nil {
		return "", err
	}
	if !settings.allowsUser(candidate.name, groupNames) {
		return "AllowUsers, DenyUsers, AllowGroups or DenyGroups keeps them out", nil
	}

	return authorizedKeyProblem(h.files, candidate, settings)
}

// sshSettings is `sshd -T` output: each key with every value it was given.
type sshSettings map[string][]string

// userSSHSettings is the effective sshd configuration for user, with Match
// blocks applied.
func userSSHSettings(ctx context.Context, h host, user string) (sshSettings, error) {
	output, err := h.run.Run(ctx, "sshd", "-T", "-C", "user="+user+",host=localhost,addr=127.0.0.1")
	if err != nil {
		return nil, fmt.Errorf("read the SSH server's settings: %w", err)
	}

	return parseSSHSettings(output), nil
}

func parseSSHSettings(output string) sshSettings {
	settings := sshSettings{}
	for _, line := range strings.Split(output, "\n") {
		key, value, _ := strings.Cut(strings.TrimSpace(line), " ")
		if key != "" {
			settings[key] = append(settings[key], strings.Fields(value)...)
		}
	}

	return settings
}

func (s sshSettings) first(key string) string {
	if len(s[key]) == 0 {
		return ""
	}
	return s[key][0]
}

// allowsUser applies sshd's DenyUsers, AllowUsers, DenyGroups and AllowGroups
// in that order. A user@host pattern counts as matching the user whatever
// the host, which errs towards refusing init rather than locking anyone out.
func (s sshSettings) allowsUser(user string, groupNames []string) bool {
	if matchesAnyUser(s["denyusers"], user) {
		return false
	}
	if len(s["allowusers"]) > 0 && !matchesAnyAllowedUser(s["allowusers"], user) {
		return false
	}
	if matchesAnyGroup(s["denygroups"], groupNames) {
		return false
	}
	if len(s["allowgroups"]) > 0 && !matchesAnyGroup(s["allowgroups"], groupNames) {
		return false
	}

	return true
}

func matchesAnyUser(patterns []string, user string) bool {
	for _, pattern := range patterns {
		userPattern, _, _ := strings.Cut(pattern, "@")
		if isMatch, _ := path.Match(userPattern, user); isMatch {
			return true
		}
	}

	return false
}

// matchesAnyAllowedUser only counts user@host patterns whose host part
// matches anything, since the admin's address isn't known here.
func matchesAnyAllowedUser(patterns []string, user string) bool {
	for _, pattern := range patterns {
		userPattern, hostPattern, hasHost := strings.Cut(pattern, "@")
		isAnyHost := !hasHost || hostPattern == "*"
		if isMatch, _ := path.Match(userPattern, user); isMatch && isAnyHost {
			return true
		}
	}

	return false
}

func matchesAnyGroup(patterns, groupNames []string) bool {
	for _, pattern := range patterns {
		for _, groupName := range groupNames {
			if isMatch, _ := path.Match(pattern, groupName); isMatch {
				return true
			}
		}
	}

	return false
}

// authorizedKeyProblem returns "" when one of candidate's authorized-keys
// files is one sshd will read and holds a key it will accept for a login.
func authorizedKeyProblem(files hostfs.FS, candidate account, settings sshSettings) (string, error) {
	isStrict := settings.first("strictmodes") != "no"
	acceptedAlgorithms := strings.Split(settings.first("pubkeyacceptedalgorithms"), ",")

	patterns := settings["authorizedkeysfile"]
	if len(patterns) == 0 {
		patterns = []string{".ssh/authorized_keys", ".ssh/authorized_keys2"}
	}

	problem := "they have no authorized_keys file"
	for _, pattern := range patterns {
		keysPath := expandKeysPattern(pattern, candidate)
		data, err := files.ReadFile(keysPath)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}

		if isStrict {
			if unsafePath := unsafeKeysPath(files, keysPath, candidate); unsafePath != "" {
				problem = unsafePath + " can be changed by other users, so the SSH server ignores " + keysPath + " (StrictModes)"
				continue
			}
		}

		if hasUsableKey(string(data), acceptedAlgorithms) {
			return "", nil
		}
		problem = keysPath + " has no key the SSH server accepts for a login"
	}

	return problem, nil
}

// unsafeKeysPath returns the first of the keys file and its directories, up to
// the home directory, that sshd's StrictModes would reject: not owned by the
// user or root, or writable by group or others. It returns "" when all pass.
func unsafeKeysPath(files hostfs.FS, keysPath string, candidate account) string {
	current := keysPath
	for {
		info, err := os.Stat(files.Path(current))
		if err != nil {
			return current
		}

		owner := ownerUID(info)
		isOwnedSafely := owner == candidate.uid || owner == 0
		isWritableByOthers := info.Mode().Perm()&0o022 != 0
		if !isOwnedSafely || isWritableByOthers {
			return current
		}

		isLastChecked := current == candidate.home || current == "/"
		if isLastChecked {
			return ""
		}
		current = path.Dir(current)
	}
}

func ownerUID(info fs.FileInfo) int {
	stat, isUnixStat := info.Sys().(*syscall.Stat_t)
	if !isUnixStat {
		return -1
	}
	return int(stat.Uid)
}

// hasUsableKey reports whether authorizedKeys holds a key sshd accepts and
// that allows a shell: keys with a forced command don't.
func hasUsableKey(authorizedKeys string, acceptedAlgorithms []string) bool {
	for _, line := range strings.Split(authorizedKeys, "\n") {
		key, _, options, _, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil {
			continue
		}

		hasForcedCommand := slices.ContainsFunc(options, func(option string) bool {
			return strings.HasPrefix(strings.ToLower(option), "command=")
		})
		if !hasForcedCommand && isAcceptedKeyType(key.Type(), acceptedAlgorithms) {
			return true
		}
	}

	return false
}

// isAcceptedKeyType maps a key type to the signature algorithms that use it:
// an "ssh-rsa" key logs in with rsa-sha2-256 or rsa-sha2-512.
func isAcceptedKeyType(keyType string, acceptedAlgorithms []string) bool {
	isUnknownList := len(acceptedAlgorithms) == 0 || acceptedAlgorithms[0] == ""
	if isUnknownList {
		return true
	}

	candidates := []string{keyType}
	switch keyType {
	case ssh.KeyAlgoRSA:
		candidates = append(candidates, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSASHA512)
	case ssh.CertAlgoRSAv01:
		candidates = append(candidates, ssh.CertAlgoRSASHA256v01, ssh.CertAlgoRSASHA512v01)
	}

	for _, candidate := range candidates {
		if slices.Contains(acceptedAlgorithms, candidate) {
			return true
		}
	}

	return false
}

// groupsOf lists the names of candidate's primary and supplementary groups.
func groupsOf(files hostfs.FS, candidate account) ([]string, error) {
	groups, err := readGroups(files)
	if err != nil {
		return nil, err
	}

	var names []string
	for name, entry := range groups {
		isPrimary := entry.gid == candidate.gid
		if isPrimary || slices.Contains(entry.members, candidate.name) {
			names = append(names, name)
		}
	}

	return names, nil
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
