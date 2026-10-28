## MODIFIED Requirements

### Requirement: Manual deploy and redeploy
A deploy SHALL be queued when an app is created, when the admin clicks Redeploy, and, while the app's "Deploy on push" setting is on (the default), when a signed webhook or polling finds the tracked branch moved. Every deploy SHALL build the head of the tracked branch at the moment the agent fetches it, and SHALL record its trigger (`create`, `manual`, `push` with the pusher's login, or `poll`).

#### Scenario: Redeploy picks up a new commit
- **WHEN** a new commit is pushed to the tracked branch and the admin clicks Redeploy
- **THEN** a new deploy of that commit starts, and once live the app serves the new code

#### Scenario: Push deploys
- **WHEN** a commit is pushed to an app's tracked branch
- **THEN** a deploy for that commit appears in the app's deploy list with trigger `push` and the pusher's login, and once live the app serves the new code

#### Scenario: Deploy on push turned off
- **WHEN** the admin turns off "Deploy on push" for an app and a commit is pushed to its tracked branch
- **THEN** no deploy is queued, and Redeploy deploys that commit

### Requirement: One deploy at a time
Tero SHALL run one build at a time on the server, oldest queued first. Each app SHALL have at most one deploy building or starting and at most one queued. A trigger while the app has a queued deploy SHALL update that deploy instead of adding another; a trigger while a deploy is building or starting SHALL queue one behind it.

#### Scenario: Two quick pushes
- **WHEN** two commits are pushed to an app while a deploy is building
- **THEN** the running deploy finishes, and one more deploy runs after it and builds the newest commit

#### Scenario: Redeploy while deploying
- **WHEN** the admin clicks Redeploy while the app's deploy is building
- **THEN** a deploy is queued behind it and the button shows "Queued"

#### Scenario: Redeploy while one is queued
- **WHEN** the admin clicks Redeploy while the app already has a queued deploy
- **THEN** no second deploy is queued and the queued one's trigger becomes `manual`

#### Scenario: Two apps deployed together
- **WHEN** deploys are queued for two apps
- **THEN** the second stays queued until the first ends, then runs
