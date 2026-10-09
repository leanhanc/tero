-- The single admin account. The CHECK keeps it to one row.
CREATE TABLE admin (
	id             INTEGER PRIMARY KEY CHECK (id = 1),
	password_hash  TEXT    NOT NULL,
	totp_secret    TEXT    NOT NULL,
	last_totp_step INTEGER NOT NULL,
	created_at     INTEGER NOT NULL
);

-- One-time setup links. Only a hash of the token is stored. A link moves
-- from issued to pending (password chosen, TOTP shown) to used.
CREATE TABLE setup_links (
	token_hash            TEXT    PRIMARY KEY,
	issued_at             INTEGER NOT NULL,
	expires_at            INTEGER NOT NULL,
	used_at               INTEGER,
	pending_password_hash TEXT,
	pending_totp_secret   TEXT
);

CREATE TABLE sessions (
	token_hash   TEXT    PRIMARY KEY,
	created_at   INTEGER NOT NULL,
	last_seen_at INTEGER NOT NULL
);

-- Browsers the admin has logged in from. They are throttled per device
-- instead of on the whole account (see auth/throttle.go).
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

CREATE TABLE security_events (
	id     INTEGER PRIMARY KEY AUTOINCREMENT,
	at     INTEGER NOT NULL,
	type   TEXT    NOT NULL,
	ip     TEXT    NOT NULL DEFAULT '',
	detail TEXT    NOT NULL DEFAULT ''
);
