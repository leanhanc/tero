# installation Specification

## Purpose
Get the Tero binary onto a fresh server with one command, and refuse to install on platforms Tero has not been tested on yet. Configuring the server is a separate step (`sudo tero init`, see `server-init`).

## Requirements

### Requirement: Tested platforms
The install script SHALL install only on platforms in Tero's tested list. The initial tested list is Ubuntu 26.04 LTS on amd64 and arm64; more platforms are added to the list as they get tested.

#### Scenario: Install on supported amd64 host
- **WHEN** the install script runs on Ubuntu 26.04 LTS with `uname -m` reporting `x86_64`
- **THEN** it installs the amd64 release binary

#### Scenario: Install on supported arm64 host
- **WHEN** the install script runs on Ubuntu 26.04 LTS with `uname -m` reporting `aarch64`
- **THEN** it installs the arm64 release binary

#### Scenario: Untested OS
- **WHEN** the install script runs on an OS or OS version not in the tested list
- **THEN** it exits with a non-zero status and a message listing the tested platforms
- **AND** it does not write any files to the system

#### Scenario: Untested architecture
- **WHEN** the install script runs on a tested OS with an architecture other than `x86_64` or `aarch64`
- **THEN** it exits with a non-zero status and a message listing the tested platforms
- **AND** it does not write any files to the system

### Requirement: Install via curl script
Tero SHALL be installable with a single `curl ... | sh` command that downloads the release binary matching the host architecture and places it on the PATH.

#### Scenario: Fresh install
- **WHEN** a user runs the install command on a supported host
- **THEN** the `tero` binary is installed on the PATH and is executable
- **AND** the script tells the user to run `sudo tero init` next

#### Scenario: Install script does not configure the server
- **WHEN** the install script finishes
- **THEN** no system user, service, package, or firewall rule has been created or changed

### Requirement: Release binary integrity
The install script SHALL verify the downloaded binary against the checksum published with the release before installing it.

#### Scenario: Checksum mismatch
- **WHEN** the downloaded binary does not match the published checksum
- **THEN** the script exits with a non-zero status and does not install the binary
- **AND** the message says the download may be corrupted or tampered with and tells the user to retry or verify the release by hand
