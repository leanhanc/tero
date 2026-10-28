package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
)

// Errors shown in the new-app form.
var (
	ErrRepositoryNotFound = errors.New("That repository wasn't found. Check the URL. For a private repository, connect GitHub in Settings and give the App access to it")
	ErrBranchNotFound     = errors.New("That branch wasn't found in the repository")
	ErrFolderNotFound     = errors.New("That folder wasn't found in the repository on this branch")
)

// Commit is the commit a deploy builds.
type Commit struct {
	SHA     string
	Message string
}

// Token is a GitHub installation token for a private repository, or empty
// for a public one. It is only ever passed to git as HTTP basic auth, never
// put in a URL or in the checkout's .git/config.
type Token string

// authFor is the token as git's HTTP basic auth for the repository. Only
// GitHub over HTTPS gets it: e2e builds read fixtures from local copies,
// where git takes no credentials.
func (t Token) authFor(repo Repository) transport.AuthMethod {
	isHTTPS := strings.HasPrefix(repo.cloneURL(), "https://")
	if t == "" || !isHTTPS {
		return nil
	}

	return &githttp.BasicAuth{Username: "x-access-token", Password: string(t)}
}

// DefaultBranch returns the branch the repository's HEAD points to.
func DefaultBranch(ctx context.Context, repo Repository, token Token) (string, error) {
	refs, err := listRefs(ctx, repo, token)
	if err != nil {
		return "", err
	}

	for _, ref := range refs {
		isSymbolicHead := ref.Name() == plumbing.HEAD && ref.Type() == plumbing.SymbolicReference
		if isSymbolicHead && ref.Target().IsBranch() {
			return ref.Target().Short(), nil
		}
	}

	return "", fmt.Errorf("%s has no default branch", repo.URL())
}

// BranchExists reports whether the branch exists on GitHub.
func BranchExists(ctx context.Context, repo Repository, token Token, branch string) (bool, error) {
	refs, err := listRefs(ctx, repo, token)
	if err != nil {
		return false, err
	}

	branchRef := plumbing.NewBranchReferenceName(branch)
	for _, ref := range refs {
		if ref.Name() == branchRef {
			return true, nil
		}
	}

	return false, nil
}

// Clone fetches the branch's latest commit into dir, which must not exist
// yet, and returns that commit. The .git directory is removed afterwards, so
// it never reaches the build.
func Clone(ctx context.Context, repo Repository, token Token, branch, dir string) (Commit, error) {
	cloned, err := git.PlainCloneContext(ctx, dir, false, &git.CloneOptions{
		URL:           repo.cloneURL(),
		Auth:          token.authFor(repo),
		ReferenceName: plumbing.NewBranchReferenceName(branch),
		SingleBranch:  true,
		Depth:         1,
		Tags:          git.NoTags,
	})
	if err != nil {
		return Commit{}, describeFetchError(err)
	}

	commit, err := headCommit(cloned)
	if err != nil {
		return Commit{}, err
	}

	return commit, os.RemoveAll(filepath.Join(dir, git.GitDirName))
}

// BranchHead returns the commit the branch points to on GitHub, without
// fetching anything else.
func BranchHead(ctx context.Context, repo Repository, token Token, branch string) (string, error) {
	refs, err := listRefs(ctx, repo, token)
	if err != nil {
		return "", err
	}

	branchRef := plumbing.NewBranchReferenceName(branch)
	for _, ref := range refs {
		if ref.Name() == branchRef {
			return ref.Hash().String(), nil
		}
	}

	return "", ErrBranchNotFound
}

// CheckRootDirectory returns the cleaned root directory, or
// ErrFolderNotFound when it is absolute, leaves the repository, or isn't a
// folder on the branch. An empty root directory means the repository root.
// It reads only the branch's latest tree, without checking files out.
func CheckRootDirectory(ctx context.Context, repo Repository, token Token, branch, rootDirectory string) (string, error) {
	cleaned, err := cleanRootDirectory(rootDirectory)
	if err != nil || cleaned == "" {
		return cleaned, err
	}

	cloned, err := git.CloneContext(ctx, memory.NewStorage(), nil, &git.CloneOptions{
		URL:           repo.cloneURL(),
		Auth:          token.authFor(repo),
		ReferenceName: plumbing.NewBranchReferenceName(branch),
		SingleBranch:  true,
		Depth:         1,
		NoCheckout:    true,
		Tags:          git.NoTags,
	})
	if err != nil {
		return "", describeFetchError(err)
	}

	tree, err := headTree(cloned)
	if err != nil {
		return "", err
	}
	if _, err := tree.Tree(cleaned); err != nil {
		return "", ErrFolderNotFound
	}

	return cleaned, nil
}

// BuildDirectory resolves the root directory inside a checkout, following
// symlinks, and refuses anything that lands outside it.
func BuildDirectory(checkoutDir, rootDirectory string) (string, error) {
	cleaned, err := cleanRootDirectory(rootDirectory)
	if err != nil {
		return "", err
	}

	checkout, err := filepath.EvalSymlinks(checkoutDir)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(checkout, filepath.FromSlash(cleaned)))
	if err != nil {
		return "", ErrFolderNotFound
	}

	relative, err := filepath.Rel(checkout, resolved)
	isInsideCheckout := err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
	if !isInsideCheckout {
		return "", ErrFolderNotFound
	}

	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", ErrFolderNotFound
	}

	return resolved, nil
}

// cleanRootDirectory normalizes a repository-relative folder path. "", "."
// and "/" style inputs that mean the root come back as "".
func cleanRootDirectory(rootDirectory string) (string, error) {
	trimmed := strings.TrimSpace(rootDirectory)
	if trimmed == "" {
		return "", nil
	}
	if strings.HasPrefix(trimmed, "/") || strings.Contains(trimmed, `\`) {
		return "", ErrFolderNotFound
	}

	cleaned := path.Clean(trimmed)
	if cleaned == "." {
		return "", nil
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", ErrFolderNotFound
	}

	return cleaned, nil
}

func listRefs(ctx context.Context, repo Repository, token Token) ([]*plumbing.Reference, error) {
	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{Name: "origin", URLs: []string{repo.cloneURL()}})

	refs, err := remote.ListContext(ctx, &git.ListOptions{Auth: token.authFor(repo)})
	if err != nil {
		return nil, describeFetchError(err)
	}

	return refs, nil
}

func headCommit(repo *git.Repository) (Commit, error) {
	head, err := repo.Head()
	if err != nil {
		return Commit{}, err
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return Commit{}, err
	}

	firstLine, _, _ := strings.Cut(strings.TrimSpace(commit.Message), "\n")
	return Commit{SHA: head.Hash().String(), Message: firstLine}, nil
}

func headTree(repo *git.Repository) (*object.Tree, error) {
	head, err := repo.Head()
	if err != nil {
		return nil, err
	}
	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return nil, err
	}

	return commit.Tree()
}

// describeFetchError turns git's errors into ones the admin can act on.
// GitHub answers "authentication required" for private and missing
// repositories alike, so both read as not found.
func describeFetchError(err error) error {
	isMissingRepository := errors.Is(err, transport.ErrRepositoryNotFound) ||
		errors.Is(err, transport.ErrAuthenticationRequired)
	if isMissingRepository {
		return ErrRepositoryNotFound
	}

	var noMatchingRef git.NoMatchingRefSpecError
	if errors.As(err, &noMatchingRef) || errors.Is(err, plumbing.ErrReferenceNotFound) {
		return ErrBranchNotFound
	}

	return fmt.Errorf("couldn't reach GitHub: %w", err)
}
