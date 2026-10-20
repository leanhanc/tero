Implements all of slice 2: the new capabilities `apps`, `build-pipeline`, `deployments` and `domains`, and this change's deltas to `process-isolation` and `server-init`. Each task names the spec scenarios it makes pass, written as `capability: Scenario`. Parts 1 to 4 are tested in fresh Lima VMs (Ubuntu 24.04 and 26.04, arm64) on the Mac, with certificates from Pebble as in slice 1. Task 5.2 runs on the cloud servers from slice 1's part 4, which is still open.

## 1. Prove the build path on both releases

- [x] 1.1 Rerun the spike program from `spike/railpack-podman-buildkit` (locally, never pushed) in fresh 24.04 and 26.04 VMs created from slice 1's templates after `sudo tero init`, as `tero` under its user manager; record in `SPIKE.md` what differs on Podman 5.7 (pasta, `--userns=auto`, cgroups). Stop and revise the design if 26.04 needs a different approach
- [x] 1.2 In the same VMs, remove the BuildKit flags `seccomp=unconfined`, `apparmor=unconfined` and `--device /dev/fuse` one at a time and keep only what builds need; run two `--userns=auto` app containers while a build runs; measure BuildKit's peak memory on the node and Bun fixtures. Record the results in `SPIKE.md` and settle the design's open questions
- [x] 1.3 Write the agent's user unit with the strictest systemd hardening under which rootless Podman and BuildKit still work, and record each setting that had to be left out and why

## 2. Agent, data model and the build

- [x] 2.1 Add the `apps` and `deploys` tables to the store's migrations (app name, repository, branch, created; deploy app, state, step, reason, commit SHA and message, image, container, host port, timestamps)
- [x] 2.2 Add `tero agent` (runs only as `tero`, refuses root) with the deploy queue: claim the oldest queued deploy in one transaction, one at a time, and mark deploys left in building or starting as interrupted on start. Covers `deployments: Interrupted deploy` and `Two apps deployed together`
- [x] 2.3 Init installs the agent unit in `/etc/systemd/user/` with `ConditionUser=tero` and enables it; init records the public IP in `/etc/tero/config.json`; the service unit gets `InaccessiblePaths=` for `tero`'s `.config`, `.local` and `.cache`. Covers `process-isolation: Service can't change the agent's setup`, `server-init: Agent running after init`, `Agent survives reboot`, `Admin provides a domain`, `Admin skips the domain`, and `process-isolation: Agent unit not writable by tero`
- [x] 2.4 Add the GitHub URL parser (only `https://github.com/<owner>/<repo>`, optional `.git`) and the go-git fetch: default branch from remote `HEAD`, branch check, depth-1 clone, and the root directory check. Covers `apps: Path outside the repository`, `apps: Not a GitHub URL` (with a test that no request leaves for the given host), `Repository or branch not found`, and `build-pipeline: Commit recorded`
- [x] 2.5 Move the spike's Railpack planning and BuildKit build into `internal/build`: plan, LLB for the host platform, the throwaway BuildKit with the flags from 1.2 and the cache volume, `dial-stdio`, tarball export, `podman load`, cleanup. Covers `build-pipeline: Plan targets the host platform`, `BuildKit lifecycle`, `BuildKit container flags`, `No published port or socket`, `Second build reuses the cache`, and `Image available after build`
- [x] 2.6 Map Railpack's failures to dashboard messages: unsuccessful plan, no start command, build error. Covers `build-pipeline: Railpack cannot plan` and `No start command`
- [x] 2.7 Write the build log file with Tero's step lines, ANSI stripped, capped at 10 MB; add the 30-minute build timeout. Covers `build-pipeline: Failed build log kept` and `Build timeout` (a unit test checks that anything failing past the deadline reads as the timeout)
- [x] 2.8 Add the fixtures under `e2e/fixtures/` (`npm`, `pnpm`, `yarn`, `bun`, `no-start`, `never-listens`, `build-fails`), each a standalone minimal backend with its own lockfile that prints `PORT` and its runtime, plus the e2e-only local source that reads them from the VM. Covers `build-pipeline: JavaScript package managers and runtimes` and `apps: App in a subfolder` once part 3 runs them

