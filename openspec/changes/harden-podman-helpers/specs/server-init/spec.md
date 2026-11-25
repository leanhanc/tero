## MODIFIED Requirements

### Requirement: Tero agent
Init SHALL create a system user `tero-agent` with no login shell, home `/var/lib/tero-agent` (mode 0700), lingering enabled and no subordinate id range, and SHALL install a systemd user unit for the Tero agent in the system-wide user unit directory, limited to `tero-agent`, and enable it so it runs under `tero-agent`'s user manager and starts on boot.

#### Scenario: Agent running after init
- **WHEN** init completes
- **THEN** the agent unit is active in `tero-agent`'s user manager and its process runs as `tero-agent`

#### Scenario: Agent survives reboot
- **WHEN** the server reboots after init
- **THEN** the agent is running again without manual steps

#### Scenario: tero-agent cannot log in interactively
- **WHEN** init completes
- **THEN** the `tero-agent` user has no password and a non-login shell

## ADDED Requirements

### Requirement: Podman user and service
Init SHALL create a system user `tero-podman` with no login shell, home `/var/lib/tero-podman` (mode 0700; Podman 4.9 adds search permission for others, 0711), lingering enabled and a subordinate uid/gid range of 1,000,000 ids, with `tero-agent` the only other member of its group. Init SHALL install a socket-activated Podman service as systemd user units limited to `tero-podman`, listening on `/run/tero-podman/podman.sock`, and SHALL have systemd create at every boot `/run/tero-podman` (0750, `tero-podman:tero-podman`), `/run/tero-env` (0750, `tero-agent:tero-podman`) and `/run/tero-output` (2770, `tero-podman:tero-podman`).

#### Scenario: Podman service available to the agent
- **WHEN** init completes
- **THEN** `podman info` run as `tero-agent` against the socket reports rootless Podman, and `/run/tero-podman` has mode 0750 with owner and group `tero-podman`, whose only member is `tero-agent`

#### Scenario: Subordinate id range sized for rootless BuildKit
- **WHEN** init completes
- **THEN** `/etc/subuid` and `/etc/subgid` each contain one entry for `tero-podman` with a count of 1000000, and none for `tero` or `tero-agent`
- **AND** the range does not overlap any other user's range

#### Scenario: tero-podman cannot log in interactively
- **WHEN** init completes
- **THEN** the `tero-podman` user has no password and a non-login shell

#### Scenario: Shared runtime directories
- **WHEN** init completes, and again after a reboot
- **THEN** `/run/tero-env` has mode 0750, owner `tero-agent` and group `tero-podman`, and `/run/tero-output` has mode 2770 with owner and group `tero-podman`
