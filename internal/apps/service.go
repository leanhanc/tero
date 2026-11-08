package apps

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/leanhanc/tero/internal/deploys"
	"github.com/leanhanc/tero/internal/githubapp"
	"github.com/leanhanc/tero/internal/source"
)

// ErrNameTaken means another app already uses the name, and so the address.
var ErrNameTaken = errors.New("Another app already uses this address")

// ErrNotFound means no app or deploy matches.
var ErrNotFound = errors.New("Not found")

// ErrRepositoryNotCovered means the repository picked from the list is no
// longer one the GitHub App covers.
var ErrRepositoryNotCovered = errors.New("The GitHub App no longer covers that repository. Refresh the list and pick it again")

// ErrAgentNotAnswering means the agent didn't check a private repository in
// time.
var ErrAgentNotAnswering = errors.New("Tero couldn't check the repository in time. Try again in a moment")

// recentDeploys is how many deploys an app's page lists.
const recentDeploys = 20

// MaxPollFailures failed polls in a row stop polling an app until it is
// redeployed.
const MaxPollFailures = 3

// Service is the dashboard's view of apps and their deploys. It reads and
// writes the store; the agent does the deploying.
type Service struct {
	db       *sql.DB
	publicIP string
	now      func() time.Time
}

// NewService returns a Service. publicIP builds each app's address.
func NewService(db *sql.DB, publicIP string, now func() time.Time) *Service {
	return &Service{db: db, publicIP: publicIP, now: now}
}

// Lookup is what the new-app form fills in from a repository URL. The form
// suggests the repository name as the display name, and an address from that.
type Lookup struct {
	Repository     string `json:"repository"`
	DefaultBranch  string `json:"defaultBranch"`
	RepositoryName string `json:"repositoryName"`
	AppDomain      string `json:"appDomain"`
}

// LookUpRepository checks a repository URL and reads its default branch.
func (s *Service) LookUpRepository(ctx context.Context, rawURL string) (Lookup, error) {
	repository, err := source.ParseRepository(rawURL)
	if err != nil {
		return Lookup{}, err
	}

	defaultBranch, err := source.DefaultBranch(ctx, repository, "")
	if err != nil {
		return Lookup{}, err
	}

	lookup := Lookup{
		Repository:     repository.URL(),
		DefaultBranch:  defaultBranch,
		RepositoryName: repository.Name,
		AppDomain:      Domain(s.publicIP),
	}
	return lookup, nil
}

// NewApp is what the admin enters to create an app. Name is the first label
// of the app's address; DisplayName is what the dashboard shows.
type NewApp struct {
	Repository string `json:"repository"`
	// GitHubRepositoryID is set when the admin picked a repository the
	// GitHub App covers, and then Repository is ignored.
	GitHubRepositoryID int64  `json:"githubRepositoryId"`
	Branch             string `json:"branch"`
	DisplayName        string `json:"displayName"`
	Name               string `json:"name"`
	RootDirectory      string `json:"rootDirectory"`
}

// Create checks the new app against GitHub, stores it, and queues its first
// deploy.
func (s *Service) Create(ctx context.Context, newApp NewApp) (string, error) {
	displayName, err := CleanDisplayName(newApp.DisplayName)
	if err != nil {
		return "", err
	}
	if err := CheckName(newApp.Name); err != nil {
		return "", err
	}
	if err := source.CheckBranchName(newApp.Branch); err != nil {
		return "", err
	}

	checked, err := s.checkSource(ctx, newApp)
	if err != nil {
		return "", err
	}

	stored := storedApp{
		name:               newApp.Name,
		displayName:        displayName,
		repositoryURL:      checked.repository.URL(),
		branch:             newApp.Branch,
		rootDirectory:      checked.rootDirectory,
		gitHubRepositoryID: checked.gitHubRepositoryID,
	}
	appID, err := s.insertApp(ctx, stored)
	if err != nil {
		return "", err
	}

	if _, err := deploys.Enqueue(ctx, s.db, appID, deploys.Trigger{Type: deploys.TriggerCreate}, s.now()); err != nil {
		return "", err
	}

	return newApp.Name, nil
}

// checkedSource is a repository, branch and folder that were found.
type checkedSource struct {
	repository         source.Repository
	rootDirectory      string
	gitHubRepositoryID int64
}

