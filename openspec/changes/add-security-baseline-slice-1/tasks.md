Implements all of slice 1: the main specs (`installation`, `server-init`, `process-isolation`, `dashboard-access`) and this change's deltas to them. Each task names the spec scenarios it makes pass, written as `capability: Scenario`. Parts 1 and 2 are tested in fresh Lima VMs (Ubuntu 24.04 and 26.04, arm64) on the Mac, and part 4 adds amd64 and real cloud servers.

## 1. Init and the host lockdown

- [x] 1.1 Create the Go module `github.com/leanhanc/tero` with a `tero` CLI (subcommands `init`, `serve`, `reset-login`, `version`) and a `Makefile` that cross-compiles linux/arm64 and linux/amd64; verify `make build` produces both binaries and `tero version` runs in the VM
- [x] 1.2 Add a Lima template per tested release, `lima/tero-ubuntu-<release>.yaml` (plain Ubuntu, nothing preinstalled, an SSH key for the default user), and `scripts/vm-test.sh` that recreates each VM, copies the binary in, and runs the test suite; verify a fresh VM comes up and runs `tero version` with no manual steps
- [x] 1.3 Add the Go e2e test harness that runs commands in the VM over SSH and asserts on output, files and exit codes; verify a trivial test passes against the VM
- [x] 1.4 Add the root check and the shared helper that root-only subcommands use; covers `server-init: Init without root` and `process-isolation: Root command run without sudo`
- [x] 1.5 Add platform detection from `/etc/os-release` and `uname -m` against the tested list; verify `init` refuses on an untested release (Ubuntu 22.04) with a message listing tested platforms (one extra Lima template, run on demand)
- [x] 1.6 Add the preflight phase that runs every check before any change: root, platform, already initialized, SSH lockout guard. Covers `server-init: SSH lockout guard` (both scenarios); verify a refused run leaves `/etc` byte-identical (checksum before and after)
- [x] 1.7 Install Podman and its rootless dependencies from the distro repositories; covers `server-init: Podman available to tero`
- [x] 1.8 Create the `tero` system user with no login shell, no password, linger enabled, and a 1,000,000-id non-overlapping subuid and subgid range. Covers `server-init: Subordinate id range sized for rootless BuildKit`, `Linger enabled`, and `tero cannot log in interactively`; also run `podman run --rm` as `tero` and check `podman top` host uids to cover `process-isolation: No container process owned by host root`
- [x] 1.9 Write `docs/security/cis-subset.md` with controls by topic, CIS IDs for Ubuntu (mapped to CIS 24.04 v2.0.0 until a 26.04 benchmark exists), and the skipped controls with reasons; verify every hardening scenario below points to a row
- [x] 1.10 SSH hardening as a drop-in that sorts first; covers `server-init: SSH locked down`, verified with `sshd -T`
- [x] 1.11 Firewall default deny with the SSH server's ports, 80 and 443 open; covers `server-init: Firewall default deny`, verified by probing the VM from a separate network namespace that other ports are closed
- [x] 1.12 Unattended security upgrades; covers `server-init: Automatic security updates`
- [x] 1.13 Kernel sysctls with persistence; covers `server-init: Kernel settings`, verified after a VM reboot
- [x] 1.14 auditd with the subset's rules, and AppArmor in enforcing mode; covers `server-init: Auditing and AppArmor`
- [x] 1.15 Disable unneeded services and set sensitive file permissions; covers `server-init: Unneeded services and file permissions`
- [x] 1.16 Add the systemd unit that runs `tero serve` as `tero` with only `CAP_NET_BIND_SERVICE`, restarting on failure and starting on boot. Covers `server-init: Service running after init` and `Service survives reboot`, and `process-isolation: Service process identity`
- [x] 1.17 Embed Caddy in `tero serve` to serve a placeholder page on the dashboard domain over HTTPS, with HTTP redirecting to HTTPS. Covers `process-isolation: Built-in proxy binds privileged ports`, `dashboard-access: Plain HTTP request`, and `server-init: Admin provides a domain` / `Admin skips the domain` (in the VM, certificates come from a local ACME test server; see design)
- [x] 1.18 Record init completion and refuse a second run with the state message; covers `server-init: Init on an already initialized server`
- [x] 1.19 Add Lynis to the VM suite and record the score from the first green run as the minimum; covers `server-init: Lynis score in end-to-end tests` (locally) and `Hardening applied`
- [x] 1.20 Add Ubuntu 24.04 LTS to the tested list next to 26.04 and run the whole part 1 suite in a fresh VM of each; covers `installation: Tested platforms` for init's platform check

