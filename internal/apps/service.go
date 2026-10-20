package apps

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/leanhanc/tero/internal/deploys"
	"github.com/leanhanc/tero/internal/source"
)

// ErrNameTaken means another app already uses the name.
var ErrNameTaken = errors.New("Another app already uses this name")

// ErrNotFound means no app or deploy matches.
var ErrNotFound = errors.New("Not found")

// recentDeploys is how many deploys an app's page lists.
const recentDeploys = 20

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

// Lookup is what the new-app form fills in from a repository URL.
type Lookup struct {
	Repository    string `json:"repository"`
	DefaultBranch string `json:"defaultBranch"`
	SuggestedName string `json:"suggestedName"`
}

// LookUpRepository checks a repository URL and reads its default branch.
func (s *Service) LookUpRepository(ctx context.Context, rawURL string) (Lookup, error) {
	repository, err := source.ParseRepository(rawURL)
	if err != nil {
		return Lookup{}, err
	}

	defaultBranch, err := source.DefaultBranch(ctx, repository)
	if err != nil {
		return Lookup{}, err
	}

	return Lookup{Repository: repository.URL(), DefaultBranch: defaultBranch, SuggestedName: suggestName(repository.Name)}, nil
}

// NewApp is what the admin enters to create an app.
type NewApp struct {
	Repository    string `json:"repository"`
	Branch        string `json:"branch"`
	Name          string `json:"name"`
	RootDirectory string `json:"rootDirectory"`
}

// Create checks the new app against GitHub, stores it, and queues its first
// deploy.
func (s *Service) Create(ctx context.Context, newApp NewApp) (string, error) {
	repository, err := source.ParseRepository(newApp.Repository)
	if err != nil {
		return "", err
	}
	if err := CheckName(newApp.Name); err != nil {
		return "", err
	}
	if err := source.CheckBranchName(newApp.Branch); err != nil {
		return "", err
	}

	hasBranch, err := source.BranchExists(ctx, repository, newApp.Branch)
	if err != nil {
		return "", err
	}
	if !hasBranch {
		return "", source.ErrBranchNotFound
	}

	rootDirectory, err := source.CheckRootDirectory(ctx, repository, newApp.Branch, newApp.RootDirectory)
	if err != nil {
		return "", err
	}

	appID, err := s.insertApp(ctx, newApp.Name, repository.URL(), newApp.Branch, rootDirectory)
	if err != nil {
		return "", err
	}

	if _, err := deploys.Enqueue(ctx, s.db, appID, s.now()); err != nil {
		return "", err
	}

	return newApp.Name, nil
}

// Redeploy queues a deploy of the app's branch.
func (s *Service) Redeploy(ctx context.Context, name string) error {
	appID, err := s.appID(ctx, name)
	if err != nil {
		return err
	}

	_, err = deploys.Enqueue(ctx, s.db, appID, s.now())
	return err
}

// SetRunTogether changes whether a deploy starts next to the live release.
func (s *Service) SetRunTogether(ctx context.Context, name string, runTogether bool) error {
	result, err := s.db.ExecContext(ctx, "UPDATE apps SET run_together = ? WHERE name = ?", runTogether, name)
	if err != nil {
		return err
	}

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
	Address     string `json:"address"`
	LatestState string `json:"latestState"`
}

// List returns every app with the state of its latest deploy.
func (s *Service) List(ctx context.Context) ([]Summary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT apps.name, COALESCE((SELECT state FROM deploys WHERE app_id = apps.id ORDER BY id DESC LIMIT 1), '')
		FROM apps ORDER BY apps.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summaries := []Summary{}
	for rows.Next() {
		var summary Summary
		if err := rows.Scan(&summary.Name, &summary.LatestState); err != nil {
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
	QueuedAt      int64  `json:"queuedAt"`
	FinishedAt    int64  `json:"finishedAt"`
}

// Detail is everything the app page shows.
type Detail struct {
	Name          string   `json:"name"`
	Address       string   `json:"address"`
	Repository    string   `json:"repository"`
	Branch        string   `json:"branch"`
	RootDirectory string   `json:"rootDirectory"`
	RunTogether   bool     `json:"runTogether"`
	Deploys       []Deploy `json:"deploys"`
}

// Get returns an app with its recent deploys, newest first.
func (s *Service) Get(ctx context.Context, name string) (Detail, error) {
	var appID int64
	detail := Detail{Name: name, Address: s.address(name)}
	err := s.db.QueryRowContext(ctx,
		"SELECT id, repository, branch, root_directory, run_together FROM apps WHERE name = ?", name).
		Scan(&appID, &detail.Repository, &detail.Branch, &detail.RootDirectory, &detail.RunTogether)
	if errors.Is(err, sql.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
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
		SELECT id, state, failed_step, reason, commit_sha, commit_message, queued_at, finished_at
		FROM deploys WHERE app_id = ? ORDER BY id DESC LIMIT ?`, appID, recentDeploys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	recent := []Deploy{}
	for rows.Next() {
		var deploy Deploy
		err := rows.Scan(&deploy.ID, &deploy.State, &deploy.FailedStep, &deploy.Reason,
			&deploy.CommitSHA, &deploy.CommitMessage, &deploy.QueuedAt, &deploy.FinishedAt)
		if err != nil {
			return nil, err
		}
		recent = append(recent, deploy)
	}

	return recent, rows.Err()
}

func (s *Service) insertApp(ctx context.Context, name, repositoryURL, branch, rootDirectory string) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		"INSERT INTO apps (name, repository, branch, root_directory, created_at) VALUES (?, ?, ?, ?, ?)",
		name, repositoryURL, branch, rootDirectory, s.now().Unix())
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
	return "https://" + name + "." + s.publicIP + ".sslip.io"
}

var nameDisallowed = regexp.MustCompile(`[^a-z0-9-]+`)

// suggestName turns a repository name into a valid app name where it can.
func suggestName(repositoryName string) string {
	lowered := strings.ToLower(repositoryName)
	hyphenated := nameDisallowed.ReplaceAllString(lowered, "-")
	trimmed := strings.Trim(hyphenated, "-")
	if trimmed == "" {
		return ""
	}
	if trimmed[0] < 'a' || trimmed[0] > 'z' {
		trimmed = "app-" + trimmed
	}

	return strings.TrimRight(trimmed[:min(len(trimmed), 30)], "-")
}
