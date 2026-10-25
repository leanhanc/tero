## ADDED Requirements

### Requirement: No session without the key service
No process running as `tero`, including the dashboard itself, SHALL be able to create a dashboard session or extend one. Sessions SHALL exist only in the key service's database.

#### Scenario: Session planted by a tero process
- **WHEN** a process running as `tero`, inside `tero.service` or in `tero`'s user manager, writes a made-up session into any file it can write and sends its token as the session cookie
- **THEN** the dashboard refuses the request with `401`

#### Scenario: Dashboard can't reach the sessions
- **WHEN** code running inside `tero.service` tries to open `/var/lib/tero-keys/keys.db`
- **THEN** it fails
