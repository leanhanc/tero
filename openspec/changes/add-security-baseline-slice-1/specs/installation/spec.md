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
