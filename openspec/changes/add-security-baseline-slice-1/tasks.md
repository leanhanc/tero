## 1. Security docs

- [ ] 1.1 Write `security/cis-subset.md` with controls by topic, CIS IDs for Ubuntu (mapped to CIS 24.04 v2.0.0 until a 26.04 benchmark exists), and the skipped controls with reasons; verify every `server-init` hardening scenario points to a row
- [ ] 1.2 Write `security/asvs-l2.md` with one row per ASVS 5.0.0 L2 requirement, marked covered (spec and test) or not applicable (reason); verify every ASVS ID cited in `dashboard-access` has a row
- [ ] 1.3 Add `SECURITY.md` with how to report vulnerabilities; verify it is linked from the README

## 2. Release pipeline

- [ ] 2.1 Add the reusable release workflow (GoReleaser, amd64 and arm64) with `actions/attest-build-provenance` and Syft SBOMs; verify a test tag produces binaries, attestations and SBOMs
- [ ] 2.2 Document `gh attestation verify` in the install docs; verify the documented command succeeds on the test release and fails on a modified binary

## 3. Init hardening

- [ ] 3.1 Add the preflight checks (root, tested platform, already initialized, SSH lockout guard) that run before any change; verify with e2e tests that each refusal leaves the host unchanged and prints the specified message
- [ ] 3.2 Apply the SSH settings as a drop-in that sorts first; verify with `sshd -T` in e2e
- [ ] 3.3 Apply the firewall default deny with 22, 80 and 443 open; verify from outside the VM that other ports are closed
- [ ] 3.4 Enable unattended security upgrades; verify the config in e2e
- [ ] 3.5 Apply the sysctls with persistence; verify the values after a reboot in e2e
- [ ] 3.6 Install auditd with the subset rules and confirm AppArmor is enforcing; verify both in e2e
- [ ] 3.7 Disable unneeded services and set sensitive file permissions; verify in e2e
- [ ] 3.8 Add Lynis to the e2e suite and set the minimum score after the first green run; verify CI fails when a hardening step is skipped

## 4. Dashboard first access

- [ ] 4.1 Add the password policy (length, no composition rules, paste allowed, bundled list plus HIBP range check with padding); verify each `Password policy` scenario with a test that includes the ASVS ID in its name
- [ ] 4.2 Store passwords as Argon2id with parameters kept in the hash; verify the database holds no plaintext
- [ ] 4.3 Add TOTP replay protection by storing the last accepted time step; verify the replay test fails before the change and passes after
- [ ] 4.4 Add per-IP and per-account rate limiting with backoff, stored in SQLite; verify the limits survive a service restart
- [ ] 4.5 Add session handling with a `__Host-` cookie, `SameSite=Strict`, a 30-minute idle timeout and a 12-hour absolute timeout, and revoke sessions on credential reset; verify each `Session management` scenario
- [ ] 4.6 Add security events (SQLite list on the dashboard home plus the journal) for setup, login success and failure, rate limiting and `reset-login`; verify no event contains secrets
- [ ] 4.7 Document both login paths (setup link and `sudo tero reset-login`); verify the doc is linked from `security/asvs-l2.md` under 6.3.4

## 5. End-to-end

- [ ] 5.1 Run the full slice-1 flow on fresh Ubuntu 26.04 VMs (amd64 and arm64): install, init, setup, login, reset-login, login; verify every scenario in `installation`, `server-init`, `process-isolation` and `dashboard-access` has a passing test
