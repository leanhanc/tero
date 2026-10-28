Implements all of slice 5: the new capability `github-connection` and this change's deltas to `process-isolation`, `server-init`, `deployments`, `apps`, `key-service`, `breach-response` and `security-log`. Each task names the spec scenarios it makes pass, written as `capability: Scenario`. Parts 1 to 6 are covered by Go and web unit tests; part 7 runs in fresh Lima VMs (Ubuntu 24.04 and 26.04, arm64) on the Mac and on amd64 (the Ubuntu PC, or EC2 through `scripts/ec2-test.sh` after asking).

## 1. The agent under its own user

- [x] 1.1 In init, create `tero-agent` (home `/var/lib/tero-agent`, 0700, no login shell, linger, the 1,000,000-id subordinate range) and drop `tero`'s range and linger. Point the agent's unit at `ConditionUser=tero-agent` and remove the Podman `InaccessiblePaths=` from `tero.service`. Covers every `server-init: Tero system user` and `Tero agent` scenario, `process-isolation: Agent unit not writable by tero or tero-agent`, `tero can't run containers`
- [x] 1.2 Create `tero-shared` and `/var/lib/tero-shared` (2770), move `tero.db` and the build logs there, move build directories to the agent's home, and update every path constant (`store`, `deploylog`, `deployer`, `breach`). Check that SQLite's WAL and shared-memory files get the group's permissions on both tested releases. Covers every `server-init: State shared by the service and the agent` scenario, `process-isolation: Service can't change the agent's setup`
- [x] 1.3 Make the agent refuse container options that would map its own uid, and check BuildKit's and apps' mappings next to each other again. Covers `process-isolation: Containers never run as the agent's uid`, every `App container hardening` scenario
- [x] 1.4 Add the `agent` caller to `tero-keys` by peer uid, the `tero-keys-callers` group on the socket, and the agent's verb list. Covers every `key-service: The agent as a caller` scenario
- [x] 1.5 Make `assume-breach` stop `tero-agent`'s user manager and processes and move aside its user units and crontab. Covers `breach-response: Nothing survives in the dashboard's memory`
- [x] 1.6 Run slices 1 to 4's e2e suites in one VM before going further, since every later part builds on this move

## 2. The per-app queue, triggers and the switch

- [x] 2.1 In `0001_apps.sql`, replace `deploys_one_in_progress` with one index for building or starting and one for queued per app, and add `trigger`, `pushed_by` and `requested_sha` to `deploys` and `deploy_on_push` to `apps`. Make `deploys.Enqueue` update the queued row when there is one and queue behind a running deploy otherwise; drop `ErrInProgress`. Covers `deployments: Two quick pushes`, `Redeploy while deploying`, `Redeploy while one is queued`, `Two apps deployed together`
- [x] 2.2 Record the trigger on create and Redeploy, show it on the app page, show "Queued" on the Redeploy button, and add the "Deploy on push" setting. Covers `deployments: Redeploy picks up a new commit`, `Deploy on push turned off`

## 3. GitHub secrets in the key service

- [x] 3.1 Add the `github_app` table to `tero-keys`' `0001_keys.sql` (App id, slug, owner, sealed private key, sealed webhook secret) and re-seal it in `key.rotate`. Covers `key-service: Rotation re-seals the App's secrets`
- [x] 3.2 Add `github.connect.begin`, `github.connect.approve` and the agent's `github.connect`, with the session-bound, single-use, 10-minute state; and `github.disconnect`, `github.status` and root `github.forget`. Covers every `key-service: Connecting GitHub needs the admin's approval` scenario
- [x] 3.3 Add `github.jwt` for the agent (RS256 with the standard library, `iat` 60 seconds back, 5-minute expiry) and `github.webhook.verify` for the dashboard (constant-time HMAC-SHA256, 1 MiB cap, and the protocol's message cap raised to fit it). Covers `key-service: Secrets never leave`, `Short-lived JWT`, `Secrets not in tero's reach`
- [x] 3.4 Add `github_connected`, `github_disconnected` and `webhook_signature_failed`, and keep the first two through a flood. Covers every `security-log` scenario in this change

## 4. Connecting GitHub

