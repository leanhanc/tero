## ADDED Requirements

### Requirement: Key held outside the tero user
The master key, the admin's credentials and the security log SHALL be readable only by `tero-keys` and root. The dashboard SHALL reach them only through the key service's requests, and no other process running as `tero` SHALL reach them at all.

#### Scenario: Dashboard can't read the key service
- **WHEN** code running inside `tero.service` tries to read `tero-keys`' `/proc/<pid>/mem`, environ or credential directory
- **THEN** every attempt fails

#### Scenario: Dashboard can't leave its cgroup
- **WHEN** code running inside `tero.service` tries to move a process into another cgroup, or to reach `tero`'s user manager bus to start a unit
- **THEN** both fail

#### Scenario: Other tero processes refused
- **WHEN** a process running as `tero` in `tero`'s user manager, including one placed in `tero-agent.service`'s cgroup, connects to `tero-keys`
- **THEN** it is refused
