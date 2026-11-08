## ADDED Requirements

### Requirement: Values never on disk or on a command line
An app's variables SHALL NOT be written to a disk or passed on a command line, and SHALL NOT appear in `podman inspect`. They SHALL reach the container from memory, through a file only that container and `tero-agent` can reach, which SHALL be removed with the container. The app's process SHALL see them as ordinary environment variables and SHALL remain its container's first process.

#### Scenario: Disk search
- **WHEN** an app with variables is running
- **THEN** a search of `/var/lib/tero-agent`, `/var/lib/tero-shared` and `/var/lib/tero` for the values finds nothing

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
