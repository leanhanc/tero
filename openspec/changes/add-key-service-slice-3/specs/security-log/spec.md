## ADDED Requirements

### Requirement: Events recorded by the key service
Security events SHALL be stored only in the key service's database. The key service SHALL record login and setup events itself. A caller MAY append only the event types allowed for it, with an optional source IP and no free text; the key service SHALL add the time and the caller (ASVS v5.0.0-16.2.1).

#### Scenario: Login events come from tero-keys
- **WHEN** the admin logs in
- **THEN** the `login_succeeded` event's source is `tero-keys`, not the dashboard

#### Scenario: Caller can't choose who or when
- **WHEN** the dashboard appends a `rate_limited` event
- **THEN** the stored event's time comes from `tero-keys`' clock and its source is `dashboard`, whatever the request contains

#### Scenario: Event type not allowed for the caller
- **WHEN** the dashboard appends a `login_succeeded` event or an unknown type
- **THEN** the request is refused and nothing is stored

### Requirement: Events can't be changed by tero
No request SHALL change or delete a recorded event, and no process running as `tero` SHALL be able to write the database that holds them (ASVS v5.0.0-16.4.2).

#### Scenario: Dashboard compromised
- **WHEN** code running inside `tero.service` tries to remove or edit past events through the socket or the filesystem
- **THEN** there is no request that does it and `/var/lib/tero-keys/` is not accessible to it

#### Scenario: Journal copy
- **WHEN** an event is recorded
- **THEN** the system journal has an entry with `_SYSTEMD_UNIT=tero-keys.service`, the event type in `TERO_EVENT` and no password, secret, code or token

### Requirement: Event limits and retention
The key service SHALL keep at most 60 appended events per caller per minute, replacing the rest with one `events_dropped` event with a count per minute; SHALL count refused connections the same way in `callers_refused`; SHALL keep at most 100,000 events, pruning the oldest but never `login_succeeded`, `setup_completed`, `login_reset`, `key_rotated`, `breach_reset` or `totp_locked`; and SHALL delete events older than one year.

#### Scenario: Flood from the dashboard
- **WHEN** the dashboard appends 10,000 events in one minute
- **THEN** at most 60 are stored, plus one `events_dropped` event with the count, and earlier events are still listed

#### Scenario: Flood doesn't hide a reset
- **WHEN** more than 100,000 other events follow a `breach_reset` event
- **THEN** the `breach_reset` event is still listed after a prune

#### Scenario: Old event
- **WHEN** an event is older than one year at a prune
- **THEN** it is deleted

### Requirement: Events when the key service is down
When the dashboard can't append an event, it SHALL write the event to its own journal output marked as not stored and carry on.

#### Scenario: Append fails
- **WHEN** the dashboard rate-limits a client while `tero-keys` is stopped
- **THEN** the journal has a `rate_limited` entry from `tero.service` with `TERO_EVENT_UNSTORED=1`