## 3. Running apps and routing

- [x] 3.1 Start app containers from the agent: a network namespace of its own with no route to the host's loopback, `--cap-drop=ALL`, `no-new-privileges`, `--userns=auto`, `k8s-file` logs with a size cap, `PORT=8080` published on a free `127.0.0.1` port. Covers `apps: PORT set` and `process-isolation: Container options`, `Apps run as different host uids`, `Apps can't reach each other directly`, `App port not exposed`
- [x] 3.2 Add the start check (HTTP request every second, any response passes, 60 seconds) and the failure paths that append the container's last 50 lines. Covers `deployments: App never listens`, `App exits on start`, and `Successful deploy`
- [x] 3.3 Start the new container next to the live one, switch the live deploy in one transaction, then stop the old container and remove its image. Covers `deployments: Failed redeploy` and `Failed deploy`
- [x] 3.4 Add the per-app "Run old and new releases together during deploys" setting (on by default); when off, stop the old container after the build, start the new one, and on failure start the previous image again. Covers `deployments: Setting off, deploy succeeds` and `Setting off, deploy fails`
- [x] 3.5 Add the agent's reconcile loop (on start and every 10 seconds). Covers `apps: App process exits` and `Reboot`
- [x] 3.6 Make the service build Caddy's config from the dashboard domain plus one host per app, reload it when apps or live deploys change, serve the "not deployed yet" page, and redirect HTTP. Covers `domains: New web app reachable without DNS`, `Dashboard on a custom domain`, `No route before live`, `HTTP request to an app`, and `Dashboard API on an app host`
- [ ] 3.7 Check the service stays as it was. Covers `process-isolation: Service sandbox unchanged` and `Agent has no listener`, and extend slice 1's `Dashboard cannot trigger root work` test to the new routes

## 4. Dashboard

- [x] 4.1 Add the app and deploy API: create app (with the URL lookup for branch and name), list apps, get app, list deploys, redeploy (409 while one is in progress), and the SSE log stream that follows the file until the deploy ends. Same session, CSRF and content-type rules as slice 1. Covers `apps: Invalid name`, `Name taken`, `Create a web app`, and `deployments: Redeploy while deploying`
- [x] 4.2 Read the code-style skill, then build the apps list on the home page (security events move below it), with `features/apps/apps-page.tsx` and its components. Covers `apps: App list`
- [x] 4.3 Build the new-app page with the URL lookup filling branch and name, and field errors from the API. Covers `apps: Default branch suggested`
- [x] 4.4 Build the app page: address link, live commit, Redeploy, the old-and-new-together setting, deploys newest first with live state, the log of the selected deploy streamed as plain text, and each failure's step and reason. Covers `apps: App page`, `build-pipeline: Watch a build`, `Log rendered as text`, and `deployments: Redeploy picks up a new commit`
- [x] 4.5 Update `docs/security/asvs-l2.md` (1.3.6 met, rows for the new endpoints, 13.2.4 still a gap with builds listed in the outbound table) and `cis-subset.md` if the agent unit changes anything there; verify every ASVS ID cited in this change has a row and a test

## 5. End to end and housekeeping

- [x] 5.1 Add e2e tests that deploy every fixture through the dashboard API in both VMs (local source) and from GitHub in CI, check each answers on its address with the expected runtime, and check the failing fixtures fail at the expected step with the expected message
- [ ] 5.2 Once slice 1's part 4 exists, run the slice 2 suite on the cloud servers (amd64 and arm64) with real Let's Encrypt certificates on sslip.io, and check the Lynis score has not dropped
- [x] 5.3 Move `openspec/config.yaml`'s context to slice 2, trim the four drafts to what later slices still own, and tick slice 2 in `openspec/roadmap.md`