// checkSource checks the branch and folder: through the agent for a
// repository the GitHub App covers, private ones included, and directly on
// GitHub for any other public repository.
func (s *Service) checkSource(ctx context.Context, newApp NewApp) (checkedSource, error) {
	covered, isCovered, err := s.coveredRepository(ctx, newApp)
	if err != nil {
		return checkedSource{}, err
	}
	if isCovered {
		return s.checkThroughAgent(ctx, covered, newApp)
	}

	repository, err := source.ParseRepository(newApp.Repository)
	if err != nil {
		return checkedSource{}, err
	}
	hasBranch, err := source.BranchExists(ctx, repository, "", newApp.Branch)
	if err != nil {
		return checkedSource{}, err
	}
	if !hasBranch {
		return checkedSource{}, source.ErrBranchNotFound
	}
	rootDirectory, err := source.CheckRootDirectory(ctx, repository, "", newApp.Branch, newApp.RootDirectory)
	if err != nil {
		return checkedSource{}, err
	}

	return checkedSource{repository: repository, rootDirectory: rootDirectory}, nil
}

// coveredRepository finds the repository among those the GitHub App covers:
// by GitHub's id when the admin picked it from the list, or by the URL's
// owner and name.
func (s *Service) coveredRepository(ctx context.Context, newApp NewApp) (githubapp.Repository, bool, error) {
	if newApp.GitHubRepositoryID != 0 {
		covered, isCovered, err := githubapp.FindRepository(ctx, s.db, newApp.GitHubRepositoryID)
		if err == nil && !isCovered {
			return githubapp.Repository{}, false, ErrRepositoryNotCovered
		}
		return covered, isCovered, err
	}

	repository, err := source.ParseRepository(newApp.Repository)
	if err != nil {
		return githubapp.Repository{}, false, err
	}

	return githubapp.FindRepositoryByName(ctx, s.db, repository.Owner, repository.Name)
}

// sourceCheckTimeout bounds the wait for the agent, which checks each
// request within a second or two.
const sourceCheckTimeout = 15 * time.Second

func (s *Service) checkThroughAgent(ctx context.Context, covered githubapp.Repository, newApp NewApp) (checkedSource, error) {
	requestID, err := githubapp.QueueSourceCheck(ctx, s.db, covered.ID, newApp.Branch, newApp.RootDirectory, s.now())
	if err != nil {
		return checkedSource{}, err
	}

	waitCtx, cancel := context.WithTimeout(ctx, sourceCheckTimeout)
	defer cancel()
	answer, err := githubapp.WaitForAnswer(waitCtx, s.db, requestID)
	if errors.Is(err, context.DeadlineExceeded) {
		return checkedSource{}, ErrAgentNotAnswering
	}
	if err != nil {
		return checkedSource{}, err
	}
	if answer.Status == githubapp.Failed {
		return checkedSource{}, sourceErrorFrom(answer.Result)
	}

	var check githubapp.SourceCheck
	if err := json.Unmarshal([]byte(answer.Result), &check); err != nil {
		return checkedSource{}, err
	}

	repository := source.Repository{Owner: covered.Owner, Name: covered.Name}
	return checkedSource{repository: repository, rootDirectory: check.RootDirectory, gitHubRepositoryID: covered.ID}, nil
}

// sourceErrorFrom turns the agent's failure back into the error the form
// shows for it.
func sourceErrorFrom(reason string) error {
	known := []error{
		source.ErrRepositoryNotFound, source.ErrBranchNotFound, source.ErrInvalidBranch, source.ErrFolderNotFound,
		githubapp.ErrNotInstalled,
	}
	for _, candidate := range known {
		if reason == candidate.Error() {
			return candidate
		}
	}

	return fmt.Errorf("checking the repository failed: %s", reason)
}

// Redeploy queues a deploy of the app's branch, behind one that is running,
// or takes over the one already queued.
func (s *Service) Redeploy(ctx context.Context, name string) error {
	appID, err := s.appID(ctx, name)
	if err != nil {
		return err
	}

	// Polling that stopped after repeated failures starts again.
	if _, err := s.db.ExecContext(ctx, "UPDATE apps SET poll_failures = 0, poll_error = '' WHERE id = ?", appID); err != nil {
		return err
	}

	_, err = deploys.Enqueue(ctx, s.db, appID, deploys.Trigger{Type: deploys.TriggerManual}, s.now())
	return err
}

// PendingChange is a change saved for an app's next deploy. Touching a build
// means redeploying it builds.
type PendingChange struct {
	Name         string
	TouchesBuild bool
}

