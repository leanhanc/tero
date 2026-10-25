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
