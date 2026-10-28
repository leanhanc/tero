package githubapp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/leanhanc/tero/internal/github"
	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/source"
)

// ErrNotInstalled means the App isn't connected or installed, so a covered
// repository can't be fetched.
var ErrNotInstalled = errors.New("Connect GitHub to deploy this app, or add its repository to the GitHub App's installation")

const (
	requestPollInterval = time.Second
	pruneInterval       = time.Hour
	// redeliveryWindow is how long GitHub keeps deliveries to redeliver.
	redeliveryWindow = 3 * 24 * time.Hour
)

// handledEvents are the webhook events Tero acts on, and so redelivers.
var handledEvents = map[string]bool{"push": true, "installation": true, "installation_repositories": true}

// KeyService is what the worker asks of the key service as the agent.
type KeyService interface {
	ConnectGitHub(ctx context.Context, secrets keys.GitHubAppSecrets) error
	SignAppToken(ctx context.Context) (keys.AppToken, error)
}

// GitHub is GitHub's API as the worker uses it.
type GitHub interface {
	ConvertManifest(ctx context.Context, code string) (github.Conversion, error)
	Installations(ctx context.Context, appToken string) ([]github.Installation, error)
	CreateInstallationToken(ctx context.Context, appToken string, installationID int64, repositoryIDs []int64, permission github.Permission) (github.InstallationToken, error)
	RevokeInstallationToken(ctx context.Context, token string) error
	InstallationRepositories(ctx context.Context, token string) ([]github.Repository, error)
	RecentDeliveries(ctx context.Context, appToken string, since time.Time) ([]github.Delivery, error)
	Redeliver(ctx context.Context, appToken string, deliveryID int64) error
}

// Worker is the agent's side of the GitHub App: it answers the dashboard's
// requests and mints the tokens deploys fetch with.
type Worker struct {
	DB     *sql.DB
	Keys   KeyService
	GitHub GitHub
	Now    func() time.Time
}

