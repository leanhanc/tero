package deployer

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/leanhanc/tero/internal/build"
	"github.com/leanhanc/tero/internal/containers"
	"github.com/leanhanc/tero/internal/github"
	"github.com/leanhanc/tero/internal/source"
	"github.com/leanhanc/tero/internal/store"
)

// e2e finding: a container start that hung left the deploy on "starting"
// with no reason. It now fails with one the admin can act on.
func TestStartTimeoutIsTheAdminsToAct(t *testing.T) {
	reason, isActionable := actionableReason(fmt.Errorf("start: %w", containers.ErrStartTimedOut))
	if !isActionable || reason != containers.ErrStartTimedOut.Error() {
		t.Fatalf("actionableReason = %q, %v, want the start timeout", reason, isActionable)
	}
}

type fakeRepositoryIDs struct {
	ids    map[string]int64
	tokens []string
}

func (f *fakeRepositoryIDs) RepositoryID(_ context.Context, owner, name, token string) (int64, error) {
	f.tokens = append(f.tokens, token)
	id, isFound := f.ids[owner+"/"+name]
	if !isFound {
		return 0, github.ErrNotFound
	}
	return id, nil
}

type fakeTokens struct {
	revoked int
}

func (f *fakeTokens) RepositoryToken(_ context.Context, repositoryID int64) (source.Token, func(), error) {
	return source.Token(fmt.Sprintf("token-for-%d", repositoryID)), func() { f.revoked++ }, nil
}

// Audit finding TERO-06: the agent reported a source without GitHub's id, so a
// repository that took over the name got the app's values.
func TestRepositoryIDComesFromGitHub(t *testing.T) {
	ctx := context.Background()
	lookups := &fakeRepositoryIDs{ids: map[string]int64{"acme/shop": 11, "evil/shop": 99}}
	tokens := &fakeTokens{}
	pipeline := Pipeline{GitHub: lookups, Tokens: tokens}

	public := app{Repository: source.Repository{Owner: "acme", Name: "shop"}}
	if id, err := pipeline.repositoryID(ctx, public); err != nil || id != 11 || lookups.tokens[0] != "" {
		t.Errorf("public repository = %d, %v, token %q", id, err, lookups.tokens[0])
	}

	covered := app{Repository: source.Repository{Owner: "acme", Name: "shop"}, GitHubRepositoryID: 11}
	if id, err := pipeline.repositoryID(ctx, covered); err != nil || id != 11 || lookups.tokens[1] != "token-for-11" {
		t.Errorf("covered repository = %d, %v, token %q", id, err, lookups.tokens[1])
	}

	// The shared database can pair a covered id with another repository's URL.
	paired := app{Repository: source.Repository{Owner: "evil", Name: "shop"}, GitHubRepositoryID: 11}
	if _, err := pipeline.repositoryID(ctx, paired); !errors.Is(err, errRepositoryMismatch) {
		t.Errorf("URL of another repository: err = %v", err)
	}
	if tokens.revoked != 2 {
		t.Errorf("%d of 2 tokens revoked", tokens.revoked)
	}

	missing := app{Repository: source.Repository{Owner: "gone", Name: "shop"}}
	var failure build.Failure
	if _, err := pipeline.repositoryID(ctx, missing); !errors.As(err, &failure) {
		t.Errorf("deleted repository: err = %v", err)
	}
}

// Audit finding (2026-11-10): only apps with variables were pinned to a
// repository id, so a repository that took over another app's name after a
// deletion or a rename was deployed.
func TestPinRepositoryRefusesAnotherRepository(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "tero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	result, err := db.ExecContext(ctx,
		"INSERT INTO apps (name, repository, branch, created_at) VALUES ('shop', 'https://github.com/acme/shop', 'main', 0)")
	if err != nil {
		t.Fatal(err)
	}
	appID, _ := result.LastInsertId()
	pipeline := Pipeline{DB: db}

	if err := pipeline.pinRepository(ctx, app{ID: appID}, 11); err != nil {
		t.Fatalf("first deploy: %v", err)
	}
	loaded, err := pipeline.loadApp(ctx, appID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PinnedRepositoryID != 11 {
		t.Fatalf("pinned id = %d, want 11", loaded.PinnedRepositoryID)
	}
	if err := pipeline.pinRepository(ctx, loaded, 11); err != nil {
		t.Errorf("same repository: %v", err)
	}
	if err := pipeline.pinRepository(ctx, loaded, 99); !errors.Is(err, errRepositoryReplaced) {
		t.Errorf("another repository: err = %v", err)
	}
}
