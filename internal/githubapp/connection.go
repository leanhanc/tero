package githubapp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/source"
)

// maxAppName is GitHub's limit on an App's name.
const maxAppName = 34

// DashboardKeys is what the dashboard asks of the key service for GitHub.
type DashboardKeys interface {
	BeginGitHubConnection(ctx context.Context, session string) (string, error)
	ApproveGitHubConnection(ctx context.Context, session, state string) error
	DisconnectGitHub(ctx context.Context, session string) error
	GitHubStatus(ctx context.Context) (keys.GitHubApp, error)
}

// Connection is the dashboard's side of connecting GitHub. It never holds
// the App's secrets: the agent stores them in the key service.
type Connection struct {
	DB     *sql.DB
	Keys   DashboardKeys
	Domain string
	Now    func() time.Time
}

// Start is what the browser posts to GitHub to create the App: the page to
// post to, the manifest and the state GitHub sends back.
type Start struct {
	Action   string `json:"action"`
	Manifest string `json:"manifest"`
	State    string `json:"state"`
}

// Begin starts connecting GitHub on a confirmed session. organization is
// empty for the admin's personal account.
func (c Connection) Begin(ctx context.Context, session, organization string) (Start, error) {
	if organization != "" {
		if err := source.CheckOwner(organization); err != nil {
			return Start{}, err
		}
	}

	manifest, err := json.Marshal(c.manifest())
	if err != nil {
		return Start{}, err
	}
	state, err := c.Keys.BeginGitHubConnection(ctx, session)
	if err != nil {
		return Start{}, err
	}

	return Start{Action: newAppPage(organization, state), Manifest: string(manifest), State: state}, nil
}

// manifest is the App Tero asks GitHub to create: private, read-only, and
// subscribed to pushes. Installation events reach an App without asking.
func (c Connection) manifest() map[string]any {
	origin := "https://" + c.Domain
	name := "Tero " + c.Domain
	if len(name) > maxAppName {
		name = name[:maxAppName]
	}

	return map[string]any{
		"name":                name,
		"url":                 origin,
		"hook_attributes":     map[string]any{"url": origin + "/api/github/webhook", "active": true},
		"redirect_url":        origin + "/github/connected",
		"setup_url":           origin + "/github/installed",
		"setup_on_update":     true,
		"public":              false,
		"default_permissions": map[string]string{"contents": "read", "metadata": "read"},
		"default_events":      []string{"push"},
	}
}

func newAppPage(organization, state string) string {
	page := "https://github.com/settings/apps/new"
	if organization != "" {
		page = "https://github.com/organizations/" + organization + "/settings/apps/new"
	}

	return page + "?state=" + url.QueryEscape(state)
}

// Finish approves the state GitHub sent the admin back with, on the session
// that began, and asks the agent to store the App. It returns the request to
// wait for.
func (c Connection) Finish(ctx context.Context, session, code, state string) (int64, error) {
	if err := c.Keys.ApproveGitHubConnection(ctx, session, state); err != nil {
		return 0, err
	}

	return QueueConnect(ctx, c.DB, code, state, c.Now())
}

// Status is the GitHub section of the settings page.
type Status struct {
	App          keys.GitHubApp `json:"app"`
	Installation *Installation  `json:"installation"`
	Repositories []Repository   `json:"repositories"`
	// InstallURL is where the admin installs the App on repositories.
	InstallURL string `json:"installUrl,omitempty"`
	// SettingsURL is the App's settings on GitHub, where it can be deleted.
	SettingsURL string `json:"settingsUrl,omitempty"`
}

// Status reads the App from the key service and the installation from the
// database.
func (c Connection) Status(ctx context.Context) (Status, error) {
	app, err := c.Keys.GitHubStatus(ctx)
	if err != nil {
		return Status{}, err
	}

	status := Status{App: app, Repositories: []Repository{}}
	installation, isInstalled, err := LoadInstallation(ctx, c.DB)
	if err != nil {
		return Status{}, err
	}
	if isInstalled {
		status.Installation = &installation
		if status.Repositories, err = Repositories(ctx, c.DB); err != nil {
			return Status{}, err
		}
	}
	if app.IsConnected {
		status.InstallURL = "https://github.com/apps/" + url.PathEscape(app.Slug) + "/installations/new"
		status.SettingsURL = SettingsPage(app)
	}

	return status, nil
}

// SettingsPage is the App's settings on GitHub, where it can be deleted.
func SettingsPage(app keys.GitHubApp) string {
	slug := url.PathEscape(app.Slug)
	if app.IsOrgOwned {
		return "https://github.com/organizations/" + url.PathEscape(app.Owner) + "/settings/apps/" + slug
	}

	return "https://github.com/settings/apps/" + slug
}

// Disconnect deletes the App's secrets on a confirmed session, and the
// installation Tero knew. Apps from public repositories go back to polling;
// the rest can't deploy until GitHub is connected again.
func (c Connection) Disconnect(ctx context.Context, session string) error {
	if err := c.Keys.DisconnectGitHub(ctx, session); err != nil {
		return err
	}

	return ForgetInstallation(ctx, c.DB)
}

// Refresh asks the agent to read the installation again, after the admin
// installed the App or changed its repositories.
func (c Connection) Refresh(ctx context.Context) (int64, error) {
	return QueueRefresh(ctx, c.DB, c.Now())
}

// RequestAnswer returns the agent's answer to a request.
func (c Connection) RequestAnswer(ctx context.Context, requestID int64) (Answer, error) {
	answer, err := AnswerOf(ctx, c.DB, requestID)
	if errors.Is(err, sql.ErrNoRows) {
		return Answer{}, ErrRequestNotFound
	}

	return answer, err
}

// ErrRequestNotFound means no request has that id.
var ErrRequestNotFound = errors.New("Not found")
