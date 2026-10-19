## ADDED Requirements

### Requirement: Manual deploy and redeploy
A deploy SHALL start when an app is created and when the admin clicks Redeploy, which deploys the latest commit of the tracked branch.

#### Scenario: Redeploy picks up a new commit
- **WHEN** a new commit is pushed to the tracked branch and the admin clicks Redeploy
- **THEN** a new deploy of that commit starts, and once live the app serves the new code

### Requirement: Deploy states
Each deploy SHALL move through the states queued, building, starting, and then live or failed, and the dashboard SHALL show the current state without a page reload. A failed deploy SHALL show the step it failed at and a plain-language reason.

#### Scenario: Successful deploy
- **WHEN** a deploy's build succeeds and the new container passes its start check
- **THEN** the deploy is marked live and the previously live deploy is marked superseded

#### Scenario: Failed deploy
- **WHEN** a deploy fails at any step
- **THEN** it is marked failed with the step and reason, and the previously live deploy is still live and serving

#### Scenario: Interrupted deploy
- **WHEN** Tero restarts while a deploy is building or starting
- **THEN** that deploy is marked failed as interrupted, and no BuildKit or half-started container is left behind

### Requirement: Start check
A web app's new container SHALL pass a start check before it receives traffic: an HTTP request to `PORT` that gets any response within 60 seconds.

#### Scenario: App never listens
- **WHEN** a new container does not answer HTTP on `PORT` within 60 seconds
- **THEN** the deploy fails at the starting step, the container is removed, and the build log ends with the container's last output

#### Scenario: App exits on start
- **WHEN** a new container's process exits before passing the start check
- **THEN** the deploy fails at the starting step with the exit code and the container's last output in the log

### Requirement: Live release keeps serving during a redeploy
When the app's "Run old and new releases together during deploys" setting is on, Tero SHALL start a redeploy's container next to the live one, move the app's route to it only after it passes the start check, and only then stop the old container and remove its image.

#### Scenario: Failed redeploy
- **WHEN** a redeploy fails at any step
- **THEN** the app keeps answering from the release that was live before

### Requirement: Old and new releases together
Each app SHALL have a setting "Run old and new releases together during deploys", on by default. When it is off, Tero SHALL stop the live container after the build succeeds and before the new container starts, and if the deploy then fails, Tero SHALL start the previous release again from its image.

#### Scenario: Setting off, deploy succeeds
- **WHEN** the setting is off and a redeploy passes its start check
- **THEN** the old container was stopped before the new one started, and the new deploy is live

#### Scenario: Setting off, deploy fails
- **WHEN** the setting is off and a redeploy fails at the starting step
- **THEN** the previous release is started again and answers on the app's address, and the failed deploy shows its reason

### Requirement: One deploy at a time
Tero SHALL run one deploy at a time on the server, in the order they were queued, and SHALL refuse a Redeploy for an app that already has a deploy queued, building or starting.

#### Scenario: Redeploy while deploying
- **WHEN** an app has a deploy in progress
- **THEN** the Redeploy button is disabled, and a direct API call is refused with a message that a deploy is in progress

#### Scenario: Two apps deployed together
- **WHEN** the admin starts deploys on two apps
- **THEN** the second stays queued until the first ends, then runs
