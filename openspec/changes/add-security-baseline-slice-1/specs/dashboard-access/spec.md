## ADDED Requirements

### Requirement: Password policy
The admin password SHALL meet OWASP ASVS 5.0.0 V6.2. It SHALL be at least 15 characters (v5.0.0-6.2.1), passwords of at least 64 characters SHALL be accepted (v5.0.0-6.2.9), there SHALL be no composition rules (v5.0.0-6.2.5), and paste and password managers SHALL be allowed (v5.0.0-6.2.7). Passwords found in a bundled list of common passwords or in the Have I Been Pwned range API SHALL be rejected (v5.0.0-6.2.12). The range API is queried with a hash prefix only.

#### Scenario: Password too short
- **WHEN** the admin chooses a 14-character password during setup
- **THEN** setup rejects it and states the 15-character minimum

#### Scenario: Long password accepted
- **WHEN** the admin chooses a 64-character password made only of lowercase letters that is not in any breach list
- **THEN** setup accepts it

#### Scenario: Breached password
- **WHEN** the admin chooses a password that appears in the common-password list or the breach API
- **THEN** setup rejects it and says the password is known from breaches

#### Scenario: Only a hash prefix leaves the server
- **WHEN** Tero checks a password against the breach API
- **THEN** the outbound request contains only the first 5 hex characters of the password's SHA-1 hash

#### Scenario: Paste allowed
- **WHEN** the admin pastes a password into the setup or login form
- **THEN** the form accepts the pasted value

### Requirement: Password storage
The admin password SHALL be stored only as an Argon2id hash (RFC 9106) and SHALL never be stored or logged in plaintext.

#### Scenario: Stored form
- **WHEN** the admin completes setup
- **THEN** Tero's database holds an Argon2id hash for the password and no plaintext or reversibly encrypted copy

### Requirement: TOTP codes are single use
A TOTP code SHALL be accepted at most once (v5.0.0-6.5.1).

#### Scenario: Replayed code
- **WHEN** a TOTP code was just used to log in and the same code is submitted again within its validity window
- **THEN** the second login fails

### Requirement: Brute-force protection
Login attempts SHALL be rate limited per source IP and per account, with increasing delays after repeated failures (v5.0.0-6.3.1).

#### Scenario: Repeated failures from one IP
- **WHEN** one IP submits 5 failed logins in a row
- **THEN** further attempts from that IP are refused for a delay that grows with each additional failure

#### Scenario: Distributed guessing
- **WHEN** failed logins for the admin account come from many IPs
- **THEN** the per-account limit applies and slows all attempts on that account

#### Scenario: Limit does not leak validity
- **WHEN** a rate-limited client submits correct credentials
- **THEN** the response does not reveal that the credentials were correct

### Requirement: Session management
Dashboard sessions SHALL follow OWASP ASVS 5.0.0 V7. The session cookie SHALL use the `__Host-` prefix with `Secure`, `HttpOnly` and `SameSite=Strict`. Sessions SHALL expire after 30 minutes of inactivity and 12 hours after login, and all sessions SHALL be revoked when the password or TOTP changes.

#### Scenario: Cookie attributes
- **WHEN** the admin logs in
- **THEN** the session cookie name starts with `__Host-`, has `Secure`, `HttpOnly`, `SameSite=Strict` and `Path=/`, and has no `Domain` attribute

#### Scenario: Idle timeout
- **WHEN** a session has made no request for 30 minutes
- **THEN** the next request is sent to the login page

#### Scenario: Absolute timeout
- **WHEN** 12 hours have passed since login, even with continuous activity
- **THEN** the session ends and the admin must log in again

#### Scenario: Credential change revokes sessions
- **WHEN** the admin's password or TOTP is reset
- **THEN** every existing session stops working

### Requirement: Security events
Tero SHALL record security events and show the most recent on the dashboard home, and SHALL also write them to the system journal (v5.0.0-6.3.5, v5.0.0-6.3.7). Every login path SHALL be documented and SHALL produce an event (v5.0.0-6.3.4).

#### Scenario: Events recorded
- **WHEN** any of these happen: setup completed, successful login, failed login, rate limit triggered, `sudo tero reset-login` used
- **THEN** an event with the time, type and source IP (when there is one) appears in the dashboard's security events list and in the journal

#### Scenario: Recovery visible after the fact
- **WHEN** the admin logs in after `sudo tero reset-login` was used
- **THEN** the dashboard home shows the reset event

#### Scenario: No secrets in events
- **WHEN** any security event is recorded
- **THEN** it contains no password, TOTP code, TOTP seed or session token
