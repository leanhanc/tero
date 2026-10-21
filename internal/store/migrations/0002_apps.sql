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
	created_at     INTEGER NOT NULL
);

-- One row per deploy. The service queues them and the agent moves them
-- through the other states; failed_step and reason are set on failure.
CREATE TABLE deploys (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	app_id         INTEGER NOT NULL REFERENCES apps (id),
	state          TEXT    NOT NULL DEFAULT 'queued'
		CHECK (state IN ('queued', 'building', 'starting', 'live', 'failed', 'superseded')),
	failed_step    TEXT    NOT NULL DEFAULT '',
	reason         TEXT    NOT NULL DEFAULT '',
	commit_sha     TEXT    NOT NULL DEFAULT '',
	commit_message TEXT    NOT NULL DEFAULT '',
	image          TEXT    NOT NULL DEFAULT '',
	container      TEXT    NOT NULL DEFAULT '',
	host_port      INTEGER NOT NULL DEFAULT 0,
	queued_at      INTEGER NOT NULL,
	started_at     INTEGER NOT NULL DEFAULT 0,
	finished_at    INTEGER NOT NULL DEFAULT 0
);

-- An app has at most one live deploy and at most one in progress.
CREATE UNIQUE INDEX deploys_one_live ON deploys (app_id) WHERE state = 'live';
CREATE UNIQUE INDEX deploys_one_in_progress ON deploys (app_id)
	WHERE state IN ('queued', 'building', 'starting');

CREATE INDEX deploys_by_app ON deploys (app_id, id);
CREATE INDEX deploys_by_state ON deploys (state, id);
