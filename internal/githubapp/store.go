// Package githubapp keeps the GitHub App's installation, the repositories it
// covers and the dashboard's requests to the agent in the shared database,
// and is the agent's worker that answers those requests. Only the agent
// talks to GitHub with the App's credentials: the dashboard writes a request
// and reads the answer.
package githubapp

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// RequestKind is what the dashboard asks the agent to do.
type RequestKind string

const (
	// Connect trades the code from GitHub's manifest flow for the App's
	// secrets and hands them to the key service.
	Connect RequestKind = "connect"
	// RefreshInstallation reads the installation and its repositories from
	// GitHub again.
	RefreshInstallation RequestKind = "refresh_installation"
	// CheckSource checks a covered repository's branch and folder for the
	// new-app form.
	CheckSource RequestKind = "check_source"
)

// RequestStatus is where a request is.
type RequestStatus string

const (
	Pending RequestStatus = "pending"
	Done    RequestStatus = "done"
	Failed  RequestStatus = "failed"
)

// requestRetention is how long answered requests are kept for the
// dashboard to read.
const requestRetention = time.Hour

// Request is one request for the agent.
type Request struct {
	ID            int64
	Kind          RequestKind
	Code          string
	State         string
	RepositoryID  int64
	Branch        string
	RootDirectory string
}

// Answer is the agent's answer to a request.
type Answer struct {
	Status RequestStatus
	Result string
}

// QueueConnect asks the agent to finish connecting GitHub with the code
// GitHub sent the admin back with and the state the admin approved.
func QueueConnect(ctx context.Context, db *sql.DB, code, state string, now time.Time) (int64, error) {
	return queue(ctx, db, Request{Kind: Connect, Code: code, State: state}, now)
}

