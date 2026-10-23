-- The single admin account. The CHECK keeps it to one row. The TOTP secret is
-- sealed with the master key version in totp_key_version (see seal.go).
CREATE TABLE admin (
	id               INTEGER PRIMARY KEY CHECK (id = 1),
	password_hash    TEXT    NOT NULL,
	totp_key_version INTEGER NOT NULL,
	totp_secret      BLOB    NOT NULL,
	last_totp_step   INTEGER NOT NULL,
	created_at       INTEGER NOT NULL
);

-- One-time setup links, issued only by root. Only a hash of the token is
-- stored. A link moves from issued to pending (password chosen, TOTP shown)
-- to used.
CREATE TABLE setup_links (
	token_hash               TEXT    PRIMARY KEY,
	issued_at                INTEGER NOT NULL,
	expires_at               INTEGER NOT NULL,
	used_at                  INTEGER,
	pending_password_hash    TEXT,
	pending_totp_key_version INTEGER,
	pending_totp_secret      BLOB
);

-- Logins with the right password and a wrong code, for the code guessing
-- limit.
CREATE TABLE code_failures (
	at INTEGER NOT NULL
);

CREATE TABLE login_lock (
	id           INTEGER PRIMARY KEY CHECK (id = 1),
	locked_until INTEGER NOT NULL
);

-- source is who recorded the event: tero-keys itself, the dashboard or root.
-- count is above 1 only for events that stand for several, such as
-- events_dropped.
CREATE TABLE security_events (
	id     INTEGER PRIMARY KEY AUTOINCREMENT,
	at     INTEGER NOT NULL,
	type   TEXT    NOT NULL,
	source TEXT    NOT NULL,
	ip     TEXT    NOT NULL DEFAULT '',
	count  INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX security_events_at ON security_events (at);