## 2. First access to the dashboard

- [ ] 2.0 Set up the dashboard frontend (React + TypeScript with Vite, using Bun for installs and scripts) under `web/`, built from `make build` through `bun run build`, embedded in the binary with `go:embed` and served with a strict Content-Security-Policy; verify `make build` produces a binary that serves the app with no Node or Bun on the server, and that the CSP header has no `unsafe-inline`
- [x] 2.1 Add the SQLite store owned by `tero` with migrations; verify the database file is mode 0600 and owned by `tero`
- [x] 2.2 Generate the one-time setup token at the end of init and print the link; covers `server-init: Link printed at the end of init`
- [ ] 2.3 Add the setup page: password policy (15-character minimum, at least 64 accepted, no composition rules, paste allowed, bundled common-password list plus padded HIBP range check), stored as an Argon2id hash. Covers `dashboard-access: Password policy` (all scenarios) and `Password storage`
- [ ] 2.4 Add TOTP enrolment with a QR code and a confirm step, required to finish setup; covers `dashboard-access: Admin completes setup`, `Setup without TOTP`, `Link reused`, and `No login before setup`
- [x] 2.5 Add login with password and TOTP, a generic error, and single-use codes; covers `dashboard-access: Correct password and code`, `Correct password, wrong code`, and `Replayed code`
- [x] 2.6 Add per-IP and per-account rate limiting with backoff, stored in SQLite; covers `dashboard-access: Brute-force protection` (all scenarios), and verify the limits survive a service restart
- [x] 2.7 Add sessions with a `__Host-` cookie, `SameSite=Strict`, a 30-minute idle timeout and a 12-hour absolute timeout; covers `dashboard-access: Cookie attributes`, `Session cookie flags`, `Idle timeout`, and `Absolute timeout` (with an injectable clock)
- [x] 2.8 Add `sudo tero reset-login`: clears the password and TOTP, revokes sessions, prints a new link, and records an event. Covers `dashboard-access: Admin loses their authenticator`, `Server settings unaffected by reset`, and `Credential change revokes sessions`
- [ ] 2.9 Add the security events list on the dashboard home and in the journal; covers `dashboard-access: Events recorded`, `Recovery visible after the fact`, and `No secrets in events`
- [x] 2.10 Check the single-admin and no-root-from-dashboard rules; covers `dashboard-access: Second account` and `process-isolation: Dashboard cannot trigger root work` (a test that walks every dashboard route and asserts no process with uid 0 is spawned)
- [ ] 2.11 Write `docs/security/asvs-l2.md` with one row per ASVS 5.0.0 L2 requirement, and document both login paths; verify every ASVS ID in `dashboard-access` has a row and a test whose name includes the ID

## 3. Releases and the install script

- [ ] 3.1 Add the reusable release workflow (GoReleaser, amd64 and arm64, checksums, Syft SBOMs, `actions/attest-build-provenance`); verify a test tag produces binaries, checksums, SBOMs and attestations. Covers `installation: SBOM published`
- [ ] 3.2 Write `install.sh`: tested-platform check, architecture mapping, download, checksum check, install to `/usr/local/bin`, next-step message, and no other system changes. Covers `installation: Install on supported amd64 host` / `arm64 host`, `Untested OS`, `Untested architecture`, `Fresh install`, `Install script does not configure the server`, and `Checksum mismatch`, verified in the VM with a local fake release server
- [ ] 3.3 Document install and `gh attestation verify`; covers `installation: Verify a release binary` and `Tampered binary`
- [ ] 3.4 Add `SECURITY.md` with how to report vulnerabilities, and turn on branch protection plus OpenSSF Scorecard in CI; verify Scorecard runs on the default branch

## 4. End-to-end CI on a cloud provider

- [ ] 4.1 Pick the e2e cloud provider (it needs an API to create and destroy servers, Ubuntu 24.04 and 26.04 images, and ideally arm64), then set up CI credentials limited to e2e servers, a budget alert, and a cleanup job for stray servers; verify the credentials cannot touch anything else
- [ ] 4.2 Add a CI job that launches fresh servers on each tested release (amd64, plus arm64 where the provider offers it) with public IPs, installs through `install.sh` from the release under test, runs `sudo tero init`, and runs the whole e2e suite with real Let's Encrypt certificates on sslip.io; verify every scenario in the four slice-1 specs passes on both architectures
- [ ] 4.3 Enforce the Lynis minimum in CI; verify the job fails when a hardening step is skipped on purpose
