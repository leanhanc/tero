## Why

Almost every real app needs configuration and credentials, and Tero can't give it any: an app sees only `PORT`. Apps can't reach a database or an API until they have variables, which makes this the next thing v1 needs after deploy on push.

Variables are also the most valuable thing on the server, and the failure behind slice 3 applies to them directly: if the internet-facing dashboard can read an app's secrets, breaking into the dashboard gives away every database and API key. Tero's main difference from Coolify and Dokploy is that it doesn't work that way.

## What Changes

- **Variables per app.** Set, change and remove them from the app page, as name and value fields or by pasting a `.env` file. Each save is one atomic change.
- **One list, with a per-variable "Sealed" option.** Sealed variables, as on Railway, are write-only once saved: name, label and last replacement only, with Replace and Delete. Sealing can't be undone. It is optional; the default is an unsealed variable the admin can read back.
- **Every value is encrypted in `tero-keys`,** under the master key. Unsealed values reach the dashboard only for a live session; sealed values never do. Nothing is in the shared database.
- **Each variable says where it is used: runtime only, build only, or both.** New variables start as both. Sealing one switches it to runtime only, and the admin can change either.
- **Saving never restarts or rebuilds anything.** The app list and the app page say how many variables changed, with a button to redeploy. If only runtime-only variables changed, that restarts the live image with no build; otherwise it builds. Any deploy that goes live, a push included, clears the notice. A failed deploy leaves the previous release serving and the notice in place.
- **`tero-keys` releases an app's values only to the agent, and only for the app's approved source** (repository, branch, folder), which it holds itself. Any other source fails the deploy before it fetches anything, until the admin approves it with a fresh check.
- **On an app with a sealed variable, every variable change needs a fresh check,** because an unsealed `NODE_OPTIONS` could read the sealed ones from inside the app.
- **Values live only in memory on the host.** They reach a container through a file in tmpfs and a small entry helper, so nothing is written to Podman's storage on disk. After a reboot, apps with variables start once `tero-keys` answers.
- **Values stay out of images and logs.** Build values use BuildKit secret mounts, and known values are masked in build logs and in the app's last output.
- **A list of when each sealed variable was read,** from the security log.
- `sudo tero rotate-key` re-encrypts the values. `sudo tero assume-breach` lists each app's variable names to rotate at their providers, and its approved source to check.
- Each app gets its own build cache mounts, so one app's build can't poison what another's reads.
- The recovery key and `sudo tero restore` move to a slice of their own, since restoring also needs a backup of the apps' database, which nothing makes yet.

## Capabilities

### New Capabilities

- `app-variables`: setting, sealing, replacing and removing variables, the `.env` paste, restart or rebuild on change, the source approval, and the read list.

### Modified Capabilities

- `key-service`: holds the values and approved sources, and answers the release verb only for the agent.
- `deployments`: deploys that restart the live image with no build, the new trigger, and a source refusal as a deploy failure.
- `build-pipeline`: build values as secret mounts, masked logs, and per-app cache mounts.
- `process-isolation`: how values reach a container, with nothing on disk; the entry helper and its read-only mounts.
- `breach-response`: `assume-breach` lists variable names and approved sources.
- `security-log`: events for changes and refused releases, and a subject on events.

## Impact

- `tero-keys`: `0001_keys.sql` gains `app_variables` and `app_sources`, and `security_events` a subject. New verbs for the dashboard, the agent and root. `key.rotate` re-seals the values.
- `tero`'s `0001_apps.sql`: `variables` as a trigger and `needs_build` on `deploys`, and `app_pending_changes`.
- Agent: the release call in the pipeline and the reconciler, restart deploys, an image label naming the source, the env file in tmpfs and masking in logs.
- `containers.Start` gains the entry helper and takes the image's entrypoint from `podman image inspect`. The `tero` binary gains a hidden subcommand for it.
- Dashboard: a variables section on the app page, in English and Spanish, markup only.
- e2e: a fixture that reports its environment, and searches of the disk, images and logs for known values, on both tested releases and architectures.
- `docs/security/asvs-l2.md`, `docs/security/sp800-190.md`, `openspec/roadmap.md`, `openspec/config.yaml`, `README.md` and `openspec/drafts/apps/spec.md`.
