## ADDED Requirements

### Requirement: Variables per app
The admin SHALL be able to set, change and remove environment variables per app from the app page, as one list. A save SHALL apply all its changes or none. A name SHALL match `^[A-Za-z_][A-Za-z0-9_]*$` and be at most 256 bytes. A value SHALL be at most 32 KiB with no NUL byte, and MAY span several lines. An app SHALL hold at most 200 variables and 256 KiB of values. `PORT` SHALL be refused, since Tero sets it.

#### Scenario: Variable available at runtime
- **WHEN** the admin sets `FOO=bar` on a live app and redeploys
- **THEN** the app's process sees `FOO` with value `bar` once the new release is live

#### Scenario: Variable removed
- **WHEN** the admin removes a variable from a live app and redeploys
- **THEN** the app's process no longer sees it once the new release is live

#### Scenario: Multi-line value
- **WHEN** the admin saves a value holding a PEM key
- **THEN** the app's process sees it byte for byte

#### Scenario: Variables isolated per app
- **WHEN** two apps exist and a variable is set on one
- **THEN** the other app's process doesn't see it

#### Scenario: Invalid save changes nothing
- **WHEN** a save holds one valid variable and one named `PORT` or `1BAD`
- **THEN** the save is refused and no variable changes

### Requirement: Where a variable is used
Each variable SHALL have a scope: runtime only, build only, or both. A new variable SHALL start as both. Sealing a variable SHALL switch it to runtime only in the form. The admin SHALL be able to change the scope of any variable.

#### Scenario: Runtime only
- **WHEN** a variable is runtime only and the app deploys
- **THEN** the running app sees it and build steps don't

#### Scenario: Build only
- **WHEN** a variable is build only and the app deploys
- **THEN** build steps see it and the running app doesn't

#### Scenario: Both
- **WHEN** a variable's scope is both and the app deploys
- **THEN** build steps and the running app see it

#### Scenario: New variable
- **WHEN** the admin adds a variable and changes nothing else
- **THEN** its scope is both

#### Scenario: Sealing switches the scope
- **WHEN** the admin seals a variable in the form
- **THEN** its scope becomes runtime only, and the admin can change it before saving

### Requirement: Paste a .env file
The admin SHALL be able to paste the contents of a `.env` file into the variables form. Comments, `export` prefixes, quotes and multi-line double-quoted values SHALL be understood. Pasting SHALL add and overwrite rows in the form, remove none, and store nothing until the admin saves. A line naming a sealed variable SHALL be reported and skipped.

#### Scenario: Paste then save
- **WHEN** the admin pastes `export A="1"`, a comment and `B=2`, then saves
- **THEN** the app has `A=1` and `B=2`

#### Scenario: Paste isn't saved by itself
- **WHEN** the admin pastes a file and leaves without saving
- **THEN** the app's variables are unchanged

#### Scenario: Paste names a sealed variable
- **WHEN** the admin pastes a line naming a sealed variable
- **THEN** the form reports it, and saving leaves the sealed value unchanged

### Requirement: Sealed variables
The admin SHALL be able to seal a variable when creating it or later. Sealing SHALL be one way. No dashboard endpoint and no key service verb the dashboard can call SHALL return a sealed value. For a sealed variable the dashboard SHALL show its name, a "Sealed" label and when it was last replaced, and SHALL offer only Replace and Delete. A `RAILPACK_*` variable SHALL NOT be sealable.

#### Scenario: Sealed value never returned
- **WHEN** the admin seals `API_KEY` and then calls every dashboard endpoint that lists or shows the app's variables
- **THEN** the responses hold its name, sealed state and last replacement time, and never its value

#### Scenario: Sealed variable available at runtime
- **WHEN** the admin seals `API_KEY` on a live app and redeploys
- **THEN** the app's process sees it with the value entered once the new release is live

#### Scenario: Replace a sealed variable
- **WHEN** the admin replaces a sealed value and redeploys
- **THEN** the app's process sees the new value once the new release is live, and the dashboard shows the new replacement time and neither value

#### Scenario: No way back
- **WHEN** the admin opens a sealed variable, or sends a save that unseals or renames it
- **THEN** the dashboard offers only Replace and Delete, and the save is refused

### Requirement: Fresh check on apps with sealed variables
Sealing a variable, and any variable change on an app that has a sealed variable, SHALL need a session confirmed with the password and a TOTP code in the last 5 minutes. On an app with no sealed variable, a save SHALL need a live session. A confirmation expiring SHALL NOT end the session.

