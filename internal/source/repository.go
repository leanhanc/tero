// Package source fetches app source code from public GitHub repositories.
// Only github.com is ever contacted: a repository is parsed into an owner and
// a name, and every URL Tero requests is built from those on a fixed host, so
// an app's settings can't point the server at another address (ASVS
// v5.0.0-1.3.6).
package source

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// ErrNotGitHubURL is shown when the admin enters anything other than a
// public GitHub repository URL.
var ErrNotGitHubURL = errors.New("Enter a public GitHub repository URL, like https://github.com/owner/repository")

// GitHub's own limits: owners are letters, digits and single hyphens; names
// also allow dots and underscores.
var (
	ownerPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9]|-[A-Za-z0-9]){0,38}$`)
	namePattern  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

// Repository is a GitHub repository by owner and name.
type Repository struct {
	Owner string
	Name  string
}

// ParseRepository accepts https://github.com/<owner>/<name>, with an
// optional ".git" or trailing slash, and nothing else.
func ParseRepository(raw string) (Repository, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return Repository{}, ErrNotGitHubURL
	}

	isPlainGitHubURL := parsed.Scheme == "https" && parsed.Host == "github.com" &&
		parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
	if !isPlainGitHubURL {
		return Repository{}, ErrNotGitHubURL
	}

	path := strings.TrimSuffix(strings.TrimPrefix(parsed.Path, "/"), "/")
	owner, name, hasBothParts := strings.Cut(path, "/")
	name = strings.TrimSuffix(name, ".git")

	isValid := hasBothParts && ownerPattern.MatchString(owner) && namePattern.MatchString(name) &&
		name != "." && name != ".."
	if !isValid {
		return Repository{}, ErrNotGitHubURL
	}

	return Repository{Owner: owner, Name: name}, nil
}

// ErrInvalidOwner rejects a name GitHub wouldn't accept for a user or
// organization.
var ErrInvalidOwner = errors.New("That isn't a valid GitHub user or organization name")

// CheckOwner applies GitHub's rules for user and organization names.
func CheckOwner(owner string) error {
	if !ownerPattern.MatchString(owner) {
		return ErrInvalidOwner
	}

	return nil
}

// URL is the repository's canonical HTTPS address, the only form Tero stores
// and fetches.
func (r Repository) URL() string {
	return "https://github.com/" + r.Owner + "/" + r.Name
}

// cloneURL is what git talks to.
func (r Repository) cloneURL() string {
	if localURL, isLocal := testOverride(r); isLocal {
		return localURL
	}

	return r.URL() + ".git"
}

// ErrInvalidBranch rejects branch names git itself wouldn't accept.
var ErrInvalidBranch = errors.New("That isn't a valid branch name")

var branchPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,200}$`)

// CheckBranchName applies git's ref name rules that matter here: no "..",
// no leading dash or slash, no trailing slash, dot or ".lock".
func CheckBranchName(branch string) error {
	isValid := branchPattern.MatchString(branch) &&
		!strings.Contains(branch, "..") && !strings.Contains(branch, "//") &&
		!strings.HasPrefix(branch, "-") && !strings.HasPrefix(branch, "/") &&
		!strings.HasSuffix(branch, "/") && !strings.HasSuffix(branch, ".") && !strings.HasSuffix(branch, ".lock")
	if !isValid {
		return ErrInvalidBranch
	}

	return nil
}
