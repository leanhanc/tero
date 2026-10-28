package githubapp

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/leanhanc/tero/internal/github"
	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/store"
)

var testNow = time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)

type fakeKeys struct {
	isConnected bool
	connected   []keys.GitHubAppSecrets
}

func (k *fakeKeys) ConnectGitHub(_ context.Context, secrets keys.GitHubAppSecrets) error {
	k.connected = append(k.connected, secrets)
	k.isConnected = true
	return nil
}

func (k *fakeKeys) SignAppToken(context.Context) (keys.AppToken, error) {
	if !k.isConnected {
		return keys.AppToken{}, keys.ErrGitHubNotConnected
	}
	return keys.AppToken{Token: "app-jwt"}, nil
}

type mintedToken struct {
	repositoryIDs []int64
	permission    github.Permission
}

type fakeGitHub struct {
	installations []github.Installation
	repositories  []github.Repository
	deliveries    []github.Delivery
	minted        []mintedToken
	revoked       int
	redelivered   []int64
}

func (g *fakeGitHub) ConvertManifest(_ context.Context, code string) (github.Conversion, error) {
	return github.Conversion{ID: 4242, Slug: "tero-dash", Owner: github.Account{Login: "acme", Type: "Organization"}, PEM: "pem", WebhookSecret: "secret"}, nil
}

func (g *fakeGitHub) Installations(context.Context, string) ([]github.Installation, error) {
	return g.installations, nil
}

func (g *fakeGitHub) CreateInstallationToken(_ context.Context, _ string, _ int64, repositoryIDs []int64, permission github.Permission) (github.InstallationToken, error) {
	g.minted = append(g.minted, mintedToken{repositoryIDs, permission})
	return github.InstallationToken{Token: "ghs_token"}, nil
}

func (g *fakeGitHub) RevokeInstallationToken(context.Context, string) error {
	g.revoked++
	return nil
}

func (g *fakeGitHub) InstallationRepositories(context.Context, string) ([]github.Repository, error) {
	return g.repositories, nil
}

func (g *fakeGitHub) RecentDeliveries(context.Context, string, time.Time) ([]github.Delivery, error) {
	return g.deliveries, nil
}

func (g *fakeGitHub) Redeliver(_ context.Context, _ string, deliveryID int64) error {
	g.redelivered = append(g.redelivered, deliveryID)
	return nil
}

func newTestWorker(t *testing.T) (Worker, *fakeKeys, *fakeGitHub) {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "tero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	keyService := &fakeKeys{isConnected: true}
	gitHub := &fakeGitHub{
		installations: []github.Installation{{ID: 77, Account: github.Account{Login: "acme", Type: "Organization"}}},
		repositories: []github.Repository{
			{ID: 101, Name: "api", Owner: github.Account{Login: "acme"}, IsPrivate: true, DefaultBranch: "main"},
			{ID: 102, Name: "site", Owner: github.Account{Login: "acme"}, DefaultBranch: "trunk"},
		},
	}
	worker := Worker{DB: db, Keys: keyService, GitHub: gitHub, Now: func() time.Time { return testNow }}

	return worker, keyService, gitHub
}

// github-connection: Dashboard never sees the private key
func TestConnectHandsTheSecretsToTheKeyService(t *testing.T) {
	worker, keyService, _ := newTestWorker(t)
	keyService.isConnected = false
	ctx := context.Background()
	requestID, err := QueueConnect(ctx, worker.DB, "the-code", "the-state", testNow)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := worker.answerNext(ctx); err != nil {
		t.Fatal(err)
	}

	answer, _ := AnswerOf(ctx, worker.DB, requestID)
	if answer.Status != Done || answer.Result != "tero-dash" {
		t.Errorf("answer = %+v", answer)
	}
	if len(keyService.connected) != 1 {
		t.Fatalf("the key service got %d connections", len(keyService.connected))
	}
	got := keyService.connected[0]
	if got.State != "the-state" || got.AppID != 4242 || got.Owner != "acme" || !got.IsOrgOwned || got.PrivateKey != "pem" {
		t.Errorf("secrets handed over = %+v", got)
	}

	var code, state string
	worker.DB.QueryRow("SELECT code, state FROM github_requests WHERE id = ?", requestID).Scan(&code, &state)
	if code != "" || state != "" {
		t.Error("the spent code and state were kept in the database")
	}
}

func TestRefreshStoresTheInstallationAndLinksApps(t *testing.T) {
	worker, _, gitHub := newTestWorker(t)
	ctx := context.Background()
	worker.DB.Exec("INSERT INTO apps (name, repository, branch, created_at) VALUES ('site', 'https://github.com/Acme/Site', 'trunk', 1)")

	if err := worker.RefreshInstallation(ctx); err != nil {
		t.Fatal(err)
	}

	installation, isInstalled, _ := LoadInstallation(ctx, worker.DB)
	if !isInstalled || installation.ID != 77 || installation.Account != "acme" || !installation.IsOrganization {
		t.Errorf("installation = %+v, %v", installation, isInstalled)
	}
	repositories, _ := Repositories(ctx, worker.DB)
	if len(repositories) != 2 || repositories[0].DefaultBranch != "main" {
		t.Errorf("repositories = %+v", repositories)
	}
	var linkedID int64
	worker.DB.QueryRow("SELECT github_repository_id FROM apps WHERE name = 'site'").Scan(&linkedID)
	if linkedID != 102 {
		t.Errorf("the public app's repository id = %d, want 102", linkedID)
	}
	if len(gitHub.minted) != 1 || gitHub.minted[0].permission != github.MetadataRead || gitHub.revoked != 1 {
		t.Errorf("tokens minted %+v, revoked %d", gitHub.minted, gitHub.revoked)
	}
}

