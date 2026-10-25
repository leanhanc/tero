-- The app routes the service's built-in proxy is serving right now, written
-- by the service after each reload. The agent waits for a new release's port
-- to appear here before stopping the old release, so no request is sent to a
-- port that's gone.
CREATE TABLE served_routes (
	app_name  TEXT    PRIMARY KEY,
	host_port INTEGER NOT NULL
);
