## Context

An app's container gets only `PORT`, passed with `--env`. The dashboard (uid `tero`) and the agent (uid `tero-agent`) share `tero.db`, and either can write any row, so the agent re-validates every field. A compromised dashboard can already rewrite an app's repository and queue a deploy of it. `tero-keys` has no network, identifies callers by uid and cgroup, seals values with AES-256-GCM under the master key, and offers a confirmed session (password and TOTP code on this session in the last 5 minutes). The dashboard offers no way to change an app's repository, branch or folder; only a verified push refreshes the stored repository name after a rename.

The agent keeps live apps running: it resumes a stopped container, including after a reboot, and recreates it from the release's image if it is gone. Railpack's plans mount secrets by name, and Railpack reads `RAILPACK_*` variables as build configuration. All builds share one BuildKit cache volume, and cache mounts such as `~/.npm` are shared between apps because no cache key is set.

Measured on Ubuntu 26.04 with Podman 5.7 and Ubuntu 24.04 with Podman 4.9:

| How the value is passed | In clear on disk | Shown by `podman inspect` |
|---|---|---|
| `--env`, `--env-file` | `userdata/config.json`, `db.sql` | Yes |
| `--secret type=env` | `userdata/config.json` | Podman 4.9 yes, 5.7 no |
| `--secret type=mount` | `userdata/secrets/` | No |
| Bind-mounted file from tmpfs, read by the entry process | Nowhere | No |

## Goals / Non-Goals

**Goals:**
- A compromised dashboard can't read a sealed value, and can't get one by pointing an app at code it controls, by adding a variable, or by restarting an image it chose.
- A stolen disk or database copy holds no variable in clear, on a server with a TPM2.
- No value appears in an image, a build log, the app's last output in a deploy log, a command line or a security event.
- Applying a runtime-only change takes the time of a restart, and no save restarts or builds anything by itself.

**Non-Goals:**
- Protecting a value the admin types while the dashboard is compromised, or a sealed value from the app that receives it.
- Variables shared between apps, and references between variables.
- Rollback with a deploy's old variables (slice 7), and removing an app's values when it is deleted (slice 10).
- Masking runtime logs, which arrive with slice 9 and use the same masking.
- Visual design and wording, which stay markup only for Cele.

## Decisions

### One list, every value in `tero-keys`

- **A flag per variable, not separate Variables and Secrets lists.** The admin has one list per app, and each row has Sealed and where the variable is used. Railway works this way. Two lists would make the admin decide what kind of thing a value is before typing it, and an app needs one environment anyway.
- **Unsealed values are encrypted too.** `app_variables` in `tero-keys` holds the app's name, the variable's name, the key version, the sealed value, the sealed flag and the variable's scope. The additional data names the table, column, app, variable and key version, as for every other sealed value. Keeping unsealed values in `tero.db` would be simpler, but people put credentials in unsealed variables, and that database is readable by the dashboard with no session and by anything that copies it.
- **A scope per variable: `runtime`, `build` or `both`.** A new variable starts as `both`, the simplest default, and it avoids the common failure of a build that can't see a value. Sealing a variable switches it to `runtime` in the form, and the admin can change it back. A build runs the install scripts of every dependency with internet access, so a value the admin marked sensitive reaches a build only when they say so. Build only keeps a value such as a private registry token out of the running app. Tero can't work out the scope from the code, since only the app decides when it reads a variable. Changing a scope counts as a change to the build.
- **No per-app data key.** The additional data already ties each value to its app. A key per app would add wrapping and rotation code and protect against nothing `tero-keys` doesn't already prevent.
- **Limits.** Names match `^[A-Za-z_][A-Za-z0-9_]*$`, up to 256 bytes. Values reach 32 KiB with no NUL, multi-line values allowed. An app holds at most 200 variables and 256 KiB in all, well under Linux's limits for one process's environment. `PORT` is refused because Tero sets it. A `RAILPACK_*` variable can't be sealed, since Railpack can copy its value into the plan.

### Who can read and change what

- **Verbs.**
  - dashboard: `variables.list` (live session; values of unsealed variables, and only name, flags and times of sealed ones), `variables.save` (one atomic set of sets and removes), `variables.reads` (when a sealed variable was read), `source.approve` (confirmed session)
  - agent: `variables.release`
  - root: `variables.summary` (names, flags and approved sources, for `assume-breach`), plus `key.rotate` re-sealing them
- **Sealing is one way.** A sealed variable can be replaced or deleted, not unsealed or renamed. A save that names a sealed variable without a new value leaves it alone. Sealing, replacing and deleting need a confirmed session.
- **Any change on an app with a sealed variable needs a confirmed session,** including unsealed ones and a variable's scope. An unsealed variable can run code in the app before its first line (`NODE_OPTIONS`, `LD_PRELOAD`) or redirect a sealed credential to another host (`STRIPE_API_BASE`). A list of dangerous names can't cover the second kind, so the check covers every change. One check lasts 5 minutes, which covers several edits. An app with no sealed variable needs only a live session, since a stolen session could read its values anyway.
- **The `.env` paste is a way into the same form.** Tero parses comments, `export`, quotes and multi-line double-quoted values, and fills the form; nothing is stored until Save. A sealed name in the paste is reported and skipped, never replaced. Pasting adds and overwrites but doesn't remove. How this looks, and whether the text view can also delete rows, is Cele's call.

