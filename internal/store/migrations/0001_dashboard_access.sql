-- The admin's password, TOTP secret and setup links live in the key
-- service's own database (internal/keys), out of tero's reach.

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