- [x] 4.1 Add the agent's GitHub client: fixed hosts, manifest conversion, `GET /app/installations`, installation tokens scoped by repository and permission, `DELETE /installation/token`, repository lists, deliveries and redelivery
- [x] 4.2 Add `github_requests` and the agent's handling of `connect`, `refresh_installation` and `check_source`; store the installation and its repositories. Covers `github-connection: Dashboard never sees the private key`
- [x] 4.3 Add `/api/github/connect/begin` (personal account or organization, returns the manifest, target page and state), the `/github/connected` page that approves the state and posts the code, the wait for the agent, and the redirect to the installation page and back. Covers `github-connection: Connect GitHub`, `Connect an organization`, `Unconfirmed session`, `Callback with a foreign state`, `One account per server`
- [x] 4.4 Add disconnecting and the installation state in settings (removed, suspended), with the link to the App on GitHub, and `github_installation_changed`. Covers every `github-connection: Disconnect GitHub` scenario

## 5. Private repositories

- [x] 5.1 Add `github_repository_id` to `apps`, the repository list on the new-app form, the stored default branch, and the private branch check through the agent. Covers `apps: Create from a private repository`, `Default branch suggested`, `Private repository by URL without the App`, `Repository or branch not found`
- [x] 5.2 Clone covered repositories with a token minted in the agent, scoped to the repository and `contents: read`, used as basic auth in memory and revoked when the clone ends; leave `.git` out of the build context. Covers every `github-connection: No long-lived repository credentials` scenario, every `process-isolation: GitHub credentials only in the agent` scenario

## 6. Deploy on push, polling and redelivery

- [x] 6.1 Add `POST /api/github/webhook`: header checks, 1 MiB cap, per-IP throttle on bad signatures, delivery ids kept 3 days, and the handlers for `ping`, `push`, `installation` and `installation_repositories`. Match pushes by repository id and branch; refresh the stored name from the payload. Covers every `github-connection: Signed webhooks` scenario, `apps: Repository renamed`, `deployments: Push deploys`
- [x] 6.2 Add polling in the agent every minute with jitter for public apps the App doesn't cover; stop after 3 failures and say why. Covers every `github-connection: Public repositories by polling` scenario
- [x] 6.3 When the agent starts, list the App's recent deliveries and ask GitHub to redeliver the failed ones Tero hasn't handled, once per id. Covers every `github-connection: Missed webhooks redelivered at start` scenario

## 7. Breach, pages, docs and end to end

- [x] 7.1 Make `assume-breach` call `github.forget` and print the line to delete the App. Covers `breach-response: GitHub connection deleted`, `What to do elsewhere`
- [x] 7.2 Read `~/.claude/skills/code-style/SKILL.md`. Add the GitHub section to settings, the repository picker on the new-app page, the `/github/connected` page, "Deploy on push" and the trigger on the app page, markup only, in English and Spanish, with tests in `web/test/`
- [ ] 7.3 (In part: the fake GitHub serves conversion, installations, tokens, repository lists and deliveries, and e2e covers connect, a private deploy, push, replay, forgery, revoked tokens and secrets at rest. Still to add: organizations, polling, a push while the server was down, the switch, disconnect, and `assume-breach` with GitHub.) Add the fake GitHub to e2e builds (manifest form and conversion for accounts and organizations, App and installation endpoints, deliveries and redelivery, git smart HTTP for public and private fixtures, signed webhook sender, revocation log), and e2e tests for connect, a private deploy, push, replay, forgery, polling, a push while the server was down, the switch, disconnect, the agent's caller checks and `assume-breach`
- [ ] 7.4 Run slices 1 to 5's e2e suites in both VMs on arm64 and on amd64, and check the Lynis score has not dropped
- [ ] 7.5 One manual run on a cloud server against real GitHub with a test account and a test organization: connect, install on a private repository, push, disconnect, and delete the App
- [x] 7.6 Update `docs/security/asvs-l2.md` (the webhook endpoint, outbound calls to GitHub, the App's secrets in the key service, and the agent's own user), `openspec/roadmap.md` (slice 5's items, and slice 6's open decision now only about which sources get secrets), and `openspec/config.yaml`'s context to slice 5
- [x] 7.7 Adversarial correctness and security review of the whole change by a separate fresh agent with minimal context; fix what it finds
