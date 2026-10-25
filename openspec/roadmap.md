# Roadmap

Tero v1 is built in vertical slices. Each slice ends with something an admin can do on a real server, covered by end-to-end tests on every tested platform. A slice becomes an OpenSpec change when work on it starts; until then its scope lives here and in [drafts/](drafts/).

v1 is one server and one admin: GitHub deploy on push, Railpack builds with a Dockerfile fallback, HTTPS on a custom domain, environment variables, web and worker apps, and a dashboard with apps, deploys, logs and rollback.

## Slice 1: Onboarding and lockdown

Change: [add-security-baseline-slice-1](changes/add-security-baseline-slice-1/)

- [x] `sudo tero init` with the host lockdown and the `tero` service user
- [x] First dashboard access: one-time link, password and TOTP, `sudo tero reset-login`
- [x] ASVS L2 mapping document
- [ ] Signed releases with SBOMs and provenance, and `install.sh`
- [ ] End-to-end CI on real cloud servers with a Lynis minimum

## Slice 2: First deploy of a JS backend

Deploy a JavaScript backend from a public GitHub repository to an HTTPS address, by hand from the dashboard.

Change: [add-first-deploy-slice-2](changes/add-first-deploy-slice-2/)

- [x] Create a web app from a public GitHub URL and branch
- [x] Build with Railpack as a library in a throwaway rootless BuildKit, load into Podman, no registry
- [x] Bun, npm, Yarn and pnpm projects, running on Node or Bun as Railpack detects
- [x] Run the app under `tero` with `PORT` set, and fail the deploy if it never listens
- [x] Default HTTPS address on `<app>.<public-ip>.nip.io`
- [x] Deploy states (queued, building, starting, live, failed) and a streamed build log
- [x] Manual redeploy of the branch's latest commit
- [ ] End-to-end run on cloud servers, once slice 1's cloud CI exists

Drafts: [apps](drafts/apps/spec.md), [build-pipeline](drafts/build-pipeline/spec.md), [deployments](drafts/deployments/spec.md), [domains](drafts/domains/spec.md)

## Slice 3: Key service and breach response

The internet-facing dashboard can't read Tero's master key or edit the security log, and one command responds to a suspected breach.

Change: [add-key-service-slice-3](changes/add-key-service-slice-3/)

- [x] A `tero-keys` service under its own system user, with no network and a Unix socket that answers only the dashboard (by its systemd unit) and root
- [x] A master key sealed by systemd (bound to the TPM when the server has one) that only `tero-keys` receives; a key that won't open never takes apps offline
- [x] The admin's password hash, TOTP secret (encrypted) and setup links held by `tero-keys`, which checks every login; only root issues setup links
- [x] Security events written by `tero-keys`, so nothing running as `tero` can change or delete them
- [x] `sudo tero rotate-key`
- [x] `sudo tero assume-breach`: stops the dashboard, ends every session, forgets trusted browsers, replaces certificates, rotates the master key, resets the login and prints what to check and rotate elsewhere

Requirements: ASVS 11.1.1, 11.3.1, 11.3.2, 11.3.3, 13.3.1, 13.3.2, 13.3.3, 13.3.4, 16.4.2 ([asvs-l2.md](../docs/security/asvs-l2.md))

## Slice 4: Account security

The dashboard gains its first settings page, and sessions move into the key service.

Change: [add-account-security-slice-4](changes/add-account-security-slice-4/)

- [x] Change the password with the current password, a fresh TOTP code and the usual length and breach checks
- [x] Option to end every other session after a change
- [x] List active sessions and end any of them, or all
- [x] Refuse new passwords that contain context words such as "tero" or the dashboard domain
- [x] Passkeys: add one or more from the settings page (after a fresh password and TOTP check), name and remove them, and log in with a passkey alone, which is phishing-resistant; password and TOTP stay as the fallback
- [x] Sessions, trusted browsers and login throttles held by `tero-keys`, which creates every session, so nothing running as `tero` can make one

Requirements: ASVS 6.2.2, 6.2.3, 6.2.11, 6.3.3, 7.4.3, 7.4.5, 7.5.1, 7.5.2 ([asvs-l2.md](../docs/security/asvs-l2.md))

## Slice 5: Deploy on push

- [ ] Connect GitHub through a per-server GitHub App (manifest flow)
- [ ] Private repositories with short-lived installation tokens
- [ ] Signed push webhooks start a deploy of the tracked branch
- [ ] Polling for public repositories
- [ ] One deploy at a time per app; a newer commit replaces a queued one

Drafts: [github-connection](drafts/github-connection/spec.md), [deployments](drafts/deployments/spec.md)

## Slice 6: Environment variables and secrets

