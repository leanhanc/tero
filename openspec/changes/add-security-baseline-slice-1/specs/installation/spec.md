## MODIFIED Requirements

### Requirement: Tested platforms
The install script SHALL install only on platforms in Tero's tested list. The tested list is Ubuntu 24.04 LTS and Ubuntu 26.04 LTS, each on amd64 and arm64. A platform joins the list once the end-to-end suite passes on it.

#### Scenario: Install on supported amd64 host
- **WHEN** the install script runs on a tested OS release with `uname -m` reporting `x86_64`
- **THEN** it installs the amd64 release binary

#### Scenario: Install on supported arm64 host
- **WHEN** the install script runs on a tested OS release with `uname -m` reporting `aarch64`
- **THEN** it installs the arm64 release binary

#### Scenario: Untested OS
- **WHEN** the install script runs on an OS or OS version not in the tested list
- **THEN** it exits with a non-zero status and a message listing the tested platforms
- **AND** it does not write any files to the system

#### Scenario: Untested architecture
- **WHEN** the install script runs on a tested OS with an architecture other than `x86_64` or `aarch64`
- **THEN** it exits with a non-zero status and a message listing the tested platforms
- **AND** it does not write any files to the system

## ADDED Requirements

### Requirement: Release provenance
Every Tero release SHALL publish, for each binary, a SLSA v1.1 Build Level 3 provenance attestation produced by a reusable GitHub Actions workflow, and an SBOM. The install documentation SHALL show how to verify a downloaded binary with `gh attestation verify`.

#### Scenario: Verify a release binary
- **WHEN** a user downloads a release binary and runs `gh attestation verify` against the Tero repository
- **THEN** verification succeeds and names the reusable release workflow as the builder

#### Scenario: Tampered binary
- **WHEN** a user runs `gh attestation verify` on a release binary that was modified after release
- **THEN** verification fails

#### Scenario: SBOM published
- **WHEN** a release is published
- **THEN** an SBOM for each binary is attached to the release
