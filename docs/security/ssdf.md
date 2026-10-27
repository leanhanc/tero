# NIST SSDF for the Tero project

How Tero is built, released and maintained follows the [NIST Secure Software Development Framework](https://csrc.nist.gov/pubs/sp/800/218/final) (SSDF, SP 800-218 version 1.1). The SSDF is a list of practices, not controls, and it is written to point at other standards. This file maps each SSDF task to where Tero meets it, usually through the standards it already uses for each layer:

| Layer | Standard | Document |
|---|---|---|
| Dashboard and API | OWASP ASVS 5.0.0 Level 2, with NIST SP 800-63B-4 for authentication | [asvs-l2.md](asvs-l2.md) |
| Host | CIS Benchmark Level 1 Server, as a chosen subset, with a Lynis minimum | [cis-subset.md](cis-subset.md) |
| Builds and app containers | NIST SP 800-190 | [sp800-190.md](sp800-190.md) |
| Releases and repository | SLSA Build Level 3, OpenSSF OSPS Baseline | [Verify a release](../../README.md#verify-a-release), [SECURITY.md](../../SECURITY.md) |

ADR 0001 explains why each standard covers its layer.

## Status values

| Status | Meaning |
|---|---|
| Met | Tero does what the task asks today. |
| Partly | Part of the task is done; the row says what is missing. |
| Not yet | The task applies and nothing does it yet. |
| N/A | The task doesn't fit a project with one maintainer, for the reason given. |

## Summary

Tasks not met yet, in the order they matter most:

| Task | What is missing |
|---|---|
| PW.1.1 | No written threat model. ADR 0001 lists the attack surfaces and each slice's design lists its risks, but nothing ties them into one model. |
| RV.1.3, RV.2.1 | `SECURITY.md` says how to report, but not how fast reports are answered and fixed, or how they are triaged. |
| PW.4.4 | Dependabot opens update pull requests, but nothing fails a build on a known vulnerability. `govulncheck` and `bun audit` in CI are on the roadmap (slice 1). |
| PO.4.2, PW.8.2 | Unit and end-to-end tests run on the maintainer's machines and on EC2 by hand. No CI runs them. |
| RV.3 | No record of root-cause analysis for fixed vulnerabilities. |

## Prepare the Organization (PO)

| Task | SSDF asks | Status | Where Tero meets it |
|---|---|---|---|
| PO.1.1 | Security requirements for the development infrastructure and process | Met | ADR 0001 sections 4 and 5; this file; [SECURITY.md](../../SECURITY.md) |
| PO.1.2 | Security requirements for the software | Met | [asvs-l2.md](asvs-l2.md), [cis-subset.md](cis-subset.md), [sp800-190.md](sp800-190.md). Specs in [openspec/specs/](../../openspec/specs/) cite requirement IDs, and each scenario has a test |
| PO.1.3 | Requirements communicated to third parties who supply software | N/A | Tero has no suppliers. Dependencies are open-source modules chosen by the maintainer and checked under PW.4 |
| PO.2.1–PO.2.3 | Roles, training and management commitment | N/A | One maintainer holds every role |
| PO.3.1 | Specify the toolchain and its security tools | Met | Go and Bun versions pinned in `go.mod` and the workflows; GoReleaser, Syft, CodeQL and OpenSSF Scorecard in [.github/workflows/](../../.github/workflows/) |
| PO.3.2 | Follow recommended security practices for the toolchain | Met | Every GitHub Action is pinned by commit SHA and updated by Dependabot; workflows default to `permissions: {}` and grant each job only what it needs; checkouts use `persist-credentials: false`; releases build without dependency caches |
| PO.3.3 | Tools produce evidence of their work | Met | SBOMs, `checksums.txt` and signed provenance on every release; CodeQL and Scorecard results in the repository's code scanning alerts |
| PO.4.1 | Criteria for security checks | Met | Each requirement is a spec scenario with an end-to-end test on a fresh server, plus a minimum Lynis score; the status columns of the three mapping documents |
| PO.4.2 | Gather the information the criteria need | Partly | `make test` and `make e2e` (Lima VMs, or `scripts/ec2-test.sh` on a cloud server) produce it, run by hand. Cloud CI is on the roadmap (slice 1) |
| PO.5.1 | Separate and protect the development environments | Partly | Releases build only in GitHub Actions, in a reusable workflow isolated from its caller. End-to-end tests run in throwaway VMs and servers. A GitHub ruleset blocks deleting or force-pushing `main` |
| PO.5.2 | Secure the developers' endpoints | Not yet | Not documented |

## Protect the Software (PS)

| Task | SSDF asks | Status | Where Tero meets it |
|---|---|---|---|
| PS.1.1 | Protect all code from unauthorized access and tampering | Met | Public GitHub repository; only the maintainer can push; a ruleset blocks deleting or force-pushing `main`; no secrets in the repository |
| PS.2.1 | A way to verify release integrity | Met | `checksums.txt` (SHA-256) for every file, checked by `install.sh`; SLSA Build Level 3 provenance signed through Sigstore, checked with `gh attestation verify` ([Verify a release](../../README.md#verify-a-release)) |
| PS.3.1 | Archive each release | Met | GitHub releases keep each binary, its SBOM, `install.sh`, `checksums.txt` and `tero.intoto.jsonl`. Builds use `-trimpath` and the commit's timestamp, so rebuilding a tag gives the same bytes |
| PS.3.2 | Provenance data for each release's components | Met | An SPDX SBOM per binary, made by Syft through GoReleaser |

## Produce Well-Secured Software (PW)

| Task | SSDF asks | Status | Where Tero meets it |
|---|---|---|---|
| PW.1.1 | Risk modeling | Partly | ADR 0001 names the four attack surfaces; each change's `design.md` in [openspec/changes/](../../openspec/changes/) has a risks section. No single threat model |
| PW.1.2 | Track design decisions and risk responses | Met | ADR 0001; `design.md` and `proposal.md` for each change |
| PW.1.3 | Use well-secured standard features instead of writing new ones | Met | Caddy for TLS and certificates, `systemd-creds` and the TPM for the master key, Argon2id from `golang.org/x/crypto`, `go-webauthn/webauthn` for passkeys, rootless Podman and BuildKit for builds |
| PW.2.1 | Review the design against the security requirements | Met | Each slice's design cites the requirements it meets, and gets a correctness and security review from a reviewer who didn't write it |
| PW.4.1 | Acquire well-secured third-party components | Met | Dependencies are widely used projects; the BuildKit image is pinned by digest |
| PW.4.2 | Create well-secured components in-house when needed | N/A | Tero builds no shared components for others |
| PW.4.4 | Check that components stay secure | Partly | Dependabot weekly for Go modules, the dashboard's packages and GitHub Actions. No vulnerability check fails a build yet (roadmap, slice 1) |
| PW.5.1 | Secure coding practices | Met | Commands run with argument lists and no shell, from system directories; every SQL query uses placeholders; request bodies decode into fixed structs with unknown fields refused ([asvs-l2.md](asvs-l2.md) rows 1.2.4, 1.2.5, 2.1.1) |
| PW.6.1 | Compiler and build tools that improve security | Met | Go toolchain version from `go.mod`; static binaries with `CGO_ENABLED=0` |
| PW.6.2 | Compiler and build settings that improve security | Met | `-trimpath`, stripped binaries, reproducible timestamps ([.goreleaser.yaml](../../.goreleaser.yaml)) |
| PW.7.1 | Decide which code gets reviewed or analyzed | Met | All of it: CodeQL covers the Go service and the dashboard's TypeScript |
| PW.7.2 | Review and analyze the code | Met | CodeQL on every push to `main`, every pull request and weekly; a review of each slice by someone who didn't write it |
| PW.8.1 | Decide which executable testing to do | Met | Every spec scenario has an end-to-end test on a fresh server of each tested Ubuntu release |
| PW.8.2 | Test the executable code | Partly | `make test` (Go and dashboard unit tests, `go vet`) and `make e2e`, with Lynis. They run by hand, not in CI. No fuzzing |
| PW.9.1 | Secure settings by default | Met | `sudo tero init` applies the host lockdown with no options to turn it off; the dashboard always needs two factors; apps always run in rootless containers |
| PW.9.2 | Document the secure defaults | Met | [cis-subset.md](cis-subset.md), [asvs-l2.md](asvs-l2.md), [sp800-190.md](sp800-190.md) |

## Respond to Vulnerabilities (RV)

| Task | SSDF asks | Status | Where Tero meets it |
|---|---|---|---|
| RV.1.1 | Gather information on vulnerabilities | Met | Private vulnerability reporting on GitHub ([SECURITY.md](../../SECURITY.md)), Dependabot alerts, CodeQL and Scorecard alerts |
| RV.1.2 | Review and test the code to find new vulnerabilities | Met | Weekly CodeQL run on the whole code base, so new queries check old code |
| RV.1.3 | A vulnerability disclosure policy | Partly | [SECURITY.md](../../SECURITY.md) says how to report and that advisories credit the reporter. It gives no response or fix timeline |
| RV.2.1 | Analyze each vulnerability | Not yet | No written triage steps |
| RV.2.2 | Plan and carry out a response | Partly | Only the latest release gets fixes, and each fix ships with a published advisory. On a server, `sudo tero assume-breach` is the admin's response to a suspected compromise |
| RV.3.1–RV.3.4 | Find root causes, look for the same problem elsewhere, improve the process | Not yet | No record kept |
