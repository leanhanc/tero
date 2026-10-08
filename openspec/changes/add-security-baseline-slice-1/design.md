## Context

ADR 0001 (`tero/adr-0001-security-baseline.md` in the project files) is the source for every requirement in this change. Slice 1 has no app or build containers yet, so only the ADR's dashboard, host and delivery sections apply. Ubuntu 26.04 LTS is the first tested platform. On it, `sudo` is sudo-rs and coreutils are the Rust uutils.

## Goals / Non-Goals

**Goals:**
- Every requirement in slice 1 that comes from the ADR can be traced to an ASVS ID, a CIS control, or SLSA.
- The host lockdown can be checked automatically in CI.

**Non-Goals:**
- Claiming CIS compliance. The CIS benchmark is a long list of individual settings ("controls"). Tero applies only the ones that fit a single-disk VPS, and for each one it leaves out, such as putting `/tmp` on its own disk partition, `security/cis-subset.md` says why.
- Passkeys, which come right after v1, and re-asking for TOTP before dangerous actions. Slice 1 has no such actions yet, and the first one (changing the dashboard domain) arrives with a later slice.
- Container rules, secrets, and self-update signature checks, which come in later slices.

## Decisions

- **ASVS traceability.** `security/asvs-l2.md` gets one row per L2 requirement, listing the spec and test that cover it or "not applicable" with a reason. Test names include the ASVS ID, for example `TestLogin_v5_0_0_6_5_1_TOTPReplay`.
- **CIS subset as data.** `security/cis-subset.md` lists the controls by topic, with one CIS ID column per distro. Until CIS publishes a 26.04 benchmark, the IDs come from CIS Ubuntu 24.04 LTS v2.0.0. Init reads its per-distro settings from one table in code that mirrors this file, so adding a distro means adding a column in both places.
- **Lynis as the CI gate.** The e2e suite starts a fresh VM, installs, runs `sudo tero init`, and then runs Lynis plus Tero's own assertions, one per scenario in `server-init`. The minimum Lynis score is fixed in the suite after the first green run, and later changes can only raise it.
- **SSH lockout guard.** Init runs every check before it makes any change, so a refusal leaves the host untouched. This guard is not in the ADR. It exists because disabling password SSH on a host that only has a password is the easiest way for init to brick a VPS.
- **Breached-password check.** The embedded list is the top 100k common passwords. Have I Been Pwned is queried with `Add-Padding: true`. If the API is unreachable, the embedded list alone decides and a security event records that the online check was skipped. The alternative of failing closed would block setup on a transient network error.
- **Argon2id parameters.** Use RFC 9106's second recommended option (`m=64 MiB, t=3, p=4`). The parameters are stored with the hash so they can be raised later.
- **TOTP replay.** Store the last accepted TOTP time step for the account and reject any code at or before it.
- **Rate limiting.** Keep counters in SQLite so they survive restarts and a restart can't be used to reset them. Use exponential backoff starting after 5 failures per IP and 10 per account.
- **Release provenance.** GoReleaser runs inside a reusable workflow, and the calling workflow only invokes it. That isolation is what reaches SLSA Build L3. `actions/attest-build-provenance` signs through Sigstore, and Syft produces the SBOMs.
- **uutils and sudo-rs.** Init shells out as little as possible. Every command it does run is exercised in e2e on 26.04, where uutils and sudo-rs are the defaults.

## Risks / Trade-offs

- [A cloud image ships an SSH config that overrides Tero's, for example a drop-in under `sshd_config.d`] → Init writes a drop-in that sorts first, and the e2e suite checks the effective settings with `sshd -T`.
- [Some VPS providers manage their own firewall or AppArmor profiles] → The skipped controls and their reasons go in `cis-subset.md`, and init warns rather than fails when a provider tool already owns a setting.
- [The `curl | sh` install can only check a checksum from the same origin] → The ADR accepts this. Provenance can be checked with `gh attestation verify`, and self-update verification arrives with the self-update slice.

## Open Questions

- The Lynis minimum score is set after the first green run in CI.
- The exact sysctl list and the set of unneeded services are filled in while writing `cis-subset.md`. The specs refer to that file, so the specs don't change when the list does.
