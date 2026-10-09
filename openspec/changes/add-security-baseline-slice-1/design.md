## Context

ADR 0001 (`tero/adr-0001-security-baseline.md` in the project files) is the source for every requirement in this change. Slice 1 has no app or build containers yet, so only the ADR's dashboard, host and delivery sections apply. The tested platforms are Ubuntu 24.04 LTS and Ubuntu 26.04 LTS. They differ in ways init has to handle: 26.04 ships Podman 5.7 with sudo-rs and the Rust uutils coreutils, and 24.04 ships Podman 4.9 with GNU sudo and coreutils.

## Goals / Non-Goals

**Goals:**
- Every requirement in slice 1 that comes from the ADR can be traced to an ASVS ID, a CIS control, or SLSA.
- The host lockdown can be checked automatically in CI.

**Non-Goals:**
- Claiming CIS compliance. The CIS benchmark is a long list of individual settings ("controls"). Tero applies only the ones that fit a single-disk VPS, and for each one it leaves out, such as putting `/tmp` on its own disk partition, `docs/security/cis-subset.md` says why.
- Passkeys, which come right after v1, and re-asking for TOTP before dangerous actions. Slice 1 has no such actions yet, and the first one (changing the dashboard domain) arrives with a later slice.
- Container rules, secrets, and self-update signature checks, which come in later slices.

## Decisions

- **ASVS traceability.** `docs/security/asvs-l2.md` gets one row per L2 requirement, listing the spec and test that cover it or "not applicable" with a reason. Test names include the ASVS ID, for example `TestLogin_v5_0_0_6_5_1_TOTPReplay`.
- **CIS subset as data.** `docs/security/cis-subset.md` lists the controls by topic, with one CIS ID column per distro. The Ubuntu IDs come from CIS Ubuntu 24.04 LTS v2.0.0, which also stands in for 26.04 until CIS publishes a 26.04 benchmark. Init reads its per-distro settings from one table in code that mirrors this file, so adding a distro means adding a column in both places.
- **Lynis as the CI gate.** The e2e suite starts a fresh VM, installs, runs `sudo tero init`, and then runs Lynis plus Tero's own assertions, one per scenario in `server-init`. The minimum Lynis score is fixed in the suite after the first green run, and later changes can only raise it.
- **SSH lockout guard.** Init runs every check before it makes any change, so a refusal leaves the host untouched. This guard is not in the ADR. It exists because disabling password SSH on a host that only has a password is the easiest way for init to brick a VPS.
- **Breached-password check.** The embedded list is the 3,000 most common passwords of at least 15 characters from SecLists' top 1,000,000 (`xato-net-10-million-passwords-1000000.txt`, MIT licensed), in rank order: about 50 KB, rebuilt with `scripts/update-common-passwords.sh` and committed, so builds need no network. Shorter passwords are already refused by length, and 3,000 is the number ASVS asks for. It is checked on every new password. Have I Been Pwned is queried with `Add-Padding: true`. If the API is unreachable, the embedded list alone decides and a security event records that the online check was skipped. The alternative of failing closed would block setup on a transient network error.
- **Argon2id parameters.** Use RFC 9106's second recommended option (`m=64 MiB, t=3, p=4`). The parameters are stored with the hash so they can be raised later.
- **TOTP replay.** Store the last accepted TOTP time step for the account and reject any code at or before it.
- **Rate limiting.** Keep counters in SQLite so they survive restarts and a restart can't be used to reset them. Use exponential backoff starting after 5 failures per IP and 10 per account.
- **Release provenance.** GoReleaser runs inside a reusable workflow, and the calling workflow only invokes it. That isolation is what reaches SLSA Build L3. `actions/attest-build-provenance` signs through Sigstore, and Syft produces the SBOMs.
- **Per-release differences.** Init shells out as little as possible, and every command it runs is exercised in e2e on each tested release, so both sudo-rs and uutils (26.04) and GNU sudo and coreutils (24.04) are covered.

