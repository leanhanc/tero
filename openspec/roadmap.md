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
- [x] Default HTTPS address on `<app>.<public-ip>.sslip.io`
- [x] Deploy states (queued, building, starting, live, failed) and a streamed build log
- [x] Manual redeploy of the branch's latest commit
- [ ] End-to-end run on cloud servers, once slice 1's cloud CI exists

Drafts: [apps](drafts/apps/spec.md), [build-pipeline](drafts/build-pipeline/spec.md), [deployments](drafts/deployments/spec.md), [domains](drafts/domains/spec.md)

## Slice 3: Account security

The dashboard gains its first settings page. Until then, `sudo tero reset-login` is the way to replace a password, and every login still needs TOTP.

- [ ] Change the password with the current password, a fresh TOTP code and the usual length and breach checks
- [ ] Option to end every other session after a change
- [ ] List active sessions and end any of them, or all
- [ ] Refuse new passwords that contain context words such as "tero" or the dashboard domain

Requirements: ASVS 6.2.2, 6.2.3, 6.2.11, 7.4.3, 7.4.5, 7.5.1, 7.5.2 ([asvs-l2.md](../docs/security/asvs-l2.md))

## Slice 4: Deploy on push

- [ ] Connect GitHub through a per-server GitHub App (manifest flow)
- [ ] Private repositories with short-lived installation tokens
- [ ] Signed push webhooks start a deploy of the tracked branch
- [ ] Polling for public repositories
- [ ] One deploy at a time per app; a newer commit replaces a queued one

Drafts: [github-connection](drafts/github-connection/spec.md), [deployments](drafts/deployments/spec.md)

## Slice 5: Environment variables and secrets

- [ ] Set, change and remove variables per app, applied on the next deploy
- [ ] Secrets stored encrypted and kept out of logs and images
- [ ] Redeploy offered after a change

Drafts: [apps](drafts/apps/spec.md)

## Slice 6: Safe releases and rollback

- [ ] Traffic moves to a new release only after it passes its start check
- [ ] Deploy history with commit, trigger, state and duration
- [ ] One-click rollback to a kept image with that deploy's variables
- [ ] Image retention and pruning

Drafts: [deployments](drafts/deployments/spec.md)

## Slice 7: Custom domains

- [ ] Add and remove custom domains with automatic certificates
- [ ] Pending state with the DNS record to create
- [ ] Reject a domain already in use

Drafts: [domains](drafts/domains/spec.md)

## Slice 8: Runtime logs

- [ ] Live and recent stdout and stderr per app in the dashboard
- [ ] Logs kept across restarts and deploys, up to a per-app limit

Drafts: [app-logs](drafts/app-logs/spec.md)

## Slice 9: Workers and other build paths

- [ ] Worker apps: no route, no published port, restarted on exit
- [ ] Dockerfile fallback
- [ ] Other languages Railpack supports, starting with Go and Python
- [ ] Delete an app

Drafts: [apps](drafts/apps/spec.md), [build-pipeline](drafts/build-pipeline/spec.md)

## Open decisions

- [ ] Decide what the admin can configure, where (dashboard or `sudo tero ...`), and the defaults. Candidates: log retention (security events, kept one year today, and each app's runtime logs), how many images are kept for rollback, and resource limits per app

## After v1

- Preview deploys for pull requests
