## Why

Slice 1 leaves a locked-down server with a dashboard that does nothing yet. Slice 2 is the first time Tero does its job: the admin pastes a public GitHub URL, clicks deploy, watches the build, and gets a working HTTPS address. The spike showed the build path works (Railpack as a library, a throwaway rootless BuildKit under Podman, `podman load`, no registry), so this slice turns it into product, under the ADR 0001 container rules that slice 1 deferred.

## What Changes

- Apps: the admin creates a web app from a public GitHub URL, a branch, a name and an optional root directory for monorepos. The dashboard lists apps and shows each app's address, state and deploys.
- Builds: Railpack plans the build, a fresh rootless BuildKit container runs it, and the image is loaded into `tero`'s Podman store. Bun, npm, Yarn and pnpm projects build and run on Node or Bun, as Railpack detects. A project with no start command fails with a message that says what to add.
- Running: each app runs in its own rootless container under `tero` with `PORT` set, in a network namespace of its own that can't reach other apps, with all capabilities dropped and its own user namespace. A deploy that never answers HTTP on `PORT` fails, and the app's last output is shown.
- Addresses: every web app answers on `https://<app>.<public-ip>.sslip.io` with a Let's Encrypt certificate, and HTTP redirects to HTTPS.
- Deploys: states queued, building, starting, live and failed, a build log streamed live and kept afterwards, and a Redeploy button that deploys the branch's latest commit. Old and new releases run together during a deploy by default, a per-app setting turns that off, and either way a failed redeploy leaves the old release serving.
- A new `tero agent` process runs under `tero`'s systemd user manager and does all builds and container work. The internet-facing service keeps its slice 1 sandbox and starts no containers.
- Init records the server's public IP and installs the agent's user unit.

## Capabilities

### New Capabilities

- `apps`: create a web app from a public GitHub URL and branch, app names, the app list and app page.
- `build-pipeline`: Railpack planning, the throwaway rootless BuildKit, loading into Podman, the build log.
- `deployments`: deploy states, manual deploy and redeploy, the start check, one deploy at a time.
- `domains`: the default `<app>.<public-ip>.sslip.io` address with HTTPS and the HTTP redirect.

### Modified Capabilities

- `process-isolation`: ADR 0001's container rules for app and build containers; the agent runs as `tero` outside the service's sandbox and has no network listener.
- `server-init`: init records the public IP and installs and enables the agent.

## Impact

- New Go dependencies: Railpack v0.40.1 (as a library), the BuildKit client v0.33.0, and go-git. Railpack requires Go 1.26.6, and the binary grows by about 30 MB.
- New SQLite tables for apps and deploys; build logs as files under `/var/lib/tero/logs/deploys/`.
- Outbound traffic during builds: GitHub, `ghcr.io` (Railpack images), Docker Hub (the pinned BuildKit image), and whatever package registries the app's build reaches. The ASVS outbound-allowlist gap (13.2.4) stays open and is documented as such.
- `docs/security/asvs-l2.md`: 1.3.6 (SSRF) moves from "Later" to "Met" through the GitHub-only URL rule; new rows for the app and deploy API.
- Dashboard: new apps list, new-app form, app page with deploys, and a live log view.
- `openspec/config.yaml` context moves to slice 2, and the four drafts keep only what later slices still own.
