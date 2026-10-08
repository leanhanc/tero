# dashboard-access Specification

## Purpose
Let exactly one admin into the dashboard, safely from the first minute: HTTPS only, no default password, password plus required TOTP, and a root-only recovery path on the server.

## Requirements

### Requirement: HTTPS only
The dashboard SHALL be served only over HTTPS on the dashboard domain.

#### Scenario: Plain HTTP request
- **WHEN** a client requests the dashboard over plain HTTP
- **THEN** it is redirected to HTTPS and no dashboard content or cookie is served over HTTP

#### Scenario: Session cookie flags
- **WHEN** the admin logs in
- **THEN** the session cookie is set with `Secure` and `HttpOnly`

### Requirement: One-time setup link
The first login SHALL happen through a single-use link printed by `sudo tero init` or `sudo tero reset-login`. There SHALL be no default password.

#### Scenario: Admin completes setup
- **WHEN** the admin opens the setup link, chooses a password, and confirms a TOTP code from their authenticator
- **THEN** the admin account is created and the admin is logged in

#### Scenario: Link reused
- **WHEN** anyone opens a setup link that was already used
- **THEN** the dashboard rejects it and creates no account

#### Scenario: No login before setup
- **WHEN** no admin account exists and someone opens the login page
- **THEN** no credentials are accepted and the page says to use the setup link from the server

### Requirement: Password plus required TOTP
Login SHALL require both the admin password and a valid TOTP code. TOTP cannot be turned off.

#### Scenario: Correct password and code
- **WHEN** the admin submits the correct password and a current TOTP code
- **THEN** the admin is logged in

#### Scenario: Correct password, wrong code
- **WHEN** the admin submits the correct password and an invalid TOTP code
- **THEN** login fails without saying which factor was wrong

#### Scenario: Setup without TOTP
- **WHEN** the admin tries to finish setup without confirming a TOTP code
- **THEN** setup does not complete

### Requirement: Single admin
Tero v1 SHALL have exactly one admin account.

#### Scenario: Second account
- **WHEN** an admin account exists
- **THEN** the dashboard offers no way to create another account

### Requirement: Login recovery from the server
`sudo tero reset-login` SHALL clear the admin's password and TOTP and print a new one-time setup link. It SHALL NOT be reachable from the dashboard.

#### Scenario: Admin loses their authenticator
- **WHEN** the admin runs `sudo tero reset-login` on the server
- **THEN** existing sessions are revoked, the old password and TOTP stop working, and a new setup link is printed

#### Scenario: Server settings unaffected by reset
- **WHEN** `sudo tero reset-login` runs
- **THEN** the dashboard domain, its certificate, and the host hardening are unchanged