- **Implementation order.** The slice is built in four parts: init and the lockdown, then dashboard first access, then releases and `install.sh`, then e2e on a cloud provider. Each part leaves a VM that passes everything built so far. Init comes first because everything else runs on a host it prepared.
- **Local test loop.** For each tested release, a Lima VM running plain Ubuntu on the Mac (arm64) is recreated for each full run, the same way the build spike did it. amd64 is first exercised in part 4.
- **HTTPS inside the VM.** The VM has no public IP, so Let's Encrypt can't issue there. The VM runs Pebble, Let's Encrypt's ACME test server, and `tero serve` is pointed at it by a test-only setting. That keeps the real ACME path under test, unlike Caddy's internal CA. Real certificates on sslip.io are first exercised in part 4.
- **Dashboard UI.** A client-side React + TypeScript app built with Vite, with Bun as the package manager and script runner for development and builds (`bun install`, `bun run build`). Bun is used only at build time. The build output is embedded in the Go binary with `go:embed` and served by `tero serve`, so the server needs no Node runtime. All security logic lives in the Go JSON API: authentication, sessions, rate limits and the password checks. React server actions or Next.js would put a Node process on every server, adding attack surface and update burden without moving any security decision out of Go. Because every asset is self-hosted, the dashboard can use a strict Content-Security-Policy (`default-src 'self'`, no inline scripts). State-changing API calls require the `SameSite=Strict` session cookie and a JSON content type, which blocks cross-site form posts.
- **Setup links.** The token sits in the URL fragment (`/setup#<token>`), so browsers never send it in requests, logs or Referer headers, and the dashboard reads it from there. Only its SHA-256 is stored. A link expires after 24 hours (ASVS v5.0.0-6.4.1), and issuing a new one cancels any unused one. Setup has two calls: the first checks the password and returns a TOTP secret, and the second confirms a code and creates the account, so nothing exists until TOTP is confirmed.
- **Root CLI and the database.** The SQLite database belongs to `tero` (`/var/lib/tero/tero.db`, mode 0600). `sudo tero init` and `sudo tero reset-login` never open it as root: they run the binary again as `tero`, which writes and prints the link. That keeps every database file owned by `tero`, and the service and CLI share state through the database with no other channel.
- **Dashboard in Caddy.** The dashboard's Go handler is a Caddy HTTP module inside the same process, not a proxied backend. The client address comes straight from the TCP connection, so rate limits can't be dodged with forwarding headers, and there is no extra listening socket.
- **Throttle numbers.** Lockouts start at the 5th failure in a row per IP (per /64 for IPv6) and the 10th for the account, at 30 seconds, doubling with each further failure up to 15 minutes. Each attempt is counted in one write transaction before the credentials are checked and refunded on success, so parallel requests can't slip past a limit, and a lockout is only ever extended. Counters are forgotten after 24 quiet hours or a successful login. `sudo tero reset-login` also clears them, since whoever runs it has proven root access.
- **Trusted devices.** A successful login sets a `__Host-tero_device` cookie (OWASP's device-cookie pattern). It grants nothing on its own: a browser holding it is throttled on its own failures (5) instead of the account's, so strangers failing logins can't keep the admin out. It lasts 180 days without a login, and `reset-login` forgets all devices.
- **Password hashing bound.** Each Argon2id hash takes 64 MiB, so at most two run at once and other requests wait up to 10 seconds, then get a 503. The service also has `MemoryMax=512M`, so a flood can only take down Tero, never sshd or the apps.
- **Firewall.** nftables rules written by init, rather than ufw. nftables is the kernel's current packet-filtering framework and ufw is a front end to it. The later per-app network isolation in the ADR also uses nftables, so one tool covers both.

## Risks / Trade-offs

- [A cloud image ships an SSH config that overrides Tero's, for example a drop-in under `sshd_config.d`] → Init writes a drop-in that sorts first, and the e2e suite checks the effective settings with `sshd -T`.
- [The service's sandboxing (`NoNewPrivileges`, a bounding set of only `CAP_NET_BIND_SERVICE`) blocks `newuidmap`, so rootless Podman can't be started as a child of the service] → Slice 1 starts no containers from the service. When apps arrive, containers run under `tero`'s systemd user manager (for example with Quadlet) instead of as children of the service.
- [Apport turns setuid core dumps back on at boot] → Init disables Apport, which CIS also recommends; the reboot test checks `fs.suid_dumpable`.
- [Some VPS providers manage their own firewall or AppArmor profiles] → The skipped controls and their reasons go in `cis-subset.md`, and init warns rather than fails when a provider tool already owns a setting.
- [The `curl | sh` install can only check a checksum from the same origin] → The ADR accepts this. Provenance can be checked with `gh attestation verify`, and self-update verification arrives with the self-update slice.

- [sslip.io names may hit Let's Encrypt rate limits if many Tero servers share them] → Check whether sslip.io is on the Public Suffix List, which makes limits apply per IP subdomain, before part 4. If it isn't, document the risk and recommend a custom domain.

## Open Questions

- The Lynis minimum score is set after the first green run in CI.
- The exact sysctl list and the set of unneeded services are filled in while writing `cis-subset.md`. The specs refer to that file, so the specs don't change when the list does.