### What `tero-keys` releases, and for what

- **An approved source per app.** `app_sources` holds the repository URL, branch and folder. The first save of any variable records the app's current source, with the session that save requires. `variables.release` carries the source the agent is about to use, and `tero-keys` compares all three exactly. A mismatch records `variables_release_refused`, and the deploy fails before fetching anything: "This app's source changed. Approve it to deploy with its variables." The app page shows the old and new source and an Approve button that needs a confirmed session.
- **Exact URLs, not GitHub's repository id.** A row can pair an approved id with another repository's URL, and an installation token scoped to one repository still reads any public one, so the id proves nothing about what gets cloned. A renamed repository therefore needs one approval, which is rare and clear.
- **An app with no variables has no approved source,** deploys exactly as it does today and never calls `tero-keys`. The agent always asks, since `apps.has_variables` is in the shared database; the flag only lets an app with no variables deploy and start while the key service can't answer. The approved source goes with the app's last variable, and the app's first seal records it again, since sealing needs a confirmed session and an approval recorded on a live session alone mustn't carry over to sealed values.
- **Only the agent's images run.** A container starts only from an image named `localhost/tero/<app>:deploy-<n>`, with `--pull=never`, and its container name must be the one the agent makes for that app and deploy, since both come from the shared database. Otherwise a row could name an image from a registry whose label someone else chose, or a path outside the env directory.
- **The image remembers its source.** The agent writes `tero.source` into the image's configuration at build time. A restart, a reboot or a recreated container releases against the label on the image it is about to run, not the app's row. Otherwise a compromised dashboard could build its code under this app with the row pointed elsewhere, restore the row and queue a restart of that image, or write a `live` row naming another app's image. An old image from the approved source still passes; slice 7's rollback does the same on purpose. The Dockerfile path in slice 10 must set this label through the frontend so that a Dockerfile can't choose it.
- **Build and run are released separately.** `variables.release` takes a phase; `build` returns variables scoped `build` or `both` and `run` returns those scoped `runtime` or `both`. The build's release is called before the fetch, so a refused source does no work. Reads are recorded per sealed variable, so the list says which phase and deploy read it.

### Getting values to a container with nothing on disk

