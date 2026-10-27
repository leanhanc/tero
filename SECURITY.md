# Security policy

## Reporting a vulnerability

Report vulnerabilities privately through GitHub: open the repository's **Security** tab and choose **Report a vulnerability** ([direct link](https://github.com/leanhanc/tero/security/advisories/new)). Please don't open a public issue.

Include what you found, how to reproduce it, and the Tero version (`tero version`). Once a fix is released, the advisory is published with credit to you, unless you'd rather stay anonymous.

## Supported versions

Only the latest release gets security fixes.

## Security baseline

Each layer of Tero is held to a published standard, and each mapping lists what is met, what isn't yet, and why:

| Layer | Standard | Mapping |
|---|---|---|
| Dashboard and API | [OWASP ASVS 5.0.0](https://github.com/OWASP/ASVS/tree/v5.0.0/5.0) Level 2, with [NIST SP 800-63B-4](https://pages.nist.gov/800-63-4/sp800-63b.html) for authentication and sessions | [docs/security/asvs-l2.md](docs/security/asvs-l2.md) |
| Host | [CIS Benchmark](https://www.cisecurity.org/cis-benchmarks) Level 1 Server, as a chosen subset, with a minimum Lynis score | [docs/security/cis-subset.md](docs/security/cis-subset.md) |
| Builds and app containers | [NIST SP 800-190](https://csrc.nist.gov/pubs/sp/800/190/final) | [docs/security/sp800-190.md](docs/security/sp800-190.md) |
| Releases | [SLSA](https://slsa.dev/spec/v1.1/) Build Level 3 and an SBOM for every binary | [Verify a release](README.md#verify-a-release) |
| Repository | [OpenSSF OSPS Baseline](https://baseline.openssf.org/), checked by OpenSSF Scorecard | — |
| Development and vulnerability response | [NIST SP 800-218](https://csrc.nist.gov/pubs/sp/800/218/final) (SSDF) | [docs/security/ssdf.md](docs/security/ssdf.md) |

## Verifying releases

Every release binary has SLSA Build Level 3 provenance and an SBOM. See [Verify a release](README.md#verify-a-release).
