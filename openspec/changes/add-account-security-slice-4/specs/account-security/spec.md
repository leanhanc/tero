## ADDED Requirements

### Requirement: Settings page
The dashboard SHALL have a settings page, reachable from every signed-in page, with the admin's password, passkeys and sessions. Every rule on it SHALL be enforced by the key service, not the browser.

#### Scenario: Open settings
- **WHEN** the logged-in admin follows the settings link
- **THEN** the page shows a password change form, the list of passkeys and the list of active sessions

#### Scenario: No session
- **WHEN** a request to any settings call arrives without a live session
- **THEN** it is refused with `401` and nothing is read or changed

### Requirement: Password change
The admin SHALL be able to change their password by giving the current password, a current TOTP code and a new password. The new password SHALL pass the same length, blocklist, breach and context-word checks as at setup. The form SHALL offer to end every other session, checked by default. Passkeys and the TOTP secret SHALL be kept (ASVS v5.0.0-6.2.2, v5.0.0-6.2.3, v5.0.0-7.4.3).

#### Scenario: Password changed
- **WHEN** the admin submits the right current password, a current code and an acceptable new password
- **THEN** the next login works with the new password and the same authenticator, and fails with the old password

#### Scenario: Other sessions ended
- **WHEN** the admin changes the password with "End every other session" checked while logged in from two browsers
- **THEN** the other browser's session ends and the current one keeps working

#### Scenario: Other sessions kept
- **WHEN** the admin unchecks "End every other session" and changes the password
- **THEN** both browsers stay logged in

#### Scenario: Wrong current password or code
- **WHEN** the current password or the code is wrong
- **THEN** the password is unchanged, the error doesn't say which was wrong, and the attempt counts toward the login throttle

#### Scenario: Code reused
- **WHEN** the code just used to log in or confirm is sent with a password change
- **THEN** the change is refused

### Requirement: Context words refused
A new password, at setup or on change, SHALL be refused when it contains, ignoring case, "tero", "admin", the dashboard domain, a label of the dashboard domain of at least 4 characters that isn't all digits, or the server's public IP (ASVS v5.0.0-6.1.2, v5.0.0-6.2.11).

#### Scenario: Domain label in the password
- **WHEN** the dashboard domain is `deploy.example.com` and the new password is `MyDeployBox2026`
- **THEN** it is refused with a message naming "deploy"

#### Scenario: Service name in the password
- **WHEN** the new password is `my-tero-password`
- **THEN** it is refused

### Requirement: Confirming it's you
Ending sessions and adding, renaming or removing a passkey SHALL need the session to have been confirmed with the password and a current TOTP code in the last 5 minutes. A login or setup with the password and a code SHALL confirm the session it starts; a passkey login SHALL NOT. The dashboard SHALL ask for the password and a code only when the admin starts one of those actions on an unconfirmed session, and then carry the action out. A wrong confirmation SHALL count toward the login throttle and the code guessing limit, and a code SHALL confirm only once (ASVS v5.0.0-7.5.1).

#### Scenario: Not confirmed
- **WHEN** the admin, logged in for more than 5 minutes without confirming, asks to end a session
- **THEN** the dashboard asks for the password and a code, the session is not ended until they pass, and then it is ended without asking again

#### Scenario: Login confirms
- **WHEN** the admin logs in with the password and a code and adds a passkey within 5 minutes
- **THEN** no further password or code is asked for

#### Scenario: Passkey login doesn't confirm
- **WHEN** the admin logs in with a passkey and asks to add another passkey
- **THEN** the key service refuses until the session is confirmed with the password and a code

#### Scenario: Confirmation expires
- **WHEN** 5 minutes have passed since the session was confirmed
- **THEN** a request to remove a passkey is refused until the admin confirms again

#### Scenario: Confirmation belongs to one session
- **WHEN** the admin confirms in one browser
- **THEN** another browser's session is still unconfirmed

#### Scenario: Guessing through confirmation
- **WHEN** someone holding a session sends wrong passwords to the confirmation step
- **THEN** they are locked out after the same number of failures as the login page

### Requirement: Active sessions
The admin SHALL see every active session with when it started, when it was last used, its IP at login and latest IP, its browser and how it logged in, with the current one marked. After confirming, the admin SHALL be able to end any session, or every session but the current one (ASVS v5.0.0-7.4.5, v5.0.0-7.5.2).

#### Scenario: List
- **WHEN** the admin is logged in from two browsers and opens settings in one
- **THEN** both sessions are listed, the current one is marked, and each shows its browser and IP

#### Scenario: End one session
- **WHEN** the confirmed admin ends the other browser's session
- **THEN** that browser's next request gets `401` and shows the login form

#### Scenario: End all other sessions
- **WHEN** the confirmed admin ends all other sessions
- **THEN** every session but the current one stops working

#### Scenario: No session tokens exposed
- **WHEN** the session list is returned
- **THEN** it holds session ids, never a session token or its hash

### Requirement: Passkeys
After confirming, the admin SHALL be able to add up to 10 passkeys, each with a name of 1 to 64 characters, rename and remove them, and see when each was added and last used. Adding or removing one SHALL offer to end every other session, checked by default. Passkeys SHALL be discoverable credentials with user verification required, bound to the dashboard domain, and verified by the key service (ASVS v5.0.0-7.4.3).

#### Scenario: Add a passkey
- **WHEN** the confirmed admin adds a passkey named "Laptop" and approves it on their authenticator
- **THEN** "Laptop" is listed with today's date, and the admin can log in with it

#### Scenario: Add without confirming
- **WHEN** an unconfirmed session asks to add a passkey
- **THEN** it is refused and no challenge is issued

#### Scenario: Rename and remove
- **WHEN** the confirmed admin renames "Laptop" to "Work laptop" and then removes it
- **THEN** the list shows the new name, then no longer shows it, and that passkey no longer logs in

#### Scenario: Limit reached
- **WHEN** 10 passkeys exist and the admin tries to add another
- **THEN** it is refused with a message to remove one first

#### Scenario: Passkey for another site
- **WHEN** a registration or login answer was made for a relying party other than the dashboard domain
- **THEN** the key service refuses it

#### Scenario: Offered after login
- **WHEN** the admin logs in with the password and a code, has no passkey, and hasn't chosen "Not now" in this browser
- **THEN** the dashboard offers to add a passkey before the page the admin was going to, and "Not now" skips it in this browser from then on

#### Scenario: Last passkey removed
- **WHEN** the admin removes their only passkey
- **THEN** login with password and TOTP still works