// github-connection: Installation removed on GitHub
func TestRefreshForgetsARemovedInstallation(t *testing.T) {
	worker, _, gitHub := newTestWorker(t)
	ctx := context.Background()
	worker.RefreshInstallation(ctx)

	gitHub.installations = nil
	if err := worker.RefreshInstallation(ctx); err != nil {
		t.Fatal(err)
	}

	if _, isInstalled, _ := LoadInstallation(ctx, worker.DB); isInstalled {
		t.Error("a removed installation was kept")
	}
	if _, _, err := worker.RepositoryToken(ctx, 101); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("token without an installation = %v", err)
	}
}

func TestSuspendedInstallationMintsNothing(t *testing.T) {
	worker, _, gitHub := newTestWorker(t)
	ctx := context.Background()
	worker.RefreshInstallation(ctx)

	suspendedAt := testNow
	gitHub.installations[0].SuspendedAt = &suspendedAt
	if err := worker.RefreshInstallation(ctx); err != nil {
		t.Fatal(err)
	}

	installation, _, _ := LoadInstallation(ctx, worker.DB)
	if !installation.IsSuspended {
		t.Error("the installation isn't marked suspended")
	}
	if _, _, err := worker.RepositoryToken(ctx, 101); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("token while suspended = %v", err)
	}
}

// github-connection: Deploy a private repository
func TestRepositoryTokenIsScopedAndRevoked(t *testing.T) {
	worker, _, gitHub := newTestWorker(t)
	ctx := context.Background()
	worker.RefreshInstallation(ctx)
	gitHub.minted, gitHub.revoked = nil, 0

	token, revoke, err := worker.RepositoryToken(ctx, 101)
	if err != nil {
		t.Fatal(err)
	}
	if token != "ghs_token" {
		t.Errorf("token = %q", token)
	}
	if len(gitHub.minted) != 1 || !slices.Equal(gitHub.minted[0].repositoryIDs, []int64{101}) || gitHub.minted[0].permission != github.ContentsRead {
		t.Errorf("minted %+v, want contents on repository 101 only", gitHub.minted)
	}

	revoke()
	if gitHub.revoked != 1 {
		t.Errorf("revoked %d tokens, want 1", gitHub.revoked)
	}
	if _, _, err := worker.RepositoryToken(ctx, 999); !errors.Is(err, ErrNotInstalled) {
		t.Errorf("token for a repository the App doesn't cover = %v", err)
	}
}

// github-connection: Push while the server was down, Delivery already
// handled
func TestRedeliverMissedAsksOnceForUnhandledFailures(t *testing.T) {
	worker, _, gitHub := newTestWorker(t)
	ctx := context.Background()
	markHandled(t, worker.DB, "handled")
	gitHub.deliveries = []github.Delivery{
		{ID: 10, GUID: "missed", Event: "push", StatusCode: 0},
		{ID: 9, GUID: "missed", Event: "push", StatusCode: 502},
		{ID: 8, GUID: "ok", Event: "push", StatusCode: 202},
		{ID: 7, GUID: "retried", Event: "push", StatusCode: 502, IsRedelivery: true},
		{ID: 6, GUID: "retried", Event: "push", StatusCode: 502},
		{ID: 5, GUID: "handled", Event: "push", StatusCode: 504},
		{ID: 4, GUID: "ignored", Event: "issues", StatusCode: 500},
		{ID: 3, GUID: "removed", Event: "installation", StatusCode: 500},
	}

	if err := worker.RedeliverMissed(ctx); err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(gitHub.redelivered, []int64{10, 3}) {
		t.Errorf("redelivered %v, want [10 3]", gitHub.redelivered)
	}
}

func TestRedeliverMissedWithoutAnApp(t *testing.T) {
	worker, keyService, gitHub := newTestWorker(t)
	keyService.isConnected = false
	gitHub.deliveries = []github.Delivery{{ID: 1, GUID: "a", Event: "push"}}

	if err := worker.RedeliverMissed(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(gitHub.redelivered) != 0 {
		t.Error("asked for redeliveries with no App connected")
	}
}

func markHandled(t *testing.T, db *sql.DB, guid string) {
	t.Helper()
	if _, err := db.Exec("INSERT INTO webhook_deliveries (guid, handled_at) VALUES (?, 1)", guid); err != nil {
		t.Fatal(err)
	}
}

// github-connection: Disconnect (public apps go back to polling)
func TestForgettingTheInstallationUnlinksApps(t *testing.T) {
	worker, _, gitHub := newTestWorker(t)
	ctx := context.Background()
	worker.DB.Exec("INSERT INTO apps (name, repository, branch, created_at) VALUES ('site', 'https://github.com/acme/site', 'trunk', 1)")
	worker.RefreshInstallation(ctx)

	gitHub.repositories = gitHub.repositories[:1]
	worker.RefreshInstallation(ctx)
	var linkedID int64
	worker.DB.QueryRow("SELECT github_repository_id FROM apps WHERE name = 'site'").Scan(&linkedID)
	if linkedID != 0 {
		t.Errorf("an app stayed linked to a repository the App no longer covers")
	}

	gitHub.repositories = []github.Repository{{ID: 102, Name: "site", Owner: github.Account{Login: "acme"}, DefaultBranch: "trunk"}}
	worker.RefreshInstallation(ctx)
	ForgetInstallation(ctx, worker.DB)
	worker.DB.QueryRow("SELECT github_repository_id FROM apps WHERE name = 'site'").Scan(&linkedID)
	if linkedID != 0 {
		t.Errorf("an app stayed linked after the installation was forgotten")
	}
}
