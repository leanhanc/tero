Implements all of slice 6: the new capability `app-variables` and this change's deltas to `key-service`, `deployments`, `build-pipeline`, `process-isolation`, `breach-response` and `security-log`. Each task names the spec scenarios it makes pass, written as `capability: Scenario`. Parts 2 to 7 are covered by Go and web unit tests; part 8 runs in fresh Lima VMs (Ubuntu 24.04 and 26.04, arm64) on the Mac and on amd64 (the Ubuntu PC, or EC2 through `scripts/ec2-test.sh` after asking).

## 1. Prove values can stay off the disk

- [x] 1.1 Repeat the Podman measurement on Ubuntu 24.04 (Podman 4.9): `--env`, `--secret`, and a tmpfs file bind-mounted under `--userns=auto`. Verified by a search of Podman's storage finding the value only in the first two
- [x] 1.2 Try the entry helper on both releases with a Railpack image with a non-root user and an image with no shell: read the file, `execve` the image's entrypoint and command. Verified by PID 1 being the app's own process and a stop signal reaching it. If this fails, stop and bring it to Lean, since decision 4 rests on it

## 2. Values in the key service

- [x] 2.1 Add `app_variables`, `app_sources` and `security_events.subject` to `0001_keys.sql`, with sealing tied to app and name, and re-seal the values in `key.rotate`. Covers `key-service: Value moved to another row`, `Rotation re-seals values`
- [x] 2.2 Add `variables.list`, `variables.save` with the limits, name rules and scope, and the rule for when a save needs a confirmed session. Covers `app-variables: Invalid save changes nothing`, every `Fresh check on apps with sealed variables` scenario that applies at this layer, `No way back`, `key-service: Dashboard asks without a session`
- [x] 2.3 Add the agent's `variables.release` by phase with the exact source comparison, `source.approve`, `variables.reads` and root's `variables.summary`. Covers `key-service: Dashboard asks for a release`, `Build release`, `Root asks for values`, `app-variables: Approval needs a check`
- [x] 2.4 Add the variable events and the journal fields, none appendable by a caller. Covers every `security-log` scenario in this change that a unit test can reach

## 3. Getting values to a container

- [x] 3.1 Add the `app-entry` subcommand and an env file format that keeps multi-line values byte for byte. Covers `app-variables: Multi-line value`
- [x] 3.2 Make `containers.Start` write the tmpfs file, mount it and the binary read-only, set `--entrypoint` from `podman image inspect`, and remove the file with the container and leftovers at agent start. An app with no variables starts as today. Covers every `process-isolation` scenario in this change

## 4. Deploys

- [x] 4.1 Add `variables` to `trigger_type`, `needs_build` to `deploys` and the `app_pending_changes` table, clear the names recorded before a deploy started when it goes live, and make `Enqueue` keep `needs_build` once set. Covers `deployments: Variable change while a push is queued`, `Variable change while building`
- [x] 4.2 Skip the fetch and build for a restart with a live release, copying its image, commit and message, and keep a shared image when retiring or discarding. Covers `app-variables: Runtime variable changed`, `New values break the app`, `deployments: Old release removed, image kept`, `Failed restart keeps the image`
- [x] 4.3 Call the release before the fetch and before the start, failing the deploy with an actionable reason on a refusal. Covers `app-variables: Rewritten source refused`, `App with no variables`
- [x] 4.4 Write `tero.source` into the image configuration at build time, and release against the image's label for restarts and the reconciler. Covers `app-variables: Image from another source`
- [x] 4.5 Make the reconciler release values before resuming when the file is gone, keep trying, and record why on the app. Covers every `deployments: Apps with variables after a reboot` scenario

## 5. Builds and logs

- [x] 5.1 Pass build-scoped values to Railpack's environment and BuildKit's secrets with `SecretsHash`, and set `CacheKey` to the app's name. Covers every `build-pipeline: Values kept out of images` scenario, `Planted package`, `Runtime only`, `Build only`, `Both`
- [x] 5.2 Add the masking writer around the deploy log, handling values across writes, and apply it to the app's last output. Covers every `build-pipeline: Values masked in deploy logs` scenario

## 6. Dashboard API

