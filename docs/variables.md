# App variables

Each app has one list of environment variables, on its page in the dashboard. Add them one by one, or paste a `.env` file to fill the form. Comments, `export`, quotes and multi-line quoted values are understood. Pasting adds and overwrites rows and removes none, and nothing is stored until you save.

`PORT` is set by Tero and can't be changed. A name uses letters, digits and underscores and can't start with a digit. A value holds up to 32 KiB, and an app up to 200 variables and 256 KiB.

## Saving doesn't deploy

Saving stores your changes and leaves the app running as it is. The app list and page then say how many variables changed, and the page offers to redeploy. Redeploying restarts the app with no build when only runtime-only variables changed, and builds the branch's latest commit otherwise. A push that deploys also applies them. If a deploy fails, the previous release keeps serving with its old values, and the notice stays.

A server reboot before you redeploy gives the app the saved values, because old values aren't kept.

## Where a variable is used

| Choice | The build sees it | The running app sees it | Applying a change |
|---|---|---|---|
| Build and runtime, the default | Yes | Yes | Builds |
| Runtime only, the default for a sealed variable | No | Yes | Restarts, with no build |
| Build only | Yes | No | Builds |

Tero can't work this out from your code, since only your app decides when it reads a variable. Values a framework bakes in at build time, such as `NEXT_PUBLIC_*`, need the build. Something only the build uses, such as a private registry token, is best as build only.

A build runs the install scripts of every dependency, with internet access, so **an unsealed value reaches those scripts by default**. Seal credentials, or set them to runtime only.

## Sealing

Seal a variable when the dashboard should never show it again, such as an API key or a database password. Once saved:

- Nobody can read its value in the dashboard, including you. You can only replace or remove it.
- Sealing can't be undone, and a sealed variable can't be renamed.
- It switches to runtime only when you seal it. You can still choose otherwise.
- The page lists each time it was handed to a deploy.
- Any change to any variable on that app asks for your password and a code, because an unsealed `NODE_OPTIONS` or API address could make the app give a sealed value away. One check lasts 5 minutes.

Sealing protects values the dashboard has stored. A value typed in while the dashboard is compromised can still be read on its way through, and your app can always read its own values. A `RAILPACK_*` variable can't be sealed, since Railpack may copy it into the build plan.

## Where values live

Every value is encrypted by the key service under the master key. The shared database holds only variable names that are waiting for a redeploy. On the server, values are only in memory while an app runs, never in Podman's storage on disk. After a reboot, an app with variables starts once the key service answers, and its page says so while it waits. Apps with no variables start regardless, and a running app is never stopped by a key service problem.

Tero hides known values in deploy logs, for every sealed value and every unsealed value of 8 bytes or more. This catches exact matches only, so don't rely on it to print secrets safely.

## Approved source

The first time you save a variable, Tero records the app's repository, branch and folder in the key service. Values are released only for that exact source. If it changes, for example when the repository is renamed on GitHub, deploys fail before fetching anything and the page shows both sources, with a button to approve the new one using your password and a code. If you didn't change it, don't approve it: someone may have tampered with the app.

## After a suspected breach

`sudo tero assume-breach` keeps your variables, so apps can come back, and lists each app's variable names to rotate at their providers and its approved source to check. Replace each value in the dashboard once you've rotated it.

If the master key is lost, for example because the TPM was cleared, stored values can't be recovered. Set them again from their providers.