- **A file in tmpfs and an entry helper.** The agent writes the values to `/run/user/<uid>/tero-env/<container>` (directory 0700, owned by `tero-agent`; the file is 0644, because `--userns=auto` maps the app away from `tero-agent`'s uid and nobody else can reach the directory). The container gets that file and the static `tero` binary as read-only mounts, and `--entrypoint` runs the binary's `app-entry` subcommand. It reads the file, sets the environment and `execve`s the image's own entrypoint and command, which the agent read from `podman image inspect` and passes in the container's command. The app stays PID 1, and sees its variables exactly as with `--env`.
- **The alternative is simpler and writes every value to disk.** `--secret type=env` needs no helper, but leaves each value in clear in `config.json` under `tero-agent`'s home, where a disk snapshot or backup can read it, whatever the TPM protects. It would also let apps start after a reboot without `tero-keys`.
- **After a reboot.** tmpfs is empty, so the reconciler releases values and rewrites the file before resuming the container. While `tero-keys` can't answer or open a value, that app stays stopped and its page says why, and the reconciler keeps trying. Apps with no variables start as always. An app that merely exited still has its file and doesn't need `tero-keys`. A running app is never stopped by a key service problem.
- **The file is removed with its container,** and leftovers go when the agent starts. Tmpfs pages can reach swap on a server that has it, and Tero doesn't manage swap, so the docs say so.

### Restart deploys

- **Saving deploys nothing.** Each save records the changed variable names in `app_pending_changes` in `tero.db` (name, whether it touches a build, when), and queues nothing. Names aren't secret, and a compromised dashboard editing this table only changes what the page shows. The app list and app page say "x variables changed" with a button to redeploy. Railway stages changes the same way.
- **The button does the least work that applies them.** If every pending name is runtime only and the app has a live release, it queues a deploy with trigger `variables` and `needs_build = 0`, with a log, a state and the same safety: the new container starts next to the old one (or after it, per the app's setting), and a failure leaves the old release serving. Otherwise it queues a build. The normal Redeploy button still builds the branch head.
- **A deploy clears what it applied when it goes live.** Promoting a deploy removes the pending names recorded before it started, whatever triggered it, a push included. A failed deploy clears nothing, and a save during a deploy stays pending.
- **The agent skips the fetch and build** when `needs_build` is 0 and the app has a live release, copying its image, commit and message. With no live release it builds. `needs_build` is a hint a compromised dashboard can flip, and either value only changes how much work a deploy does.
- **Pending values can reach the app early.** A running app keeps the values it started with, but a reboot releases the current ones, so a pending value can take effect then. Holding old values back would mean keeping every old sealed value in `tero-keys`, which is what replacing one should end.
- **Other settings.** No app setting needs a redeploy today: the display name and "Deploy on push" apply at once, and running old and new releases together applies at the next deploy. `app_pending_changes` and the notice aren't specific to variables, so a later setting that needs a redeploy, such as a resource limit, adds its name there.
- **The queue merges upward.** `Enqueue` keeps `needs_build = 1` once any trigger on the queued row needed a build, so applying runtime-only changes can't cancel a queued push.
- **A shared image isn't removed with its container.** Retiring or discarding a release keeps an image that another deploy uses.

### Builds

- **Secret mounts only.** Variables scoped `build` or `both` go to `GenerateBuildPlan` through `app.NewEnvironment`, and to BuildKit through `secretsprovider.FromMap`, with `SecretsHash` so a changed value invalidates the steps that use it. A build script can still copy a value into its output; the form says so next to the scope, and an e2e check proves Tero itself adds none.
- **No build history.** BuildKit keeps every step's output in its cache volume on disk as build history, so a step that prints a value would leave it there. The daemon runs with `[history] maxEntries = 0`, which turns that off.
- **Per-app cache mounts.** `ConvertPlanOptions.CacheKey` is a hash of the app's name and source, so a build from another source can't plant what this app's builds read. Layers are content-addressed, so they stay shared and safe; shared `~/.npm`-style mounts would let any app's build plant a package that an app with secrets later installs.

### Masking

The agent wraps the deploy log in a writer that replaces known values with `[hidden]`, across chunk boundaries, for every sealed value and every unsealed value of 8 bytes or more (shorter ones, such as `true` or `8080`, would ruin the log). It covers the build log and the app's last output. It catches exact matches only, not encoded or split values, so it is a safety net, not a boundary.

### Events and the read list

`security_events` gains a `subject`. `tero-keys` records `variables_changed`, `variable_sealed` and `variable_replaced` (subject `<app>/<name>`), `source_approved` and `variables_release_refused` (subject the app, warning priority), and `sealed_variable_released` once per sealed variable per release, with the deploy and phase. Names are validated, so they can't forge journal lines. Values never appear. `variables.reads` lists the last 100 for one variable.

### Breach and rotation

`rotate-key` re-seals every value in the same transaction as the rest. `assume-breach` prints each app's variable names, marking sealed ones, for rotating at their providers, and each approved source, since an attacker with a confirmed session could have approved their own. It deletes no variable: apps need them to come back, and the admin replaces them one by one. Values whose master key is already lost are marked as lost first, so a lost key can't stop the response; `rotate-key` refuses while unmarked lost values remain, and works once they are marked. Its existing "redeploy every app" step then fetches each from source.

## Risks / Trade-offs

- [Unsealed values reach builds by default] → A dependency's install script can read an unsealed credential and send it out. The form says so next to the scope, and sealing the variable keeps it out of builds. The roadmap's open decision on outbound rules for builds would narrow this.
- [A value typed into a compromised dashboard is exposed] → The process handles it in clear on its way to `tero-keys`, and it serves the page, so encrypting in the browser wouldn't help. Sealing protects stored values, not ones entered during a compromise. The docs say so, and `assume-breach` says to rotate.
- [A confirmed check doesn't say what it approves] → A compromised dashboard can show one thing and do another during the 5 minutes after the admin confirms. This raises the bar from "compromised" to "compromised while the admin is confirming", and each change is recorded by `tero-keys` with its subject.
- [`tero-agent`'s helpers carry app traffic] → pasta or slirp4netns and rootlessport run under the uid that `tero-keys` now trusts with every value, which raises the stakes of slice 5's existing risk. Moving them to their own uid stays a later hardening, listed on the roadmap by this change.
- [Apps with variables need `tero-keys` after a reboot] → Narrows slice 3's "a lost key never takes apps offline": a running app is unaffected, but a cleared TPM followed by a reboot keeps those apps down until their variables are entered again. Chosen so that nothing is on disk.
- [An unreadable master key loses the values] → Sealed values can't be recovered without a recovery key, and unsealed ones can't either, since they share the key. The admin sets them again from their providers.
- [Restarts can be slow] → `--userns=auto` copies every layer before each start, about 6 minutes on a slow disk, until slice 7 fixes it. The deploy shows as starting, and the old release keeps serving.
- [The helper mounts two host files into each app with variables] → Both are read-only and hold nothing beyond that app's own values and a public binary. `docs/security/sp800-190.md` 4.4.3's "no host folders" gains this exception.
- [Old code from the approved source still gets the values] → A compromised dashboard could restart an older image, including a vulnerable one. Only images still kept can be chosen, and slice 7's image retention limits those.

## Open Questions

None. Lean settled the design's questions on 2026-11-05 and 2026-11-07: one list with one-way seals, a scope of runtime only, build only or both per variable, starting as both and switching to runtime only when sealed, each app pinned to an approved source, a fresh check for every change on an app with a sealed variable, saves that deploy nothing until the admin redeploys, values in memory only, and the recovery key and `sudo tero restore` in a slice of their own.
