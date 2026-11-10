package initflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/leanhanc/tero/internal/hostfs"
)

const sshdConfigPath = "/etc/ssh/sshd_config"

// maxSSHIncludeDepth is sshd's own limit on nested Include directives.
const maxSSHIncludeDepth = 16

// matchSSHSettings are requiredSSHSettings with the deprecated alias sshd
// still accepts in a Match block for kbdinteractiveauthentication.
var matchSSHSettings = map[string]string{
	"permitrootlogin":                 "no",
	"passwordauthentication":          "no",
	"kbdinteractiveauthentication":    "no",
	"challengeresponseauthentication": "no",
	"permitemptypasswords":            "no",
}

// findMatchOverride reads sshd's configuration and its includes for a Match
// block that sets one of requiredSSHSettings to another value. `sshd -T`
// applies Match blocks only for the connection it is told about, so a block
// for another user or address would slip past checkEffectiveSSH (OpenSSH
// sshd_config(5), Match; CIS Ubuntu Linux Benchmark 5.1). A block that ends
// with an included file is assumed to go on, which errs towards refusing.
func findMatchOverride(files hostfs.FS) (string, error) {
	if !files.Exists(sshdConfigPath) {
		return "", nil
	}

	override, _, err := scanSSHConfig(files, sshdConfigPath, false, 0)
	return override, err
}

// scanSSHConfig scans one configuration file, starting inside a Match block
// when the Include that named it was in one. It returns the first override
// found and whether the file ends inside a Match block.
func scanSSHConfig(files hostfs.FS, path string, isInMatch bool, depth int) (string, bool, error) {
	if depth > maxSSHIncludeDepth {
		return "", isInMatch, errors.New("sshd's configuration nests Include deeper than sshd allows")
	}
	content, err := files.ReadFile(path)
	if err != nil {
		return "", isInMatch, fmt.Errorf("read %s: %w", path, err)
	}

	for _, line := range strings.Split(string(content), "\n") {
		keyword, value := parseSSHConfigLine(line)
		switch {
		case keyword == "match":
			isInMatch = true
		case keyword == "include":
			override, endsInMatch, err := scanSSHIncludes(files, value, isInMatch, depth)
			if err != nil || override != "" {
				return override, isInMatch, err
			}
			isInMatch = isInMatch || endsInMatch
		case isInMatch:
			want, isRequired := matchSSHSettings[keyword]
			isOverride := isRequired && !strings.EqualFold(value, want)
			if isOverride {
				return fmt.Sprintf("a Match block in %s sets %s to %q instead of %q", path, keyword, value, want), isInMatch, nil
			}
		}
	}

	return "", isInMatch, nil
}

// scanSSHIncludes scans every file an Include names, in the lexical order
// sshd reads them. Relative paths are under /etc/ssh.
func scanSSHIncludes(files hostfs.FS, patterns string, isInMatch bool, depth int) (string, bool, error) {
	endsInMatch := false
	for _, pattern := range strings.Fields(patterns) {
		pattern = strings.Trim(pattern, `"`)
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join("/etc/ssh", pattern)
		}
		matches, err := filepath.Glob(files.Path(pattern))
		if err != nil {
			return "", endsInMatch, err
		}

		for _, match := range matches {
			hostPath, err := filepath.Rel(files.Path("/"), match)
			if err != nil {
				return "", endsInMatch, err
			}
			info, err := os.Stat(match)
			if err != nil || info.IsDir() {
				continue
			}

			override, fileEndsInMatch, err := scanSSHConfig(files, "/"+hostPath, isInMatch, depth+1)
			if err != nil || override != "" {
				return override, endsInMatch, err
			}
			endsInMatch = endsInMatch || fileEndsInMatch
		}
	}

	return "", endsInMatch, nil
}

// parseSSHConfigLine returns a line's keyword, lowercased as sshd treats it,
// and its value, with an optional "=" between them, a trailing comment and
// quotes around a single value removed. Comments and blank lines have no
// keyword.
func parseSSHConfigLine(line string) (string, string) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", ""
	}

	separator := strings.IndexAny(trimmed, " \t=")
	if separator < 0 {
		return strings.ToLower(trimmed), ""
	}
	keyword := strings.ToLower(trimmed[:separator])
	value := strings.TrimLeft(trimmed[separator:], " \t=")
	value, _, _ = strings.Cut(value, " #")
	value = strings.TrimSpace(value)
	if keyword != "include" {
		value = strings.Trim(value, `"`)
	}

	return keyword, value
}
