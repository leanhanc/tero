package githubapp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/leanhanc/tero/internal/deploys"
	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/source"
)

// deliveryRetention matches GitHub's redelivery window: a delivery older
// than that can't come again.
const deliveryRetention = 3 * 24 * time.Hour

// EventAppender records a security event the dashboard saw.
type EventAppender interface {
	AppendEvent(ctx context.Context, eventType keys.EventType, ip string) error
}

// Webhooks acts on webhooks the key service already verified. A webhook is
// only ever a trigger: a push queues a deploy of the branch, and the agent
// builds the head it reads from GitHub itself, never the commit named here.
type Webhooks struct {
	DB     *sql.DB
	Events EventAppender
	Now    func() time.Time
}

// Handle acts on a verified delivery, once per delivery id. It reports
// false for a delivery already handled, which a replay or a redelivery can
// be.
func (w Webhooks) Handle(ctx context.Context, event, guid string, body []byte) (bool, error) {
	wasHandled, err := WasHandled(ctx, w.DB, guid)
	if err != nil || wasHandled {
		return false, err
	}

	if err := w.act(ctx, event, body); err != nil {
		return false, err
	}

	return true, w.markHandled(ctx, guid)
}

func (w Webhooks) act(ctx context.Context, event string, body []byte) error {
	switch event {
	case "push":
		return w.push(ctx, body)
	case "installation", "installation_repositories":
		return w.installationChanged(ctx)
	default:
		return nil
	}
}

// pushEvent is the part of a push webhook Tero reads.
type pushEvent struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	IsDeleted  bool   `json:"deleted"`
	Repository struct {
		ID    int64  `json:"id"`
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
			Name  string `json:"name"`
		} `json:"owner"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
	} `json:"sender"`
}

// maxLoginLength bounds what is stored of a pusher's login.
const maxLoginLength = 39

func (w Webhooks) push(ctx context.Context, body []byte) error {
	var push pushEvent
	if err := json.Unmarshal(body, &push); err != nil {
		return fmt.Errorf("read the push: %w", err)
	}

	branch, isBranch := strings.CutPrefix(push.Ref, "refs/heads/")
	if !isBranch || push.IsDeleted || push.Repository.ID == 0 {
		return nil
	}

	if err := w.refreshRepositoryName(ctx, push); err != nil {
		return err
	}

	appIDs, err := w.appsTracking(ctx, push.Repository.ID, branch)
	if err != nil {
		return err
	}

	trigger := deploys.Trigger{
		Type:         deploys.TriggerPush,
		PushedBy:     truncate(push.Sender.Login, maxLoginLength),
		RequestedSHA: truncate(push.After, 64),
	}
	for _, appID := range appIDs {
		if _, err := deploys.Enqueue(ctx, w.DB, appID, trigger, w.Now()); err != nil {
			return err
		}
	}

	return nil
}

// appsTracking returns the apps that deploy on push from the repository's
// branch.
func (w Webhooks) appsTracking(ctx context.Context, repositoryID int64, branch string) ([]int64, error) {
	rows, err := w.DB.QueryContext(ctx,
		"SELECT id FROM apps WHERE github_repository_id = ? AND branch = ? AND deploy_on_push = 1", repositoryID, branch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var appIDs []int64
	for rows.Next() {
		var appID int64
		if err := rows.Scan(&appID); err != nil {
			return nil, err
		}
		appIDs = append(appIDs, appID)
	}

	return appIDs, rows.Err()
}

// refreshRepositoryName follows a rename or transfer: the push names the
// repository as it is now. A name GitHub wouldn't accept is ignored.
func (w Webhooks) refreshRepositoryName(ctx context.Context, push pushEvent) error {
	owner := push.Repository.Owner.Login
	if owner == "" {
		owner = push.Repository.Owner.Name
	}
	repository, err := source.ParseRepository("https://github.com/" + owner + "/" + push.Repository.Name)
	if err != nil {
		return nil
	}

	_, err = w.DB.ExecContext(ctx, "UPDATE apps SET repository = ? WHERE github_repository_id = ?",
		repository.URL(), push.Repository.ID)
	if err != nil {
		return err
	}
	_, err = w.DB.ExecContext(ctx, "UPDATE github_repositories SET owner = ?, name = ? WHERE id = ?",
		repository.Owner, repository.Name, push.Repository.ID)
	return err
}

// installationChanged records the change and asks the agent to read the
// installation again; the webhook's own list of repositories isn't trusted
// for that.
func (w Webhooks) installationChanged(ctx context.Context) error {
	if err := w.Events.AppendEvent(ctx, keys.EventGitHubInstallationChanged, ""); err != nil {
		return err
	}

	_, err := QueueRefresh(ctx, w.DB, w.Now())
	return err
}

func (w Webhooks) markHandled(ctx context.Context, guid string) error {
	now := w.Now()
	if _, err := w.DB.ExecContext(ctx, "INSERT OR IGNORE INTO webhook_deliveries (guid, handled_at) VALUES (?, ?)", guid, now.Unix()); err != nil {
		return err
	}

	_, err := w.DB.ExecContext(ctx, "DELETE FROM webhook_deliveries WHERE handled_at < ?", now.Add(-deliveryRetention).Unix())
	return err
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}

	return value[:limit]
}