- [x] 6.1 Add `GET` and `POST /api/apps/{name}/variables`, the approval route and the reads route, with a body cap, record each save's names as pending and queue nothing, and add the redeploy route that chooses restart or build. Covers `app-variables: Save deploys nothing`, `Several saves counted once per variable`, `Build variable changed`, `Push applies the changes`, `Sealed value never returned`
- [x] 6.2 Add the `.env` parser in TypeScript beside the form, so a pasted file stays in the browser until Save, with tests for comments, `export`, quotes and multi-line values. Covers every `app-variables: Paste a .env file` scenario

## 7. Dashboard page and breach

- [x] 7.1 Move the confirm form out of the settings feature into `web/src/components`, keeping settings' behaviour. Verified by the existing settings tests
- [x] 7.2 Add the variables section to the app page in English and Spanish, markup only: rows, Sealed and the scope with its defaults, Replace and Delete, paste, the source approval and the read list. Covers `app-variables: Several saves after one check`, `Confirmation expires`, `Unconfirmed seal`, `Replace a sealed variable`, `New variable`, `Sealing switches the scope`. List the wording for Cele, who decides how the page makes the scopes, their defaults and the pending changes clear
- [x] 7.3 Show the number of changed variables on the app list and app page, with the redeploy button on the page, `variables` as a trigger on the deploy list, and the reason on an app waiting for `tero-keys`
- [x] 7.4 Make `assume-breach` print names and approved sources through `variables.summary`. Covers every `breach-response` scenario in this change

## 8. End to end and docs

- [x] 8.1 Add an e2e fixture that reports its environment at an HTTP path, and e2e tests for the scenarios above, including searches of the disk, the saved image, the shared directory and the journal for known values
- [ ] 8.2 Run the whole e2e suite, slices 1 to 5 included, on both releases and both architectures. Verified by a green run on each. Done on arm64 for 26.04 and 24.04; amd64 still to run
- [x] 8.3 Update `docs/security/asvs-l2.md` (v5.0.0-13.3.1, 13.3.2, 14.2.4, 16.2.5, the cryptographic inventory, Sensitive data), `docs/security/sp800-190.md` (4.1.4, and 4.4.3's mounts), `openspec/config.yaml`'s context, `README.md` and `openspec/drafts/apps/spec.md`, stating the limits: a value typed into a compromised dashboard is exposed, tmpfs can reach swap, and masking catches exact matches only
- [x] 8.4 Document for admins how variables work: the three scopes and their defaults, that sealing switches to runtime only and that unsealed values reach builds by default, which runs dependencies' install scripts, and that saving deploys nothing until a redeploy. Verified by the page existing and linked from `README.md`
- [x] 8.5 Update `openspec/roadmap.md`: link this change under slice 6, reword its "a change restarts" item to a redeploy the admin starts, tick slice 5's items, move the recovery key and `sudo tero restore` to a slice of their own, and add running Podman's helpers under their own uid
- [x] 8.7 Fix the adversarial review's findings, each with a unit test:
  - The agent always asks `tero-keys`, using `has_variables` only when it can't answer, so turning the flag off can't skip the source check or start an app without values; update the "App with no variables" scenario
  - Validate `deploys.container` before any file or podman use (`container == containers.Name(app, deployID)`), and don't treat an empty name as a file
  - Delete the approved source when an app's last variable goes, re-pin it on the app's first seal, refuse approval with no variables
  - Record variable events inside the save's transaction
  - Masker: scan held text at Close with no hold-back; also mask each line of 8 bytes or more of a multi-line value
  - A restart deploy clears only `touches_build = 0` pending rows; a deploy that built despite `needs_build = 0` counts as a build
  - `IsUnreadable` for lost sealed values; fix design.md's wording on `rotate-key` after values are marked lost
  - A restart builds when the live image's label differs from the app's source; remove the container and env file after a failed reconciler start; `Remove` skips an empty container
  - Refuse sealing `MISE_*` and `TURBO_CACHE_DIR`; `.env` parser: `A= # note` is empty, and text after a closing quote is an invalid line
- [x] 8.8 e2e fixes: `testSaveVariables` assumes the session is unconfirmed, but it was confirmed less than 5 minutes earlier, so expire or skip that check; then rerun on Ubuntu 26.04 and 24.04 and ask Lean to run amd64
- [x] 8.6 Have a separate agent with minimal context review the change adversarially for correctness and security, against ASVS 5.0 L2 and SP 800-190, and fix what it confirms
