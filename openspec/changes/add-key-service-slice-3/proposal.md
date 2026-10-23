## Why

ADR 0001 seals Tero's master key with systemd and hands it to "the Tero service". Slice 2 split that service in two: `tero serve`, which faces the internet, and `tero agent`, which runs Podman. Both run as `tero`. Giving the key to `tero serve` means one bug in the process that parses every request can read every secret, which is the failure the October 2026 Railway breach showed: values were encrypted and still read in clear through the component that could decrypt. Giving it to the agent doesn't work either: the agent is a user unit, systemd only seals credentials for user units from version 256 on (`systemd-creds --user`), and Ubuntu 24.04 ships 255. Both processes share a uid, and any process running as `tero` under its user manager can pose as the agent, so neither choice puts a user boundary around the key.

Today the TOTP secret sits unencrypted in `tero`'s SQLite database next to the password hash and the setup links, so a bug in the dashboard can read the TOTP secret or replace the admin's credentials and keep access after the bug is fixed. The security events the dashboard shows live in the same database, where anything running as `tero` can change or delete them. There is also no single response to "this server may be compromised": `sudo tero reset-login` replaces the login and nothing else.

This slice comes before account security and environment variables because both build on where the key lives.

## What Changes

- A new `tero keys` process runs as the system service `tero-keys.service` under its own system user `tero-keys`, started through `tero-keys.socket`. It has no network, no capabilities and the full systemd sandbox. It answers the dashboard (identified by its systemd unit) and root, and nobody else.
- `sudo tero init` generates a 256-bit master key and seals it with `systemd-creds` (TPM2 plus host key when the server has a TPM, host key otherwise). Only `tero-keys` receives it.
- The admin's credentials move to `tero-keys`: the password hash, the TOTP secret (encrypted with the master key) and the setup links. Only root can issue a setup link. `tero-keys` checks every login, so the dashboard can't read, replace or brute-force the credentials.
- Security events move to `tero-keys`. It records login and setup events itself, stamps the time and caller on the few events others may append, writes each one to the journal, and nothing running as `tero` can change or delete them.
- A key that won't open never takes apps offline; `sudo tero reset-login` replaces it.
- `sudo tero rotate-key` replaces the master key and re-encrypts everything under it.
- `sudo tero assume-breach` stops the dashboard, ends every session, forgets every trusted browser, replaces Caddy's certificates and keys, rotates the master key, resets the login, starts the dashboard again and prints a new setup link and a list of what to check and rotate outside the server.

## Capabilities

### New Capabilities

- `key-service`: the `tero-keys` process, its socket and caller check, the sealed master key, the admin's credentials, and `rotate-key`.
- `security-log`: security events recorded by `tero-keys`, append-only for every caller.
- `breach-response`: `sudo tero assume-breach`.

### Modified Capabilities

- `dashboard-access`: setup and login are checked by `tero-keys`; `reset-login` goes through it and recovers a lost key.
- `process-isolation`: a third process with its own user; the dashboard can't reach the key or the credentials.
- `server-init`: init creates the `tero-keys` user, seals the master key and installs and starts the socket and service before the dashboard.

## Impact

- New system user `tero-keys`, units `/etc/systemd/system/tero-keys.socket` and `tero-keys.service`, sealed keys under `/etc/tero/keys/`, database `/var/lib/tero-keys/keys.db`, socket `/run/tero-keys/keys.sock`.
- `tero`'s database loses the `admin`, `setup_links` and `security_events` tables; it keeps sessions, trusted browsers and login throttles. Migrations are edited in place: there are no installs to upgrade.
- `tero.service` wants `tero-keys.socket`; login fails closed while `tero-keys` can't answer, and apps keep serving.
- No new Go dependencies: AES-256-GCM from the standard library, and `golang.org/x/sys` (already indirect) for the socket peer checks.
- `docs/security/asvs-l2.md`: 11.1.1, 11.3.1 to 11.3.3, 13.3.1, 13.3.2, 13.3.4 and 16.4.2 with tests; 13.3.3 met on servers with a TPM2 only; the "Logging" and "Cryptography" sections describe `tero-keys`. ADR 0001's secrets section is rewritten for `tero-keys`.
- `docs/security/cis-subset.md`: the new system user has no shell and no password.
