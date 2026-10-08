# process-isolation Specification

## Purpose
Keep host root out of Tero's runtime. Root is used once at init and afterwards only when an admin types `sudo tero ...` on the server; the long-running service and every container it starts run as the unprivileged `tero` user.

## Requirements

### Requirement: Service runs unprivileged
The Tero service SHALL run as the `tero` user with `CAP_NET_BIND_SERVICE` as its only capability.

#### Scenario: Service process identity
- **WHEN** the Tero service is running
- **THEN** its main process uid is `tero` and its effective, permitted and bounding capability sets contain only `CAP_NET_BIND_SERVICE`

#### Scenario: Built-in proxy binds privileged ports
- **WHEN** the Tero service starts
- **THEN** its built-in Caddy listens on ports 80 and 443 without running as root

### Requirement: Rootless containers only
Every container Tero starts SHALL run under rootless Podman as the `tero` user.

#### Scenario: No container process owned by host root
- **WHEN** Tero has started a container
- **THEN** every container process, `conmon`, and network helper has a non-zero host uid belonging to `tero` or its subordinate range

#### Scenario: No privileged containers
- **WHEN** Tero starts any container
- **THEN** the container is not started with `--privileged`

### Requirement: Root actions only through the local CLI
Actions that need root SHALL be available only as `sudo tero ...` commands on the server, never from the dashboard or any network-facing interface.

#### Scenario: Dashboard cannot trigger root work
- **WHEN** an authenticated admin uses every action the dashboard offers
- **THEN** none of them executes a process as host root

#### Scenario: Root command run without sudo
- **WHEN** a user runs a root-only `tero` subcommand without root
- **THEN** it exits with a non-zero status and tells the user to use `sudo`
