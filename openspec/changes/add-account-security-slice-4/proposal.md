## Why

The admin can't change their password, see where they are logged in, or end a session from the dashboard: `sudo tero reset-login` is the only way, and it wipes the TOTP secret too. That leaves ASVS 6.2.2, 6.2.3, 7.4.5 and 7.5.2 planned, and new passwords aren't checked for context words (6.2.11). Every login also needs a password and a TOTP code, which a convincing phishing page can relay in real time; a passkey can't be relayed, because the browser ties it to the dashboard's domain (ASVS 6.3.3, Level 3).

Slice 3 left one gap open on purpose: sessions live in `tero`'s database, so the dashboard, the agent, a build that reaches uid `tero` or a planted user unit can each insert a session and log in without the key service. Account security is the slice that adds session management, so it is where sessions move into `tero-keys`, together with trusted browsers and login throttles, which they depend on.

## What Changes

- A settings page in the dashboard, the first one, with three parts: password, passkeys and sessions.
- **Password change** with the current password, a fresh TOTP code and the new password, which must pass the length, blocklist and breach checks and contain no context word ("tero", "admin", the dashboard domain or one of its labels, or the server's public IP). The context-word check applies to setup too. A checkbox, on by default, ends every other session.
- **Sessions**: list every active session (when it started, when it was last used, from which IP and browser, and how it logged in), and end any one of them or all the others.
- **Confirming it's you**: ending sessions and adding, renaming or removing a passkey need the password and a fresh TOTP code entered on this session in the last 5 minutes (ASVS 7.5.1, 7.5.2).
- **Passkeys**: add one or more after confirming, name, rename and remove them, and log in with a passkey alone. Password and TOTP stay as the fallback and can't be removed. Adding or removing a passkey offers to end every other session (ASVS 7.4.3).
- **Sessions, trusted browsers and login throttles move into `tero-keys`**. It creates a session only after it has checked a login itself, and the dashboard asks it to check the session cookie on every request. `tero`'s database keeps only apps and deploys.
- Every request that reads or changes the account names the caller's session, so a compromised dashboard with no admin session can't list sessions, read events or change anything.
- `sudo tero reset-login` and `sudo tero assume-breach` also delete every passkey, and no longer touch `tero`'s database.

## Capabilities

### New Capabilities

- `account-security`: the settings page, password change, the confirmation step, the session list and passkeys.

### Modified Capabilities

- `key-service`: holds sessions, trusted browsers, login throttles and passkeys, checks passkey logins, and serves the account requests; the dashboard's verbs change.
- `dashboard-access`: login with a passkey; context words refused at setup; sessions checked by the key service; reset deletes passkeys.
- `process-isolation`: no process running as `tero` can create a dashboard session.
- `breach-response`: passkeys are deleted, and the output says to remove them from the admin's devices.
- `security-log`: events for password changes, passkeys, confirmations and ended sessions.

## Impact

- New Go dependency `github.com/go-webauthn/webauthn` for parsing and verifying passkey registrations and assertions (its CBOR dependency `fxamacker/cbor` is already in the module graph).
- `tero-keys`' migration `0001_keys.sql` gains `sessions`, `trusted_devices`, `login_throttles`, `passkeys` and a WebAuthn user handle on `admin`; `tero`'s `0001_dashboard_access.sql` is removed. Migrations are edited in place: there are no installs to upgrade.
- `tero-keys` reads the dashboard domain from `/etc/tero/config.json` at start, for the passkey relying party ID.
- `internal/auth`'s throttle and session code moves into `internal/keys`; the `_login-link` child that opened `tero`'s database for `reset-login` goes away.
- New dashboard page `web/src/features/settings/`, and a passkey button on the login page, in English and Spanish, markup only.
- `docs/security/asvs-l2.md`: 6.2.2, 6.2.3, 6.2.11, 6.1.2, 7.4.3, 7.4.5, 7.5.1 and 7.5.2 met with tests; 6.3.3 (Level 3) met; the login paths, sessions and cryptography sections describe passkeys and sessions in `tero-keys`.
- `openspec/roadmap.md`: slice 4 gains the move of sessions into `tero-keys`.
