## Why

Today an app deploys only when it is created and when the admin clicks Redeploy, and only from a public repository. Every push means opening the dashboard, and private code can't be deployed at all. Deploy on push is the core promise of v1, and most real apps live in private repositories.

GitHub offers two ways to reach private code: long-lived credentials (personal access tokens, deploy keys) or a GitHub App that mints tokens lasting an hour. A token stored on the server keeps working after a breach is fixed, which is the persistence slice 3 removed for the master key. A GitHub App created for this one server, whose private key stays in `tero-keys` like the master key, avoids that.

Where those tokens go matters too. The agent shares the `tero` user with the internet-facing dashboard, so `tero-keys` can't tell them apart, and anything given to the agent would be within the dashboard's reach. Slice 6 has to solve this before apps get secrets; this slice is the first to hand the agent a credential, so it solves it now: the agent moves to its own user, and only it talks to GitHub with the App's credentials.

## What Changes

- **The agent runs as its own system user, `tero-agent`**, which owns rootless Podman, the subordinate id range, BuildKit and every app container. `tero-keys` recognises it by uid and answers it. The dashboard and the agent share the database through a group-owned directory. `tero` no longer has a subordinate range, lingering or a user manager.
- **Connect GitHub** from the settings page, for the admin's personal account or an organization. Tero creates a private GitHub App for this server through GitHub's App manifest flow, with read-only access to contents and metadata and the `push` event, and sends the admin to install it on the repositories they choose. One GitHub account per server. Connecting and disconnecting need a confirmed session (password and a fresh TOTP code).
- **The App's private key and webhook secret are held by `tero-keys`**, encrypted under the master key. The agent does the manifest conversion and hands both straight to `tero-keys`, so the dashboard never sees them. `tero-keys` signs short-lived App JWTs for the agent only and checks webhook signatures for the dashboard; it never returns either secret.
- **Private repositories.** The new-app form lists the repositories the App is installed on, next to the public URL field. Every fetch uses an installation token scoped to that one repository with `contents: read`, held only in the agent's memory and revoked as soon as the clone ends. Tero never accepts a personal access token or deploy key.
- **Signed push webhooks** at `https://<dashboard-domain>/api/github/webhook`. A push to an app's tracked branch queues a deploy; anything unsigned, forged, replayed or for another branch queues nothing. The webhook is only a trigger: Tero always builds the branch head it reads from GitHub itself.
- **Deploy on push is a per-app setting**, on by default.
- **Polling for public repositories** that the App doesn't cover: every minute the agent reads the tracked branch's head and queues a deploy when it moved.
- **Missed webhooks redelivered at start.** GitHub doesn't redeliver a webhook the server missed, so when the agent starts it asks GitHub for the App's failed deliveries and has them sent again.
- **One deploy at a time per app, and a newer commit replaces a queued one.** An app holds at most one running and one queued deploy; a push, poll or Redeploy while one is queued updates it instead of adding another. Redeploy is no longer refused while a deploy runs. Builds stay one at a time per server.
- Each deploy records what triggered it: creation, Redeploy, push (with the pusher's login) or poll.
- **Disconnect** removes the App's credentials from `tero-keys`; apps from private repositories stop deploying with a message, public ones fall back to polling, and the dashboard links to the App's page on GitHub to delete it.
- `sudo tero rotate-key` re-encrypts the App's secrets. `sudo tero assume-breach` also stops everything running as `tero-agent`, deletes the GitHub connection, and prints the link to delete the App on GitHub.

## Capabilities

### New Capabilities

- `github-connection`: the manifest flow, installation, the repository list, installation tokens, signed webhooks, polling, redelivery at start and disconnecting.

### Modified Capabilities

- `process-isolation`: containers run under `tero-agent`, not `tero`; the agent is the only process that holds GitHub credentials.
- `server-init`: init creates `tero-agent` with the subordinate range and lingering, the shared directory, and the agent's unit for that user.
- `deployments`: deploy on push and poll, the per-app setting, the per-app queue with replacement, Redeploy while a deploy runs, and the trigger on each deploy.
- `apps`: create an app from a repository the App is installed on, private ones included; repositories are matched by GitHub's id, so a rename doesn't break the app.
- `key-service`: answers the agent by uid; holds the App's private key and webhook secret, signs App tokens for the agent and verifies webhook signatures for the dashboard.
- `breach-response`: `assume-breach` stops `tero-agent`'s processes, deletes the GitHub connection and says to delete the App on GitHub.
- `security-log`: events for connecting, disconnecting, installation changes and failed webhook signatures.

## Impact

- Init: the new `tero-agent` and `tero-shared` groups and user, `tero-keys-callers` for the key service's socket, `/var/lib/tero-agent` and `/var/lib/tero-shared`, the subordinate range and linger moved from `tero`, the agent's unit with `ConditionUser=tero-agent`, and `tero.service` without the Podman `InaccessiblePaths=`. Every path constant for the database, build logs and build directories changes.
- `tero-keys`' `0001_keys.sql` gains a `github_app` table and the connection states; its peer check gains the agent. `tero`'s `0001_apps.sql` gains the installation and its repositories, the repository id and "Deploy on push" on `apps`, the trigger on `deploys`, `github_requests`, handled delivery ids, and per-app queue indexes (one running, one queued). Migrations are edited in place: there are no installs to upgrade.
- New outbound calls from the agent to `api.github.com` on a fixed host. The dashboard makes no new outbound call.
- The webhook endpoint is the dashboard's first unauthenticated `POST` that isn't a login, so it gets its own body cap, throttle and replay check.
- No new Go dependency is expected: JWTs are RS256 over a fixed header, signed with the standard library, and the GitHub calls are a handful of plain HTTPS requests.
- Dashboard: a GitHub section in settings, the repository picker on the new-app page, "Deploy on push" on the app page, the trigger on each deploy, in English and Spanish, markup only.
- e2e: a fake GitHub compiled only into e2e builds (like Pebble), plus one manual run against real GitHub before the release. Slices 1 to 4's suites run again, since the agent's move touches them.
- `docs/security/asvs-l2.md`, `openspec/roadmap.md` (slice 6's open decision on the agent's identity is half settled here) and `openspec/config.yaml`'s context move to slice 5.