// RecordPendingChanges notes what a save changed and whether the app has any
// variables after it. Saving deploys nothing: the app page offers to redeploy.
// Neither value is trusted by the agent: the first only decides what the page
// shows and how much work a deploy does, the second whether the agent asks the
// key service for values.
func (s *Service) RecordPendingChanges(ctx context.Context, name string, changes []PendingChange, hasVariables bool) error {
	appID, err := s.appID(ctx, name)
	if err != nil {
		return err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "UPDATE apps SET has_variables = ? WHERE id = ?", hasVariables, appID); err != nil {
		return err
	}
	for _, change := range changes {
		if err := recordPendingChange(ctx, tx, appID, change); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// recordPendingChange gives a change a newer id each time, so a deploy already
// running doesn't clear it, and keeps a need for a build once there is one.
func recordPendingChange(ctx context.Context, tx *sql.Tx, appID int64, change PendingChange) error {
	var touchedBuild bool
	err := tx.QueryRowContext(ctx, "SELECT touches_build FROM app_pending_changes WHERE app_id = ? AND name = ?", appID, change.Name).Scan(&touchedBuild)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	_, err = tx.ExecContext(ctx, "DELETE FROM app_pending_changes WHERE app_id = ? AND name = ?", appID, change.Name)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO app_pending_changes (app_id, name, touches_build) VALUES (?, ?, ?)",
		appID, change.Name, touchedBuild || change.TouchesBuild)

	return err
}

// RedeployChanges applies what was saved, with the least work that does it: a
// restart of the live image, unless a change reaches a build. With nothing
// pending it does nothing.
func (s *Service) RedeployChanges(ctx context.Context, name string) error {
	appID, err := s.appID(ctx, name)
	if err != nil {
		return err
	}

	pending, needsBuild, err := s.pendingChanges(ctx, appID)
	if err != nil || len(pending) == 0 {
		return err
	}

	trigger := deploys.Trigger{Type: deploys.TriggerVariables, SkipsBuild: !needsBuild}
	_, err = deploys.Enqueue(ctx, s.db, appID, trigger, s.now())
	return err
}

func (s *Service) pendingChanges(ctx context.Context, appID int64) ([]string, bool, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT name, touches_build FROM app_pending_changes WHERE app_id = ? ORDER BY name", appID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	names := []string{}
	needsBuild := false
	for rows.Next() {
		var name string
		var touchesBuild bool
		if err := rows.Scan(&name, &touchesBuild); err != nil {
			return nil, false, err
		}
		names = append(names, name)
		needsBuild = needsBuild || touchesBuild
	}

	return names, needsBuild, rows.Err()
}

// SetDisplayName changes the name the dashboard shows for the app.
func (s *Service) SetDisplayName(ctx context.Context, name, displayName string) error {
	cleaned, err := CleanDisplayName(displayName)
	if err != nil {
		return err
	}

	result, err := s.db.ExecContext(ctx, "UPDATE apps SET display_name = ? WHERE name = ?", cleaned, name)
	if err != nil {
		return err
	}

	return requireChanged(result)
}

// SetRunTogether changes whether a deploy starts next to the live release.
func (s *Service) SetRunTogether(ctx context.Context, name string, runTogether bool) error {
	result, err := s.db.ExecContext(ctx, "UPDATE apps SET run_together = ? WHERE name = ?", runTogether, name)
	if err != nil {
		return err
	}

	return requireChanged(result)
}

// SetDeployOnPush changes whether pushes and polls queue deploys of the app.
func (s *Service) SetDeployOnPush(ctx context.Context, name string, deployOnPush bool) error {
	result, err := s.db.ExecContext(ctx, "UPDATE apps SET deploy_on_push = ? WHERE name = ?", deployOnPush, name)
	if err != nil {
		return err
	}

	return requireChanged(result)
}

// requireChanged returns ErrNotFound when an update matched no app.
func requireChanged(result sql.Result) error {
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return ErrNotFound
	}

	return nil
}

// Summary is one row of the app list.
type Summary struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Address     string `json:"address"`
	LatestState string `json:"latestState"`
	// PendingChanges counts what was saved since the app's last live deploy.
	PendingChanges int `json:"pendingChanges"`
}

// List returns every app with the state of its latest deploy.
func (s *Service) List(ctx context.Context) ([]Summary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT apps.name, apps.display_name,
			COALESCE((SELECT state FROM deploys WHERE app_id = apps.id ORDER BY id DESC LIMIT 1), ''),
			(SELECT count(*) FROM app_pending_changes WHERE app_id = apps.id)
		FROM apps ORDER BY apps.display_name COLLATE NOCASE, apps.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summaries := []Summary{}
	for rows.Next() {
		var summary Summary
		if err := rows.Scan(&summary.Name, &summary.DisplayName, &summary.LatestState, &summary.PendingChanges); err != nil {
			return nil, err
		}
		summary.Address = s.address(summary.Name)
		summaries = append(summaries, summary)
	}

	return summaries, rows.Err()
}