- [ ] Set, change and remove variables per app
- [ ] A change restarts the live image with the new values, with no build; only variables marked "needed at build" rebuild
- [ ] Secrets encrypted per app under the slice 3 master key and kept out of logs and images
- [ ] Decide how the agent receives secrets: it can't yet be told apart from other processes running as `tero`, and the dashboard writes the deploys it runs. Candidates: the agent under its own uid, and `tero-keys` releasing an app's secrets only for a source the admin approved with a fresh TOTP check in `tero-keys`
- [ ] Per-secret list of when it was read, from the security log
- [ ] `sudo tero assume-breach` lists each app's secret names to rotate at their providers and the link to revoke the GitHub App
- [ ] Recovery key printed once, and `sudo tero restore` for moving encrypted secrets to a new server

Drafts: [apps](drafts/apps/spec.md)

## Slice 7: Safe releases and rollback

- [ ] Traffic moves to a new release only after it passes its start check
- [ ] Deploy history with commit, trigger, state and duration
- [ ] One-click rollback to a kept image with that deploy's variables
- [ ] Image retention and pruning

Drafts: [deployments](drafts/deployments/spec.md)

## Slice 8: Custom domains

- [ ] Add and remove custom domains with automatic certificates
- [ ] Pending state with the DNS record to create
- [ ] Reject a domain already in use

Drafts: [domains](drafts/domains/spec.md)

## Slice 9: Runtime logs

- [ ] Live and recent stdout and stderr per app in the dashboard
- [ ] Logs kept across restarts and deploys, up to a per-app limit

Drafts: [app-logs](drafts/app-logs/spec.md)

## Slice 10: Workers and other build paths

- [ ] Worker apps: no route, no published port, restarted on exit
- [ ] Dockerfile fallback
- [ ] Other languages Railpack supports, starting with Go and Python
- [ ] Delete an app

Drafts: [apps](drafts/apps/spec.md), [build-pipeline](drafts/build-pipeline/spec.md)

## Slice 11: Remove Tero from a server

`sudo tero purge` takes the server back to how it was before `sudo tero init`.

- [ ] List everything that will be removed, then ask the admin to type the dashboard domain to confirm; `--yes` skips the prompt for scripts
- [ ] Stop and remove every app container, image and the BuildKit cache, then the agent and service units, the `tero` user and its data, configuration and logs
- [ ] Undo init's host changes (firewall rules, SSH and kernel settings). Init doesn't record what it changed yet, so it starts keeping that record, and purge leaves alone anything the admin edited since
- [ ] Remove the `tero` binary last
- [ ] Root only, never offered in the dashboard

## Slice 13: Passkeys only

An opt-in switch that turns the password and TOTP login off, for admins who want only the phishing-resistant path.

- [ ] "Use passkeys only" on the settings page, offered once the admin has a passkey, after a fresh confirmation; with only one, it warns that losing that device means `sudo tero reset-login` over SSH, and suggests adding a second
- [ ] While it is on, `tero-keys` refuses password and code logins, and refuses removing the last passkey
- [ ] Turning it off needs a fresh passkey check; `sudo tero reset-login` turns it off and stays the recovery for a lost device
- [ ] The login page shows only the passkey button while it is on

## Open decisions

- [ ] Decide what the admin can configure, where (dashboard or `sudo tero ...`), and the defaults. Candidates: log retention (security events, kept one year today, and each app's runtime logs), how many images are kept for rollback, and resource limits per app

## After v1

- Preview deploys for pull requests

### Tero addresses

Default addresses under a domain Tero owns, instead of a third-party wildcard DNS whose name means nothing to the admin or their visitors.

- [ ] Run an open-source wildcard DNS server (sslip.io's) for a Tero domain, on two small servers, answering names like `shop.5-6-7-8.<tero-domain>` with the IP inside them; no signup and nothing stored about servers
- [ ] Add the domain to the Public Suffix List, so each server gets its own Let's Encrypt limits and one server's abuse doesn't taint the others
- [ ] Switch new app and dashboard addresses to it, falling back to nip.io while it's unreachable
- [ ] Plan for abuse reports and uptime, since every server's default address depends on it

### Private access

Opt-in ways to keep the server off the public internet, for admins who want a smaller attack surface than a public dashboard and SSH port.

- [ ] Private access with WireGuard built into Tero: `sudo tero init` or the dashboard turns it on, Tero manages the keys, and the dashboard shows a QR code to add each device
- [ ] Once on, SSH and the dashboard answer only over WireGuard; Tero first checks for a recent handshake from one of your devices, so turning it on can't lock you out
- [ ] Only accept web traffic from Cloudflare's IP ranges, for apps whose domain is behind Cloudflare
- [ ] Later, a Cloudflare Tunnel mode where the server answers on no public port at all
