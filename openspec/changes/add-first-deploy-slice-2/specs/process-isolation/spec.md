## ADDED Requirements

### Requirement: Containers run outside the service
App and build containers SHALL be started by the Tero agent, a process running as `tero` under `tero`'s systemd user manager. The Tero service SHALL NOT start containers or run Podman, and the agent SHALL NOT listen on any network socket.

#### Scenario: Service sandbox unchanged
- **WHEN** a deploy has run
- **THEN** the Tero service still has `NoNewPrivileges` and a bounding set of only `CAP_NET_BIND_SERVICE`, and no `podman` or `conmon` process descends from it

#### Scenario: Agent has no listener
- **WHEN** the agent is running
- **THEN** it has no listening TCP, UDP or Unix socket

#### Scenario: Service can't change the agent's setup
- **WHEN** the Tero service tries to read or write `tero`'s Podman storage or configuration, or its systemd user unit directory
- **THEN** the paths are inaccessible to it

#### Scenario: Agent unit not writable by tero
- **WHEN** init completes
- **THEN** the agent's unit file is owned by root and not writable by `tero`

### Requirement: App container hardening
Every app container SHALL run with all capabilities dropped, `no-new-privileges`, its own user namespace from `tero`'s subordinate range (`--userns=auto`), and a network namespace of its own with no route to the host's loopback, with its port published on `127.0.0.1` only.

#### Scenario: Container options
- **WHEN** an app's container is running
- **THEN** `podman inspect` shows no added capabilities, `no-new-privileges`, an automatic user namespace, a network mode of its own (pasta or slirp4netns), and its port bound only to `127.0.0.1`

#### Scenario: Apps run as different host uids
- **WHEN** two apps are running
- **THEN** their processes have different host uids, both inside `tero`'s subordinate range

#### Scenario: Apps can't reach each other directly
- **WHEN** one app's container tries to connect to another app's published port through the host's loopback, the container gateway or the host's address
- **THEN** the connection fails

#### Scenario: App port not exposed
- **WHEN** an app is running
- **THEN** its published port refuses connections from outside the server, and the app is reachable only through its HTTPS address