// Run catches up on what happened while the agent was down, then answers
// requests until ctx ends.
func (w Worker) Run(ctx context.Context) {
	if err := w.RefreshInstallation(ctx); err != nil {
		slog.Warn("read the GitHub App's installation", "error", err)
	}
	if err := w.RedeliverMissed(ctx); err != nil {
		slog.Warn("ask GitHub to redeliver missed webhooks", "error", err)
	}

	requests := time.NewTicker(requestPollInterval)
	prune := time.NewTicker(pruneInterval)
	defer requests.Stop()
	defer prune.Stop()

	for {
		if err := w.answerPending(ctx); err != nil && ctx.Err() == nil {
			slog.Error("answer a GitHub request", "error", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-prune.C:
			if err := pruneRequests(ctx, w.DB, w.Now()); err != nil {
				slog.Error("prune GitHub requests", "error", err)
			}
		case <-requests.C:
		}
	}
}

// answerPending answers every pending request, so a slow one doesn't keep
// the new-app form waiting past its timeout for the next tick.
func (w Worker) answerPending(ctx context.Context) error {
	for ctx.Err() == nil {
		hasAnswered, err := w.answerNext(ctx)
		if err != nil || !hasAnswered {
			return err
		}
	}

	return nil
}

func (w Worker) answerNext(ctx context.Context) (bool, error) {
	req, hasRequest, err := nextRequest(ctx, w.DB)
	if err != nil || !hasRequest {
		return false, err
	}

	result, err := w.handle(ctx, req)
	if err != nil {
		slog.Warn("GitHub request failed", "kind", req.Kind, "error", err)
		return true, answer(ctx, w.DB, req.ID, Failed, shownError(err), w.Now())
	}

	return true, answer(ctx, w.DB, req.ID, Done, result, w.Now())
}

// shownErrors are the failures the dashboard shows as they are. Anything
// else is logged here and shown as errFailed, so GitHub's answers and
// internal details stay out of the dashboard.
var shownErrors = []error{
	source.ErrRepositoryNotFound, source.ErrBranchNotFound, source.ErrInvalidBranch, source.ErrFolderNotFound,
	ErrNotInstalled, errConnectFailed, keys.ErrInvalidConnection,
}

// errConnectFailed is shown when GitHub didn't hand over the new App.
var errConnectFailed = errors.New("GitHub didn't hand over the new App. Start again from Settings")

var errFailed = errors.New("Tero couldn't finish this with GitHub. Try again, and see `journalctl --user -u tero-agent` on the server if it keeps failing")

func shownError(err error) string {
	for _, known := range shownErrors {
		if errors.Is(err, known) {
			return known.Error()
		}
	}

	return errFailed.Error()
}

func (w Worker) handle(ctx context.Context, req Request) (string, error) {
	switch req.Kind {
	case Connect:
		return w.connect(ctx, req.Code, req.State)
	case RefreshInstallation:
		return "", w.RefreshInstallation(ctx)
	case CheckSource:
		return w.checkSource(ctx, req.RepositoryID, req.Branch, req.RootDirectory)
	default:
		return "", fmt.Errorf("unknown request %q", req.Kind)
	}
}

// connect trades GitHub's code for the new App's secrets and hands them to
// the key service, which accepts them only for a state the admin approved.
// The dashboard never sees them.
func (w Worker) connect(ctx context.Context, code, state string) (string, error) {
	conversion, err := w.GitHub.ConvertManifest(ctx, code)
	if err != nil {
		return "", fmt.Errorf("%w: %v", errConnectFailed, err)
	}

	err = w.Keys.ConnectGitHub(ctx, keys.GitHubAppSecrets{
		State:         state,
		AppID:         conversion.ID,
		Slug:          conversion.Slug,
		Owner:         conversion.Owner.Login,
		IsOrgOwned:    conversion.Owner.IsOrganization(),
		PrivateKey:    conversion.PEM,
		WebhookSecret: conversion.WebhookSecret,
	})
	if err != nil {
		return "", err
	}

	return conversion.Slug, nil
}

// RefreshInstallation reads the App's installation and the repositories it
// covers from GitHub. With no App connected or no installation, it forgets
// the one stored.
func (w Worker) RefreshInstallation(ctx context.Context) error {
	appToken, err := w.Keys.SignAppToken(ctx)
	if errors.Is(err, keys.ErrGitHubNotConnected) {
		return ForgetInstallation(ctx, w.DB)
	}
	if err != nil {
		return err
	}

	installations, err := w.GitHub.Installations(ctx, appToken.Token)
	if err != nil {
		return err
	}
	if len(installations) == 0 {
		return ForgetInstallation(ctx, w.DB)
	}

	found := installations[0]
	installation := Installation{
		ID:             found.ID,
		Account:        found.Account.Login,
		IsOrganization: found.Account.IsOrganization(),
		IsSuspended:    found.SuspendedAt != nil,
	}
	if installation.IsSuspended {
		return saveSuspended(ctx, w.DB, installation, w.Now())
	}

	repositories, err := w.listRepositories(ctx, appToken.Token, installation.ID)
	if err != nil {
		return err
	}

	return saveInstallation(ctx, w.DB, installation, repositories, w.Now())
}

func (w Worker) listRepositories(ctx context.Context, appToken string, installationID int64) ([]Repository, error) {
	token, err := w.GitHub.CreateInstallationToken(ctx, appToken, installationID, nil, github.MetadataRead)
	if err != nil {
		return nil, err
	}
	defer w.revoke(ctx, token.Token)

	found, err := w.GitHub.InstallationRepositories(ctx, token.Token)
	if err != nil {
		return nil, err
	}

	repositories := make([]Repository, 0, len(found))
	for _, repository := range found {
		repositories = append(repositories, Repository{
			ID:            repository.ID,
			Owner:         repository.Owner.Login,
			Name:          repository.Name,
			IsPrivate:     repository.IsPrivate,
			DefaultBranch: repository.DefaultBranch,
		})
	}

	return repositories, nil
}

// RepositoryToken mints a token that can only read the contents of one
// covered repository. The caller must call revoke once the fetch ends,
// whatever happened; the token never leaves the agent's memory.
func (w Worker) RepositoryToken(ctx context.Context, repositoryID int64) (token source.Token, revoke func(), err error) {
	installation, isInstalled, err := LoadInstallation(ctx, w.DB)
	if err != nil {
		return "", nil, err
	}
	_, isCovered, err := FindRepository(ctx, w.DB, repositoryID)
	if err != nil {
		return "", nil, err
	}
	if !isInstalled || installation.IsSuspended || !isCovered {
		return "", nil, ErrNotInstalled
	}

	appToken, err := w.Keys.SignAppToken(ctx)
	if errors.Is(err, keys.ErrGitHubNotConnected) {
		return "", nil, ErrNotInstalled
	}
	if err != nil {
		return "", nil, err
	}

	minted, err := w.GitHub.CreateInstallationToken(ctx, appToken.Token, installation.ID, []int64{repositoryID}, github.ContentsRead)
	if errors.Is(err, github.ErrNotFound) {
		return "", nil, ErrNotInstalled
	}
	if err != nil {
		return "", nil, err
	}

	return source.Token(minted.Token), func() { w.revoke(ctx, minted.Token) }, nil
}

// revoke ends a token early. It runs even when ctx has ended, since the
// token would otherwise live for an hour; a failure is only logged.
func (w Worker) revoke(ctx context.Context, token string) {
	revokeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	if err := w.GitHub.RevokeInstallationToken(revokeCtx, token); err != nil {
		slog.Warn("revoke a GitHub installation token", "error", err)
	}
}

// SourceCheck is the answer to a source check: the cleaned folder.
type SourceCheck struct {
	RootDirectory string `json:"rootDirectory"`
}

// checkSource checks that a covered repository has the branch and folder.
// Its errors are the ones the new-app form shows.
func (w Worker) checkSource(ctx context.Context, repositoryID int64, branch, rootDirectory string) (string, error) {
	repository, isCovered, err := FindRepository(ctx, w.DB, repositoryID)
	if err != nil {
		return "", err
	}
	if !isCovered {
		return "", ErrNotInstalled
	}
	if err := source.CheckBranchName(branch); err != nil {
		return "", err
	}

	token, revoke, err := w.RepositoryToken(ctx, repositoryID)
	if err != nil {
		return "", err
	}
	defer revoke()

	// The dashboard can write these rows, so the name is checked again.
	target, err := source.ParseRepository("https://github.com/" + repository.Owner + "/" + repository.Name)
	if err != nil {
		return "", err
	}
	hasBranch, err := source.BranchExists(ctx, target, token, branch)
	if err != nil {
		return "", err
	}
	if !hasBranch {
		return "", source.ErrBranchNotFound
	}

	cleaned, err := source.CheckRootDirectory(ctx, target, token, branch, rootDirectory)
	if err != nil {
		return "", err
	}

	encoded, err := json.Marshal(SourceCheck{RootDirectory: cleaned})
	return string(encoded), err
}

// RedeliverMissed asks GitHub to send again every failed delivery of an
// event Tero acts on, from GitHub's redelivery window, that Tero hasn't
// handled and hasn't asked for before. Each arrives signed at the webhook
// endpoint like a live one.
func (w Worker) RedeliverMissed(ctx context.Context) error {
	appToken, err := w.Keys.SignAppToken(ctx)
	if errors.Is(err, keys.ErrGitHubNotConnected) {
		return nil
	}
	if err != nil {
		return err
	}

	deliveries, err := w.GitHub.RecentDeliveries(ctx, appToken.Token, w.Now().Add(-redeliveryWindow))
	if err != nil {
		return err
	}

	missed, err := missedDeliveries(ctx, w.DB, deliveries)
	if err != nil {
		return err
	}
	for _, delivery := range missed {
		if err := w.GitHub.Redeliver(ctx, appToken.Token, delivery.ID); err != nil {
			slog.Warn("ask GitHub to redeliver a webhook", "delivery", delivery.GUID, "error", err)
		}
	}
	if len(missed) > 0 {
		slog.Info("asked GitHub to redeliver missed webhooks", "count", len(missed))
	}

	return nil
}

// missedDeliveries picks, for each delivery GUID, the newest failed attempt
// of a handled event, unless any attempt succeeded, Tero recorded it as
// handled, or a redelivery was already asked for.
func missedDeliveries(ctx context.Context, db *sql.DB, deliveries []github.Delivery) ([]github.Delivery, error) {
	type attempts struct {
		newest        github.Delivery
		hasSucceeded  bool
		hasRedelivery bool
	}
	byGUID := map[string]*attempts{}
	var order []string
	for _, delivery := range deliveries {
		if !handledEvents[delivery.Event] {
			continue
		}

		seen, isKnown := byGUID[delivery.GUID]
		if !isKnown {
			seen = &attempts{newest: delivery}
			byGUID[delivery.GUID] = seen
			order = append(order, delivery.GUID)
		}
		seen.hasSucceeded = seen.hasSucceeded || !delivery.IsFailed()
		seen.hasRedelivery = seen.hasRedelivery || delivery.IsRedelivery
	}

	var missed []github.Delivery
	for _, guid := range order {
		seen := byGUID[guid]
		if seen.hasSucceeded || seen.hasRedelivery {
			continue
		}

		wasHandled, err := WasHandled(ctx, db, guid)
		if err != nil {
			return nil, err
		}
		if !wasHandled {
			missed = append(missed, seen.newest)
		}
	}

	return missed, nil
}

// WasHandled reports whether Tero handled a delivery.
func WasHandled(ctx context.Context, db *sql.DB, guid string) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, "SELECT count(*) FROM webhook_deliveries WHERE guid = ?", guid).Scan(&count)
	return count > 0, err
}

func saveSuspended(ctx context.Context, db *sql.DB, installation Installation, now time.Time) error {
	_, err := db.ExecContext(ctx, `
		INSERT OR REPLACE INTO github_installation (id, installation_id, account, is_organization, is_suspended, refreshed_at)
		VALUES (1, ?, ?, ?, 1, ?)`,
		installation.ID, installation.Account, installation.IsOrganization, now.Unix())
	return err
}
