## ADDED Requirements

### Requirement: Passkey login
The login page SHALL offer to log in with one of the admin's passkeys alone, with no password or code. The key service SHALL check the passkey and create the session. A failed passkey login SHALL count toward the per-IP throttle only (ASVS v5.0.0-6.3.3).

#### Scenario: Log in with a passkey
- **WHEN** the admin, with a passkey added, chooses "Log in with a passkey" and approves it on their authenticator
- **THEN** the admin is logged in, and the session list shows the session as logged in with a passkey

#### Scenario: No passkey yet
- **WHEN** the admin has no passkey and tries to log in with one
- **THEN** login fails and the password and code form still works

#### Scenario: Reused challenge
- **WHEN** a passkey answer that already logged the admin in is sent again
- **THEN** it is refused and no session is created

#### Scenario: Strangers can't lock the admin out with passkeys
- **WHEN** someone sends many invalid passkey answers from one IP
- **THEN** that IP is throttled and the admin's password and passkey logins from other browsers still work

## MODIFIED Requirements

### Requirement: Password plus required TOTP
Login SHALL require either the admin password and a valid TOTP code, or one of the admin's passkeys. TOTP cannot be turned off, and password with TOTP SHALL always work as the fallback. The key service SHALL check every factor and create every session; the dashboard SHALL NOT store or read the password hash, the TOTP secret, passkeys or sessions.

#### Scenario: Correct password and code
- **WHEN** the admin submits the correct password and a current TOTP code
- **THEN** the admin is logged in

#### Scenario: Correct password, wrong code
- **WHEN** the admin submits the correct password and an invalid TOTP code
- **THEN** login fails without saying which factor was wrong

#### Scenario: Setup without TOTP
- **WHEN** the admin tries to finish setup without confirming a TOTP code
- **THEN** setup does not complete

#### Scenario: Key service not answering
- **WHEN** the admin submits the login form, or loads any signed-in page, while `tero-keys` is stopped
- **THEN** the request fails with a message that the server's key service isn't running, and no session is created

#### Scenario: No credentials in the dashboard's database
- **WHEN** setup has completed and the admin has logged in
- **THEN** `tero`'s database has no table that holds a password hash, a TOTP secret, a setup link, a passkey, a session, a trusted browser or a login throttle

### Requirement: Login recovery from the server
`sudo tero reset-login` SHALL clear the admin's password, TOTP and passkeys, end every session, forget every trusted browser and throttle, and print a new one-time setup link. It SHALL NOT be reachable from the dashboard. When the master key can't be opened, it SHALL set the unreadable key files aside, seal a new key and start the key service first.

#### Scenario: Admin loses their authenticator
- **WHEN** the admin runs `sudo tero reset-login` on the server
- **THEN** existing sessions are revoked, the old password, TOTP and passkeys stop working, and a new setup link is printed

#### Scenario: Server settings unaffected by reset
- **WHEN** `sudo tero reset-login` runs
- **THEN** the dashboard domain, its certificate, and the host hardening are unchanged

#### Scenario: Master key lost
- **WHEN** `tero-keys` failed to start because its key can't be opened and the admin runs `sudo tero reset-login`
- **THEN** the unreadable files are moved under `/etc/tero/keys-unreadable/`, a new master key is sealed, `tero-keys` starts, and the printed setup link works
