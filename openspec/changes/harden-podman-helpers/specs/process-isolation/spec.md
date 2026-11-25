## MODIFIED Requirements

### Requirement: Containers run outside the service
App and build containers SHALL be started by the Tero agent, a process running as the `tero-agent` system user under that user's systemd user manager, through a Podman service that runs as the `tero-podman` system user under that user's systemd user manager. `tero-podman` SHALL own Podman's storage, every image and container, and the network helpers Podman starts. The Tero service SHALL NOT start containers or run Podman, the agent SHALL NOT listen on any network socket, and no container process SHALL run as `tero-podman`'s or `tero-agent`'s own uid.

#### Scenario: Service sandbox unchanged
- **WHEN** a deploy has run
- **THEN** the Tero service still has `NoNewPrivileges` and a bounding set of only `CAP_NET_BIND_SERVICE`, and no `podman` or `conmon` process descends from it

#### Scenario: Agent has no listener
- **WHEN** the agent is running
- **THEN** it has no listening TCP, UDP or Unix socket

#### Scenario: Service can't change the agent's setup
- **WHEN** a process running as `tero` tries to read or write `/var/lib/tero-agent`, `/var/lib/tero-podman`, Podman's storage or configuration, the shared directories in `/run`, or either user's systemd user unit directory
- **THEN** it is refused

#### Scenario: Agent unit not writable by tero or tero-agent
- **WHEN** init completes
- **THEN** the agent's unit file is owned by root and not writable by `tero` or `tero-agent`

#### Scenario: Containers never run as Podman's own uid
- **WHEN** an app and a build are running
- **THEN** every process in their containers has a host uid inside `tero-podman`'s subordinate range, never `tero-podman`'s or `tero-agent`'s own uid

#### Scenario: tero can't run containers
- **WHEN** a process running as `tero` runs `podman run`
- **THEN** it fails, since `tero` has no subordinate id range and can't reach the Podman service

### Requirement: App container hardening
Every app container SHALL run with all capabilities dropped, `no-new-privileges`, its own user namespace from `tero-podman`'s subordinate range (`--userns=auto`), and a network namespace of its own with no route to the host's loopback, with its port published on `127.0.0.1` only.

#### Scenario: Container options
- **WHEN** an app's container is running
- **THEN** `podman inspect` shows no added capabilities, `no-new-privileges`, an automatic user namespace, a network mode of its own (pasta or slirp4netns), and its port bound only to `127.0.0.1`

#### Scenario: Apps run as different host uids
- **WHEN** two apps are running
- **THEN** their processes have different host uids, both inside `tero-podman`'s subordinate range

#### Scenario: Apps can't reach each other directly
- **WHEN** one app's container tries to connect to another app's published port through the host's loopback, the container gateway or the host's address
- **THEN** the connection fails

#### Scenario: App port not exposed
- **WHEN** an app is running
- **THEN** its published port refuses connections from outside the server, and the app is reachable only through its HTTPS address

### Requirement: Values never on disk or on a command line
An app's variables SHALL NOT be written to a disk or passed on a command line, and SHALL NOT appear in `podman inspect`. They SHALL reach the container from memory, through a file in `/run/tero-env` that only `tero-agent` can write and only `tero-agent`, `tero-podman` and that container can read, which SHALL be removed with the container. The app's process SHALL see them as ordinary environment variables and SHALL remain its container's first process.

#### Scenario: Disk search
- **WHEN** an app with variables is running
- **THEN** a search of `/var/lib/tero-agent`, `/var/lib/tero-podman`, `/var/lib/tero-shared` and `/var/lib/tero` for the values finds nothing

#### Scenario: Not visible to other processes
- **WHEN** an app with variables is starting and running
- **THEN** no host process's command line holds a value, `podman inspect` shows none, and a process running as `tero` can't read the file

#### Scenario: Other apps can't read it
- **WHEN** another app's container looks for the file
- **THEN** it isn't there

#### Scenario: Image's own command kept
- **WHEN** an app with variables starts
- **THEN** PID 1 in its container is the image's own entrypoint and command, and signals reach it

#### Scenario: File removed
- **WHEN** a release is replaced, or the agent starts and finds a file with no container
- **THEN** the file is gone

## ADDED Requirements

### Requirement: Podman apart from the agent
Podman's network helpers (pasta or slirp4netns, and rootlessport), conmon and the Podman service SHALL run as `tero-podman`. Only the agent, the build planner it confines and `tero-agent`'s user manager SHALL run as `tero-agent`. The Podman service's socket SHALL be reachable only by `tero-podman` and `tero-agent`.

#### Scenario: Network helpers apart from the agent
- **WHEN** an app with a published port is running
- **THEN** every conmon, pasta, slirp4netns and rootlessport process runs as `tero-podman`, and no process other than the agent, the planner and the user manager runs as `tero-agent`

#### Scenario: Podman service only for the agent
- **WHEN** a process running as `tero`, or as any other user that is neither `tero-agent`, `tero-podman` nor root, connects to the Podman service's socket
- **THEN** it is refused
