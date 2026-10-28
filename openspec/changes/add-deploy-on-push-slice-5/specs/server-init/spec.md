## MODIFIED Requirements

### Requirement: Tero agent
Init SHALL create a system user `tero-agent` with no login shell, home `/var/lib/tero-agent` (mode 0700; Podman 4.9 adds search permission for others, 0711), lingering enabled and a subordinate uid/gid range of 1,000,000 ids, and SHALL install a systemd user unit for the Tero agent in the system-wide user unit directory, limited to `tero-agent`, and enable it so it runs under `tero-agent`'s user manager and starts on boot.

#### Scenario: Agent running after init
- **WHEN** init completes
- **THEN** the agent unit is active in `tero-agent`'s user manager and its process runs as `tero-agent`

#### Scenario: Subordinate id range sized for rootless BuildKit
- **WHEN** init completes
- **THEN** `/etc/subuid` and `/etc/subgid` each contain one entry for `tero-agent` with a count of 1000000
- **AND** the range does not overlap any other user's range

#### Scenario: Agent survives reboot
- **WHEN** the server reboots after init
- **THEN** the agent is running again without manual steps

#### Scenario: tero-agent cannot log in interactively
- **WHEN** init completes
- **THEN** the `tero-agent` user has no password and a non-login shell

## ADDED Requirements

### Requirement: Tero service user
Init SHALL create a system user `tero` with no login shell, no subordinate id range and no lingering, which runs the Tero service.

#### Scenario: tero cannot log in interactively
- **WHEN** init completes
- **THEN** the `tero` user has no password and a non-login shell

#### Scenario: tero has no containers
- **WHEN** init completes
- **THEN** `/etc/subuid` and `/etc/subgid` have no entry for `tero`, and `loginctl show-user tero` doesn't report `Linger=yes`

### Requirement: State shared by the service and the agent
Init SHALL create `/var/lib/tero-shared`, owned by root with group `tero-shared` and mode 2770, holding the database and build logs, with `tero` and `tero-agent` the only members of `tero-shared`. Init SHALL give the key service's socket the group `tero-keys-callers`, whose only members are `tero` and `tero-agent`.

#### Scenario: Shared directory
- **WHEN** init completes and a deploy has run
- **THEN** `/var/lib/tero-shared` has mode 2770 and group `tero-shared`, and the database, its WAL and shared-memory files and the build log all have group `tero-shared` and no permission for others

#### Scenario: Other users kept out
- **WHEN** a user who is neither `tero` nor `tero-agent` (and not root) tries to read the database or connect to the key service's socket
- **THEN** both are refused

## REMOVED Requirements

### Requirement: Tero system user
**Reason**: The subordinate id range and lingering move to `tero-agent`, so `tero` only runs the service.
**Migration**: See "Tero service user" and "Tero agent". There are no installs to migrate.
