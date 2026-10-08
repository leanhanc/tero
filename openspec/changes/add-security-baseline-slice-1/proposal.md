## Why

Tero's pitch is security from the first minute, and ADR 0001 defines what that means in checkable terms: OWASP ASVS 5.0.0 Level 2 for the dashboard, a documented CIS Level 1 Server subset for the host, and SLSA Build L3 for releases. Slice 1 (install, `tero init`, the `tero` service user, first dashboard access) is where those standards first touch code, so its specs need to carry them before implementation starts.

## What Changes

- Install: every release carries SLSA Build L3 provenance and an SBOM, and the install docs show how to verify a download with `gh attestation verify`.
- Init: the host hardening requirement lists the CIS subset by topic (SSH, firewall, automatic updates, kernel settings, auditd, AppArmor, services, file permissions), and init refuses to turn off SSH password login when doing so would lock the admin out.
- Dashboard first access:
  - The password policy follows ASVS V6.2: at least 15 characters, at least 64 accepted, no composition rules, paste allowed, and breached passwords rejected.
  - The password is stored as an Argon2id hash.
  - TOTP codes work only once (6.5.1).
  - Logins are rate limited per IP and per account (6.3.1).
  - Session cookies use the `__Host-` prefix and `SameSite=Strict`, with idle and absolute timeouts (V7).
  - A security events list shows logins, failures and recovery, and every login path is recorded (6.3.4, 6.3.5, 6.3.7).
- `process-isolation`: no change. The ADR's container rules (`--cap-drop=ALL`, `--userns=auto`, per-app networks) apply once Tero starts app and build containers, so they come with the apps slice. Secrets requirements also wait for that slice.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `installation`: release provenance and SBOM; verification instructions.
- `server-init`: CIS subset by topic; SSH lockout guard.
- `dashboard-access`: ASVS L2 password policy, password storage, TOTP replay protection, brute-force protection, session management, security events.

## Impact

- Release pipeline: GoReleaser in a reusable GitHub Actions workflow with `actions/attest-build-provenance` and Syft SBOMs.
- Repo docs: `security/asvs-l2.md` (ASVS L2 matrix) and `security/cis-subset.md` (CIS controls per distro, including skipped ones).
- Init touches `sshd_config`, firewall, sysctl, auditd, AppArmor and unattended-upgrades on the host.
- Dashboard login: a bundled common-password list, outbound HTTPS to the Have I Been Pwned range API, and new SQLite tables for the TOTP last-used step, rate-limit state, sessions and security events.
- End-to-end CI runs Lynis on a freshly initialized server and enforces a minimum score.
