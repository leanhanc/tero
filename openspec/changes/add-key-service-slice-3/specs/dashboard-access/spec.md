## MODIFIED Requirements

### Requirement: Password plus required TOTP
Login SHALL require both the admin password and a valid TOTP code. TOTP cannot be turned off. The key service SHALL check both; the dashboard SHALL NOT store or read the password hash or the TOTP secret.

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
- **WHEN** the admin submits the login form while `tero-keys` is stopped
- **THEN** login fails with a message that the server's key service isn't running and to run `sudo tero reset-login`, and no session is created

#### Scenario: No credentials in the dashboard's database
- **WHEN** setup has completed
- **THEN** `tero`'s database has no table that holds a password hash, a TOTP secret or a setup link

### Requirement: Login recovery from the server
`sudo tero reset-login` SHALL clear the admin's password and TOTP and print a new one-time setup link. It SHALL NOT be reachable from the dashboard. When the master key can't be opened, it SHALL set the unreadable key files aside, seal a new key and start the key service first.

#### Scenario: Admin loses their authenticator
- **WHEN** the admin runs `sudo tero reset-login` on the server
- **THEN** existing sessions are revoked, the old password and TOTP stop working, and a new setup link is printed

#### Scenario: Server settings unaffected by reset
- **WHEN** `sudo tero reset-login` runs
- **THEN** the dashboard domain, its certificate, and the host hardening are unchanged

#### Scenario: Master key lost
- **WHEN** `tero-keys` failed to start because its key can't be opened and the admin runs `sudo tero reset-login`
- **THEN** the unreadable files are moved under `/etc/tero/keys-unreadable/`, a new master key is sealed, `tero-keys` starts, and the printed setup link works
