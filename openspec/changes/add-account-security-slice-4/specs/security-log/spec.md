## MODIFIED Requirements

### Requirement: Events recorded by the key service
Security events SHALL be stored only in the key service's database. The key service SHALL record login, setup, throttle and account events itself. A caller MAY append only the event types allowed for it, with an optional source IP and no free text; the key service SHALL add the time and the caller (ASVS v5.0.0-16.2.1).

#### Scenario: Login events come from tero-keys
- **WHEN** the admin logs in
- **THEN** the `login_succeeded` event's source is `tero-keys`, not the dashboard

#### Scenario: Caller can't choose who or when
- **WHEN** the dashboard appends a `breach_check_skipped` event
- **THEN** the stored event's time comes from `tero-keys`' clock and its source is `dashboard`, whatever the request contains

#### Scenario: Event type not allowed for the caller
- **WHEN** the dashboard appends a `login_succeeded`, `rate_limited` or unknown event
- **THEN** the request is refused and nothing is stored

### Requirement: Events when the key service is down
When the dashboard can't append an event, it SHALL write the event to its own journal output marked as not stored and carry on.

#### Scenario: Append fails
- **WHEN** the online breach check can't run during setup while `tero-keys` is stopped
- **THEN** the journal has a `breach_check_skipped` entry from `tero.service` with `TERO_EVENT_UNSTORED=1`

### Requirement: Event limits and retention
The key service SHALL keep at most 60 appended events per caller per minute, replacing the rest with one `events_dropped` event with a count per minute; SHALL count refused connections the same way in `callers_refused`; SHALL keep at most 100,000 events, pruning the oldest but never `login_succeeded`, `passkey_login_succeeded`, `setup_completed`, `login_reset`, `key_rotated`, `breach_reset`, `totp_locked`, `password_changed`, `passkey_added` or `passkey_removed`; and SHALL delete events older than one year.

#### Scenario: Flood from the dashboard
- **WHEN** the dashboard appends 10,000 events in one minute
- **THEN** at most 60 are stored, plus one `events_dropped` event with the count, and earlier events are still listed

#### Scenario: Flood doesn't hide a reset
- **WHEN** more than 100,000 other events follow a `breach_reset` or `passkey_added` event
- **THEN** that event is still listed after a prune

#### Scenario: Old event
- **WHEN** an event is older than one year at a prune
- **THEN** it is deleted

## ADDED Requirements

### Requirement: Account events
The key service SHALL record `password_changed`, `account_confirmed`, `confirm_failed`, `sessions_ended` with the number of sessions ended, `passkey_added`, `passkey_removed`, `passkey_login_succeeded`, `passkey_login_failed` and `passkey_counter_mismatch`. A wrong code with the right password on confirmation or password change SHALL be recorded as `confirm_totp_failed` and listed to the dashboard as `confirm_failed`, as a wrong login code is listed as `login_failed`. Events SHALL never hold a password, code, token, passkey or its public key.

#### Scenario: Password change recorded
- **WHEN** the admin changes the password and ends 2 other sessions
- **THEN** a `password_changed` event and a `sessions_ended` event with count 2 are recorded, both with source `tero-keys`

#### Scenario: Passkey login recorded
- **WHEN** the admin logs in with a passkey
- **THEN** a `passkey_login_succeeded` event is recorded with source `tero-keys`

#### Scenario: Dashboard can't append account events
- **WHEN** the dashboard appends a `password_changed` or `passkey_added` event
- **THEN** the request is refused and nothing is stored
