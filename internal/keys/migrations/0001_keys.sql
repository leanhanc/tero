-- The single admin account. The CHECK keeps it to one row. The TOTP secret is
-- sealed with the master key version in totp_key_version (see seal.go).
-- webauthn_user_id is the random user handle passkeys are made for.
CREATE TABLE admin (
	id               INTEGER PRIMARY KEY CHECK (id = 1),
	password_hash    TEXT    NOT NULL,
	totp_key_version INTEGER NOT NULL,
	totp_secret      BLOB    NOT NULL,
	last_totp_step   INTEGER NOT NULL,
	webauthn_user_id BLOB    NOT NULL,
	created_at       INTEGER NOT NULL
);

-- The admin's passkeys. credential is go-webauthn's credential record as
-- JSON: the public key, flags and signature counter, none of it secret.
CREATE TABLE passkeys (
	id            TEXT    PRIMARY KEY,
	credential_id BLOB    NOT NULL UNIQUE,
	credential    TEXT    NOT NULL,
	name          TEXT    NOT NULL,
	created_at    INTEGER NOT NULL,
	last_used_at  INTEGER NOT NULL DEFAULT 0
);

-- Dashboard sessions. id is what the session list shows and ends; only the
-- token's hash is stored. confirmed_at is when the admin last confirmed it's
-- them on this session, for the actions that need a fresh check.
CREATE TABLE sessions (
	id           TEXT    PRIMARY KEY,
	token_hash   TEXT    NOT NULL UNIQUE,
	method       TEXT    NOT NULL CHECK (method IN ('password', 'passkey', 'setup')),
	created_at   INTEGER NOT NULL,
	last_seen_at INTEGER NOT NULL,
	login_ip     TEXT    NOT NULL,
	last_ip      TEXT    NOT NULL,
	user_agent   TEXT    NOT NULL,
	confirmed_at INTEGER NOT NULL DEFAULT 0
);

-- Browsers the admin has logged in from. They are throttled per device
-- instead of on the whole account (see throttle.go).
CREATE TABLE trusted_devices (
	token_hash   TEXT    PRIMARY KEY,
	created_at   INTEGER NOT NULL,
	last_used_at INTEGER NOT NULL
);

-- Failed-login counters, keyed by "ip:<address or /64>", "device:<hash>" or
-- "account".
CREATE TABLE login_throttles (
	key          TEXT    PRIMARY KEY,
	failures     INTEGER NOT NULL,
	last_failure INTEGER NOT NULL,
	locked_until INTEGER NOT NULL
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

-- The GitHub App this server created, at most one. The private key and
-- webhook secret are sealed with the master key version in key_version; the
-- rest isn't secret.
CREATE TABLE github_app (
	id             INTEGER PRIMARY KEY CHECK (id = 1),
	app_id         INTEGER NOT NULL,
	slug           TEXT    NOT NULL,
	owner          TEXT    NOT NULL,
	is_org_owned   INTEGER NOT NULL CHECK (is_org_owned IN (0, 1)),
	key_version    INTEGER NOT NULL,
	private_key    BLOB    NOT NULL,
	webhook_secret BLOB    NOT NULL,
	connected_at   INTEGER NOT NULL
);

-- Apps' environment variables. The app's name never changes, so it identifies
-- the app here. value is sealed with the master key version in key_version,
-- for the app and variable named in the row. value_bytes counts the clear
-- value, for the per-app limit. scope says which phases of a deploy get it.
CREATE TABLE app_variables (
	app         TEXT    NOT NULL,
	name        TEXT    NOT NULL,
	key_version INTEGER NOT NULL,
	value       BLOB    NOT NULL,
	value_bytes INTEGER NOT NULL,
	scope       TEXT    NOT NULL CHECK (scope IN ('runtime', 'build', 'both')),
	is_sealed   INTEGER NOT NULL CHECK (is_sealed IN (0, 1)),
	replaced_at INTEGER NOT NULL,
	PRIMARY KEY (app, name)
);

-- The source each app with variables may have them released for. The dashboard
-- can't change a row without a confirmed session, and the shared database can't
-- change it at all.
-- repository_id is GitHub's id for the repository at that URL, pinned by the
-- first release after an approval: a repository that replaces it under the same
-- name, after a deletion or a rename, has another id. replaced_by is the id a
-- release was refused for, until the admin approves again.
CREATE TABLE app_sources (
	app            TEXT    PRIMARY KEY,
	repository     TEXT    NOT NULL,
	branch         TEXT    NOT NULL,
	root_directory TEXT    NOT NULL,
	repository_id  INTEGER NOT NULL DEFAULT 0,
	replaced_by    INTEGER NOT NULL DEFAULT 0,
	approved_at    INTEGER NOT NULL
);

-- Connections the admin started on a confirmed session. Only the state's
-- hash is stored. The admin approves it when GitHub sends them back, and the
-- agent then uses it once to store the App.
CREATE TABLE github_connections (
	state_hash  TEXT    PRIMARY KEY,
	session_id  TEXT    NOT NULL,
	expires_at  INTEGER NOT NULL,
	approved_at INTEGER NOT NULL DEFAULT 0
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
-- events_dropped. subject names what the event concerns, such as an app or a
-- variable, and detail adds what it needs, such as a deploy.
CREATE TABLE security_events (
	id     INTEGER PRIMARY KEY AUTOINCREMENT,
	at     INTEGER NOT NULL,
	type   TEXT    NOT NULL,
	source TEXT    NOT NULL,
	ip     TEXT    NOT NULL DEFAULT '',
	count  INTEGER NOT NULL DEFAULT 1,
	subject TEXT   NOT NULL DEFAULT '',
	detail TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX security_events_at ON security_events (at);
CREATE INDEX security_events_by_subject ON security_events (type, subject, id) WHERE subject != '';