// QueueRefresh asks the agent to read the installation again. A refresh
// still pending is reused.
func QueueRefresh(ctx context.Context, db *sql.DB, now time.Time) (int64, error) {
	var pendingID int64
	err := db.QueryRowContext(ctx, "SELECT id FROM github_requests WHERE kind = ? AND status = 'pending'", string(RefreshInstallation)).
		Scan(&pendingID)
	if err == nil {
		return pendingID, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	return queue(ctx, db, Request{Kind: RefreshInstallation}, now)
}

// QueueSourceCheck asks the agent to check a covered repository's branch
// and folder.
func QueueSourceCheck(ctx context.Context, db *sql.DB, repositoryID int64, branch, rootDirectory string, now time.Time) (int64, error) {
	return queue(ctx, db, Request{Kind: CheckSource, RepositoryID: repositoryID, Branch: branch, RootDirectory: rootDirectory}, now)
}

func queue(ctx context.Context, db *sql.DB, req Request, now time.Time) (int64, error) {
	result, err := db.ExecContext(ctx, `
		INSERT INTO github_requests (kind, code, state, repository_id, branch, root_directory, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		string(req.Kind), req.Code, req.State, req.RepositoryID, req.Branch, req.RootDirectory, now.Unix())
	if err != nil {
		return 0, err
	}

	return result.LastInsertId()
}

// AnswerOf returns a request's answer; its status stays pending until the
// agent answers.
func AnswerOf(ctx context.Context, db *sql.DB, requestID int64) (Answer, error) {
	var answer Answer
	var status string
	err := db.QueryRowContext(ctx, "SELECT status, result FROM github_requests WHERE id = ?", requestID).Scan(&status, &answer.Result)
	answer.Status = RequestStatus(status)
	return answer, err
}

// WaitForAnswer polls a request until the agent answers or ctx ends.
func WaitForAnswer(ctx context.Context, db *sql.DB, requestID int64) (Answer, error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		answer, err := AnswerOf(ctx, db, requestID)
		if err != nil || answer.Status != Pending {
			return answer, err
		}

		select {
		case <-ctx.Done():
			return Answer{}, ctx.Err()
		case <-ticker.C:
		}
	}
}

// nextRequest returns the oldest pending request.
func nextRequest(ctx context.Context, db *sql.DB) (Request, bool, error) {
	var req Request
	var kind string
	err := db.QueryRowContext(ctx, `
		SELECT id, kind, code, state, repository_id, branch, root_directory
		FROM github_requests WHERE status = 'pending' ORDER BY id LIMIT 1`).
		Scan(&req.ID, &kind, &req.Code, &req.State, &req.RepositoryID, &req.Branch, &req.RootDirectory)
	if errors.Is(err, sql.ErrNoRows) {
		return Request{}, false, nil
	}
	req.Kind = RequestKind(kind)

	return req, err == nil, err
}

// answer records the agent's answer. The code and state are cleared: they
// are spent either way.
func answer(ctx context.Context, db *sql.DB, requestID int64, status RequestStatus, result string, now time.Time) error {
	_, err := db.ExecContext(ctx,
		"UPDATE github_requests SET status = ?, result = ?, code = '', state = '', answered_at = ? WHERE id = ?",
		string(status), result, now.Unix(), requestID)
	return err
}

func pruneRequests(ctx context.Context, db *sql.DB, now time.Time) error {
	cutoff := now.Add(-requestRetention).Unix()
	_, err := db.ExecContext(ctx, "DELETE FROM github_requests WHERE created_at < ?", cutoff)
	return err
}

// Installation is where the App is installed.
type Installation struct {
	ID             int64  `json:"-"`
	Account        string `json:"account"`
	IsOrganization bool   `json:"isOrganization"`
	IsSuspended    bool   `json:"isSuspended"`
}

// Repository is a repository the installation covers.
type Repository struct {
	ID            int64  `json:"id"`
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	IsPrivate     bool   `json:"isPrivate"`
	DefaultBranch string `json:"defaultBranch"`
}

// LoadInstallation returns the installation, if the App is installed.
func LoadInstallation(ctx context.Context, db *sql.DB) (Installation, bool, error) {
	var installation Installation
	err := db.QueryRowContext(ctx, "SELECT installation_id, account, is_organization, is_suspended FROM github_installation WHERE id = 1").
		Scan(&installation.ID, &installation.Account, &installation.IsOrganization, &installation.IsSuspended)
	if errors.Is(err, sql.ErrNoRows) {
		return Installation{}, false, nil
	}

	return installation, err == nil, err
}

// Repositories lists the covered repositories by owner and name.
func Repositories(ctx context.Context, db *sql.DB) ([]Repository, error) {
	rows, err := db.QueryContext(ctx,
		"SELECT id, owner, name, is_private, default_branch FROM github_repositories ORDER BY owner COLLATE NOCASE, name COLLATE NOCASE")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	repositories := []Repository{}
	for rows.Next() {
		var repository Repository
		if err := rows.Scan(&repository.ID, &repository.Owner, &repository.Name, &repository.IsPrivate, &repository.DefaultBranch); err != nil {
			return nil, err
		}
		repositories = append(repositories, repository)
	}

	return repositories, rows.Err()
}

// FindRepository returns a covered repository by GitHub's id.
func FindRepository(ctx context.Context, db *sql.DB, id int64) (Repository, bool, error) {
	var repository Repository
	err := db.QueryRowContext(ctx, "SELECT id, owner, name, is_private, default_branch FROM github_repositories WHERE id = ?", id).
		Scan(&repository.ID, &repository.Owner, &repository.Name, &repository.IsPrivate, &repository.DefaultBranch)
	if errors.Is(err, sql.ErrNoRows) {
		return Repository{}, false, nil
	}

	return repository, err == nil, err
}

// FindRepositoryByName returns a covered repository by owner and name,
// ignoring case as GitHub does.
func FindRepositoryByName(ctx context.Context, db *sql.DB, owner, name string) (Repository, bool, error) {
	var repository Repository
	err := db.QueryRowContext(ctx, `
		SELECT id, owner, name, is_private, default_branch FROM github_repositories
		WHERE owner = ? COLLATE NOCASE AND name = ? COLLATE NOCASE`, owner, name).
		Scan(&repository.ID, &repository.Owner, &repository.Name, &repository.IsPrivate, &repository.DefaultBranch)
	if errors.Is(err, sql.ErrNoRows) {
		return Repository{}, false, nil
	}

	return repository, err == nil, err
}

// ForgetInstallation deletes the installation and its repositories, after
// a disconnect or when GitHub reports none. Apps are unlinked from their
// repositories: public ones go back to polling, private ones fail to fetch
// until GitHub is connected again, which links them back.
func ForgetInstallation(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	statements := []string{
		"DELETE FROM github_installation",
		"DELETE FROM github_repositories",
		"UPDATE apps SET github_repository_id = 0, poll_failures = 0, poll_error = ''",
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// saveInstallation replaces the installation and its repositories, and
// links apps created from a public URL to a repository the installation now
// covers.
func saveInstallation(ctx context.Context, db *sql.DB, installation Installation, repositories []Repository, now time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `
		INSERT OR REPLACE INTO github_installation (id, installation_id, account, is_organization, is_suspended, refreshed_at)
		VALUES (1, ?, ?, ?, ?, ?)`,
		installation.ID, installation.Account, installation.IsOrganization, installation.IsSuspended, now.Unix())
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM github_repositories"); err != nil {
		return err
	}
	// Apps whose repository the installation no longer covers are unlinked;
	// the loop below links the covered ones again.
	if _, err := tx.ExecContext(ctx, "UPDATE apps SET github_repository_id = 0"); err != nil {
		return err
	}

	for _, repository := range repositories {
		_, err := tx.ExecContext(ctx,
			"INSERT INTO github_repositories (id, owner, name, is_private, default_branch) VALUES (?, ?, ?, ?, ?)",
			repository.ID, repository.Owner, repository.Name, repository.IsPrivate, repository.DefaultBranch)
		if err != nil {
			return err
		}

		repositoryURL := "https://github.com/" + repository.Owner + "/" + repository.Name
		_, err = tx.ExecContext(ctx,
			"UPDATE apps SET github_repository_id = ? WHERE github_repository_id = 0 AND repository = ? COLLATE NOCASE",
			repository.ID, repositoryURL)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}