#### Scenario: Unconfirmed seal
- **WHEN** a save seals a variable on a session not confirmed in the last 5 minutes
- **THEN** it is refused, nothing changes, and the dashboard asks for the password and a code

#### Scenario: Unsealed change on a sealed app
- **WHEN** a save adds `NODE_OPTIONS` on an app with a sealed variable, on an unconfirmed session
- **THEN** it is refused and nothing changes

#### Scenario: Several saves after one check
- **WHEN** the admin confirms and saves twice within 5 minutes
- **THEN** the second save doesn't ask again

#### Scenario: Confirmation expires
- **WHEN** more than 5 minutes pass after confirming
- **THEN** the admin stays logged in, and the next save that needs a check asks for it

#### Scenario: App with no sealed variable
- **WHEN** a save changes an unsealed variable on an app with no sealed variable, on a live unconfirmed session
- **THEN** it is accepted

### Requirement: Changes wait for a redeploy
A save SHALL never restart or build anything. The app list and the app page SHALL show how many variables changed since the last live deploy, and the app page SHALL offer to redeploy. If every changed variable is runtime only and the app has a live release, that redeploy SHALL restart the live image with no build; otherwise it SHALL build. Any deploy that goes live SHALL clear the changes recorded before it started. A deploy that fails SHALL leave the previous release serving and the changes pending.

#### Scenario: Save deploys nothing
- **WHEN** the admin saves a change to any variable on a live app
- **THEN** no deploy is queued, the app keeps answering with its old values, and the app list and page show the number of changed variables

#### Scenario: Several saves counted once per variable
- **WHEN** the admin saves `A` twice and `B` once
- **THEN** the page shows 2 variables changed

#### Scenario: Runtime variable changed
- **WHEN** the admin redeploys after changing only runtime-only variables on a live app
- **THEN** a deploy with trigger `variables` runs, its log shows no fetch or build, it uses the live release's image and commit, and the notice clears once it is live

#### Scenario: Build variable changed
- **WHEN** the admin redeploys after changing a build-only variable, a variable whose scope is both, or a variable's scope
- **THEN** the deploy fetches the branch head and builds with the current values, and the notice clears once it is live

#### Scenario: Push applies the changes
- **WHEN** a push deploys the app while variables are pending
- **THEN** the deploy uses the current values, and the notice clears once it is live

#### Scenario: New values break the app
- **WHEN** a saved value makes the app exit at start after the admin redeploys
- **THEN** the deploy fails, the previous release keeps answering with its old values, and the notice stays

### Requirement: Approved source
The first save of any variable on an app SHALL record the app's repository URL, branch and folder in the key service as its approved source. The app's first seal SHALL record its current source again, and removing its last variable SHALL remove its approved source. Values SHALL be released only for that exact source. Any other source SHALL fail the deploy before it fetches anything, with a reason that says the source changed. The app page SHALL show both sources and offer an approval that needs a session confirmed in the last 5 minutes.

#### Scenario: Rewritten source refused
- **WHEN** an app with variables has its repository, branch or folder changed in the database and a deploy is queued
- **THEN** the deploy fails before fetching, the app's process never gets the values, and `variables_release_refused` is recorded

#### Scenario: Approving a new source
- **WHEN** the admin approves the shown source on a confirmed session and redeploys
- **THEN** the deploy succeeds with the app's variables

#### Scenario: Approval needs a check
- **WHEN** the dashboard approves a source on an unconfirmed session
- **THEN** it is refused

#### Scenario: Image from another source
- **WHEN** a restart or a live row names an image built from a source other than the approved one
- **THEN** no values are released for it and its container doesn't get them

#### Scenario: App with no variables
- **WHEN** an app has no variables
- **THEN** it deploys with no approved source, also while the key service is stopped

#### Scenario: Flag turned off in the database
- **WHEN** an app with variables is marked in the database as having none and a deploy is queued
- **THEN** the agent still asks the key service, which still checks the source

### Requirement: Read list
The app page SHALL show, for each sealed variable, the last 100 times it was released, each with the deploy and whether it was for a build or a run. Reading the list SHALL need a live session.

#### Scenario: Release shown
- **WHEN** an app with a sealed variable deploys
- **THEN** that variable's list shows the deploy, with the time

#### Scenario: Reboot shown
- **WHEN** the server reboots and the app starts again
- **THEN** the list gains an entry for the run
