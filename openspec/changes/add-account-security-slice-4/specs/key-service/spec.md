## ADDED Requirements

### Requirement: Sessions held by the key service
Dashboard sessions, trusted browsers and login throttles SHALL be stored only in the key service's database. The key service SHALL create a session only after it checked a password and TOTP code, a passkey, or a setup link's code itself, and SHALL check the session token on every dashboard request that needs one. Sessions SHALL end after 30 minutes without a request and 12 hours after login. Each session SHALL have a random id for listing, separate from its token, and only the token's SHA-256 hash SHALL be stored.

#### Scenario: Session created only by a login
- **WHEN** the admin logs in
- **THEN** the session row is in `tero-keys`' database and the dashboard received only the token for the cookie

#### Scenario: Invented token
- **WHEN** a request carries a session cookie the key service never issued
- **THEN** it is refused with `401`

#### Scenario: Ended session refused at once
- **WHEN** a session is ended from another browser's session list
- **THEN** the very next request with it is refused

### Requirement: Account requests need the caller's session
Every key service request from the dashboard that lists sessions, ends sessions, confirms, changes the password, manages passkeys or lists events SHALL carry a live session token, and SHALL be refused without one. Ending sessions and managing passkeys SHALL also need that session to be confirmed within the last 5 minutes.

#### Scenario: Dashboard asks without a session
- **WHEN** the dashboard sends `session.list`, `passkey.register.begin` or `events.list` with no token or an ended one
- **THEN** the key service refuses it

#### Scenario: Unconfirmed session ends another
- **WHEN** the dashboard sends `session.revoke` with a live session that isn't confirmed
- **THEN** the key service refuses it and every session keeps working

### Requirement: Passkeys verified by the key service
The key service SHALL issue passkey challenges, keep them for at most 2 minutes, accept each once, and verify registrations and assertions against the dashboard domain it reads from the root-owned configuration, with user verification required. It SHALL keep at most 1,024 open login challenges and at most 4 per IP or IPv6 /64, and SHALL NOT charge the throttles for an answer whose challenge is gone or expired. Passkey public keys SHALL be stored only in its database.

#### Scenario: Expired challenge
- **WHEN** a passkey answer arrives more than 2 minutes after its challenge was issued
- **THEN** it is refused

#### Scenario: Flood of login challenges
- **WHEN** an anonymous client asks for thousands of login challenges
- **THEN** at most 4 from its address and 1,024 in all are kept, an earlier challenge from another address still works, and the key service's memory stays within its limit

#### Scenario: Passkey without user verification
- **WHEN** an assertion arrives without the user-verified flag
- **THEN** it is refused

## MODIFIED Requirements

### Requirement: Admin credentials held by the key service
The admin's password hash, TOTP secret, passkeys and setup links SHALL be stored and checked only by the key service. TOTP secrets SHALL be encrypted with AES-256-GCM under the master key, with the table, column, row and key version as additional data. A stored TOTP secret, password hash, session token hash or passkey private detail SHALL never leave the key service (ASVS v5.0.0-13.3.1).

#### Scenario: Secret encrypted at rest
- **WHEN** the admin has completed setup
- **THEN** neither `tero`'s database nor `tero-keys`' database holds the TOTP secret in clear, and `tero`'s database holds no password hash

#### Scenario: Ciphertext moved to another row
- **WHEN** a sealed TOTP secret is copied into another row's column
- **THEN** it doesn't decrypt there

#### Scenario: Replayed code
- **WHEN** a code that already logged the admin in is sent to `login.password`, `account.confirm` or `password.change` again
- **THEN** it is refused

### Requirement: Code guessing limit in the key service
The key service SHALL refuse every check of a password and TOTP code (`login.password`, `account.confirm` and `password.change`) for 24 hours once 20 such checks within 24 hours had the right password and a wrong code, independent of the login throttles. Checks with a wrong password SHALL NOT count toward it. Passkey logins SHALL keep working while it is locked.

#### Scenario: Guessing codes with the password
- **WHEN** 20 checks within 24 hours, from login, confirmation or password change, have the right password and a wrong code
- **THEN** `tero-keys` refuses password and code checks for the next 24 hours, and has recorded a `totp_failed` or `confirm_totp_failed` event for each and a `totp_locked` event

#### Scenario: Guessing passwords from the dashboard
- **WHEN** the dashboard sends more than 30 password and code checks in one minute
- **THEN** `tero-keys` refuses the rest as busy without checking them, records one `rate_limited` event with their count, and lists wrong codes with the right password to the dashboard as `login_failed` or `confirm_failed`

#### Scenario: Strangers can't lock the admin out
- **WHEN** any number of login attempts have a wrong password
- **THEN** they don't count toward the 24-hour limit, and the admin can still log in from a trusted browser or with a passkey
