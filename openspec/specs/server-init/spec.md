# server-init Specification

## Purpose
`sudo tero init` turns a fresh server on a tested platform (initially Ubuntu 26.04 LTS, see `installation`) into a Tero host in one run: it hardens the host, installs Podman, creates the unprivileged `tero` system user, installs the Tero service, and hands the admin a way into the dashboard. It is the only step that needs root for setup.

## Requirements

### Requirement: Init runs once as root
`tero init` SHALL require root and SHALL run only once per server. When it refuses to run, it SHALL say what state the server is in and what the user can do instead.

#### Scenario: Init without root
- **WHEN** a user runs `tero init` without root privileges
- **THEN** it exits with a non-zero status and tells the user to run `sudo tero init`

#### Scenario: Init on an already initialized server
- **WHEN** `sudo tero init` runs on a server where init already completed
- **THEN** it exits with a non-zero status without changing the system
- **AND** the message says the server is already a Tero host, shows the dashboard URL, and points to `sudo tero reset-login` for anyone who lost dashboard access

### Requirement: Host hardening
Init SHALL harden the host to a documented subset of the CIS Benchmark Level 1 Server profile for the host's distro before Tero starts serving traffic. The subset, its CIS IDs per distro, and every skipped control with its reason are listed in `security/cis-subset.md`.

#### Scenario: Hardening applied
- **WHEN** init completes
- **THEN** every control in the CIS subset for the host's distro is in effect

#### Scenario: Lynis score in end-to-end tests
- **WHEN** the end-to-end suite runs Lynis on a freshly initialized server
- **THEN** the hardening index is at or above the minimum score set in the suite

### Requirement: Podman installation
Init SHALL install Podman from the OS's own package repositories so containers can run rootless under the `tero` user.

#### Scenario: Podman available to tero
- **WHEN** init completes
- **THEN** the `tero` user can run `podman info` successfully in rootless mode

### Requirement: Tero system user
Init SHALL create a system user `tero` with no login shell, lingering enabled, and a subordinate uid/gid range of 1,000,000 ids.

#### Scenario: Subordinate id range sized for rootless BuildKit
- **WHEN** init completes
- **THEN** `/etc/subuid` and `/etc/subgid` each contain one entry for `tero` with a count of 1000000
- **AND** the range does not overlap any other user's range

#### Scenario: Linger enabled
- **WHEN** init completes
- **THEN** `loginctl show-user tero` reports `Linger=yes` and `/run/user/<tero uid>` exists

#### Scenario: tero cannot log in interactively
- **WHEN** init completes
- **THEN** the `tero` user has no password and a non-login shell

### Requirement: Tero service
Init SHALL install and start a systemd unit that runs the Tero server as the `tero` user and restarts it on failure and on boot.

#### Scenario: Service running after init
- **WHEN** init completes
- **THEN** the Tero systemd unit is enabled and active, and its main process runs as `tero`

#### Scenario: Service survives reboot
- **WHEN** the server reboots after init
- **THEN** the Tero service and all running apps come back without manual steps

### Requirement: Dashboard domain at init
Init SHALL ask for a dashboard domain, and SHALL fall back to `<public-ip>.sslip.io` when none is given.

#### Scenario: Admin provides a domain
- **WHEN** the admin enters a domain at the init prompt
- **THEN** the dashboard is served over HTTPS on that domain

#### Scenario: Admin skips the domain
- **WHEN** the admin leaves the domain prompt empty
- **THEN** the dashboard is served over HTTPS on `<public-ip>.sslip.io`

### Requirement: First-login link
Init SHALL finish by printing a one-time link the admin uses to set up their login. See `dashboard-access`.

#### Scenario: Link printed at the end of init
- **WHEN** init completes
- **THEN** the last output is an HTTPS one-time setup link on the dashboard domain