// Deploy is one deploy as the app page shows it.
type Deploy struct {
	ID            int64  `json:"id"`
	State         string `json:"state"`
	FailedStep    string `json:"failedStep"`
	Reason        string `json:"reason"`
	CommitSHA     string `json:"commitSha"`
	CommitMessage string `json:"commitMessage"`
	Trigger       string `json:"trigger"`
	PushedBy      string `json:"pushedBy"`
	QueuedAt      int64  `json:"queuedAt"`
	FinishedAt    int64  `json:"finishedAt"`
}

// Detail is everything the app page shows.
type Detail struct {
	Name          string `json:"name"`
	DisplayName   string `json:"displayName"`
	Address       string `json:"address"`
	Repository    string `json:"repository"`
	Branch        string `json:"branch"`
	RootDirectory string `json:"rootDirectory"`
	RunTogether   bool   `json:"runTogether"`
	DeployOnPush  bool   `json:"deployOnPush"`
	// PollError says why polling stopped, after repeated failures.
	PollError string `json:"pollError"`
	// StartProblem says why the live release can't start, while the agent
	// keeps trying.
	StartProblem string `json:"startProblem"`
	// PendingChanges names what was saved since the last live deploy.
	PendingChanges []string `json:"pendingChanges"`
	// PendingNeedsBuild says redeploying those changes builds.
	PendingNeedsBuild bool     `json:"pendingNeedsBuild"`
	Deploys           []Deploy `json:"deploys"`
}

// Get returns an app with its recent deploys, newest first.
func (s *Service) Get(ctx context.Context, name string) (Detail, error) {
	var appID int64
	detail := Detail{Name: name, Address: s.address(name)}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, display_name, repository, branch, root_directory, run_together, deploy_on_push,
			CASE WHEN poll_failures >= ? THEN poll_error ELSE '' END, start_problem
		FROM apps WHERE name = ?`, MaxPollFailures, name).
		Scan(&appID, &detail.DisplayName, &detail.Repository, &detail.Branch, &detail.RootDirectory, &detail.RunTogether,
			&detail.DeployOnPush, &detail.PollError, &detail.StartProblem)
	if errors.Is(err, sql.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, err
	}

	detail.PendingChanges, detail.PendingNeedsBuild, err = s.pendingChanges(ctx, appID)
	if err != nil {
		return Detail{}, err
	}

	detail.Deploys, err = s.recentDeploys(ctx, appID)
	return detail, err
}

// DeployState returns a deploy's state, and ErrNotFound for an unknown id.
func (s *Service) DeployState(ctx context.Context, deployID int64) (deploys.State, error) {
	var state string
	err := s.db.QueryRowContext(ctx, "SELECT state FROM deploys WHERE id = ?", deployID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}

	return deploys.State(state), err
}

func (s *Service) recentDeploys(ctx context.Context, appID int64) ([]Deploy, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, state, failed_step, reason, commit_sha, commit_message, trigger_type, pushed_by, queued_at, finished_at
		FROM deploys WHERE app_id = ? ORDER BY id DESC LIMIT ?`, appID, recentDeploys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	recent := []Deploy{}
	for rows.Next() {
		var deploy Deploy
		err := rows.Scan(&deploy.ID, &deploy.State, &deploy.FailedStep, &deploy.Reason,
			&deploy.CommitSHA, &deploy.CommitMessage, &deploy.Trigger, &deploy.PushedBy, &deploy.QueuedAt, &deploy.FinishedAt)
		if err != nil {
			return nil, err
		}
		recent = append(recent, deploy)
	}

	return recent, rows.Err()
}

// storedApp is a checked new app, ready to insert.
type storedApp struct {
	name               string
	displayName        string
	repositoryURL      string
	branch             string
	rootDirectory      string
	gitHubRepositoryID int64
}

func (s *Service) insertApp(ctx context.Context, app storedApp) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		"INSERT INTO apps (name, display_name, repository, branch, root_directory, github_repository_id, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		app.name, app.displayName, app.repositoryURL, app.branch, app.rootDirectory, app.gitHubRepositoryID, s.now().Unix())
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed: apps.name") {
		return 0, ErrNameTaken
	}
	if err != nil {
		return 0, fmt.Errorf("save the app: %w", err)
	}

	return result.LastInsertId()
}

func (s *Service) appID(ctx context.Context, name string) (int64, error) {
	var appID int64
	err := s.db.QueryRowContext(ctx, "SELECT id FROM apps WHERE name = ?", name).Scan(&appID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}

	return appID, err
}

func (s *Service) address(name string) string {
	return "https://" + Host(name, s.publicIP)
}
