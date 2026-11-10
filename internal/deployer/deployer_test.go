package deployer

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/leanhanc/tero/internal/build"
	"github.com/leanhanc/tero/internal/containers"
	"github.com/leanhanc/tero/internal/github"
	"github.com/leanhanc/tero/internal/source"
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
