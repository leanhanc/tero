-- Apps the admin created. The name is the first label of the app's address,
-- so it never changes; the display name is what the dashboard shows, and can
-- change at any time.
CREATE TABLE apps (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	name           TEXT    NOT NULL UNIQUE,
	display_name   TEXT    NOT NULL DEFAULT '',
	repository     TEXT    NOT NULL,
	branch         TEXT    NOT NULL,
	root_directory TEXT    NOT NULL DEFAULT '',
	-- When off, the old release stops before the new one starts.
	run_together   INTEGER NOT NULL DEFAULT 1 CHECK (run_together IN (0, 1)),
	-- When off, pushes and polls queue nothing; Redeploy still does.
	deploy_on_push INTEGER NOT NULL DEFAULT 1 CHECK (deploy_on_push IN (0, 1)),
	-- GitHub's id for the repository, 0 until the App's installation covers
	-- it. Pushes are matched by it, so a rename keeps working.
	github_repository_id INTEGER NOT NULL DEFAULT 0,
	-- Failed polls in a row; polling stops at 3 and poll_error says why.
	poll_failures  INTEGER NOT NULL DEFAULT 0,
	poll_error     TEXT    NOT NULL DEFAULT '',
	created_at     INTEGER NOT NULL
);

-- One row per deploy. The service and the agent queue them, and the agent
-- moves them through the other states; failed_step and reason are set on
-- failure. trigger_type says what queued it, with the pusher's login and the
-- pushed commit for a push. The agent always builds the branch head, so
-- requested_sha is only shown, never built.
CREATE TABLE deploys (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	app_id         INTEGER NOT NULL REFERENCES apps (id),
	state          TEXT    NOT NULL DEFAULT 'queued'
		CHECK (state IN ('queued', 'building', 'starting', 'live', 'failed', 'superseded')),
	failed_step    TEXT    NOT NULL DEFAULT '',
	reason         TEXT    NOT NULL DEFAULT '',
	commit_sha     TEXT    NOT NULL DEFAULT '',
	commit_message TEXT    NOT NULL DEFAULT '',
	trigger_type   TEXT    NOT NULL DEFAULT 'manual'
		CHECK (trigger_type IN ('create', 'manual', 'push', 'poll')),
	pushed_by      TEXT    NOT NULL DEFAULT '',
	requested_sha  TEXT    NOT NULL DEFAULT '',
	image          TEXT    NOT NULL DEFAULT '',
	container      TEXT    NOT NULL DEFAULT '',
	host_port      INTEGER NOT NULL DEFAULT 0,
	queued_at      INTEGER NOT NULL,
	started_at     INTEGER NOT NULL DEFAULT 0,
	finished_at    INTEGER NOT NULL DEFAULT 0
);

-- An app has at most one live deploy, one running and one queued.
CREATE UNIQUE INDEX deploys_one_live ON deploys (app_id) WHERE state = 'live';
CREATE UNIQUE INDEX deploys_one_running ON deploys (app_id)
	WHERE state IN ('building', 'starting');
CREATE UNIQUE INDEX deploys_one_queued ON deploys (app_id) WHERE state = 'queued';

CREATE INDEX deploys_by_app ON deploys (app_id, id);
CREATE INDEX deploys_by_state ON deploys (state, id);

-- The GitHub App's installation as the agent last read it from GitHub. A
-- private App has at most one. The App's secrets are in the key service.
CREATE TABLE github_installation (
	id              INTEGER PRIMARY KEY CHECK (id = 1),
	installation_id INTEGER NOT NULL,
	account         TEXT    NOT NULL,
	is_organization INTEGER NOT NULL CHECK (is_organization IN (0, 1)),
	is_suspended    INTEGER NOT NULL CHECK (is_suspended IN (0, 1)),
	refreshed_at    INTEGER NOT NULL
);

-- The repositories the installation covers, by GitHub's id.
CREATE TABLE github_repositories (
	id             INTEGER PRIMARY KEY,
	owner          TEXT    NOT NULL,
	name           TEXT    NOT NULL,
	is_private     INTEGER NOT NULL CHECK (is_private IN (0, 1)),
	default_branch TEXT    NOT NULL
);

-- Work the dashboard asks of the agent, which alone talks to GitHub with the
-- App's credentials. The agent answers each with done or failed and a
-- result; answered requests are pruned after an hour.
CREATE TABLE github_requests (
	id            INTEGER PRIMARY KEY AUTOINCREMENT,
	kind          TEXT    NOT NULL CHECK (kind IN ('connect', 'refresh_installation', 'check_source')),
	code          TEXT    NOT NULL DEFAULT '',
	state         TEXT    NOT NULL DEFAULT '',
	repository_id INTEGER NOT NULL DEFAULT 0,
	branch        TEXT    NOT NULL DEFAULT '',
	root_directory TEXT   NOT NULL DEFAULT '',
	status        TEXT    NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'done', 'failed')),
	result        TEXT    NOT NULL DEFAULT '',
	created_at    INTEGER NOT NULL,
	answered_at   INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX github_requests_pending ON github_requests (status, id);

-- Webhook deliveries Tero handled, kept for GitHub's 3-day redelivery
-- window: a repeat is ignored, and the agent doesn't ask GitHub to redeliver
-- one Tero already handled.
CREATE TABLE webhook_deliveries (
	guid       TEXT    PRIMARY KEY,
	handled_at INTEGER NOT NULL
);
