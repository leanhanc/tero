# deployments Specification

## Purpose
What later slices add to deploys: deploys on push (slice 4), no dropped requests during a switch, history and one-click rollback (slice 6). Manual deploys, deploy states and the start check are in the slice 2 change.

## Requirements

### Requirement: Deploy on push
Tero SHALL start a deploy whenever the app's tracked branch gets a new commit, through a signed webhook or, for public repositories, polling (see `github-connection`).

#### Scenario: Push deploys
- **WHEN** a commit is pushed to an app's tracked branch
- **THEN** a deploy for that commit appears in the app's deploy list and starts building

### Requirement: Old release keeps serving until the new one is ready
For a web app, the proxy SHALL switch traffic to the new container only after it passes its start check, and only then stop the old container.

#### Scenario: Requests during a deploy
- **WHEN** a web app is being redeployed and clients send requests throughout
- **THEN** every request is answered by either the old or the new release

### Requirement: One deploy at a time per app
Tero SHALL run at most one deploy per app at a time. A newer commit SHALL replace any deploy still queued for that app.

#### Scenario: Two quick pushes
- **WHEN** two commits are pushed to an app while a deploy is building
- **THEN** the running deploy finishes, and only the newest commit is deployed after it

### Requirement: Deploy history
The dashboard SHALL list each app's deploys with commit, message, trigger, state, start time and duration.

#### Scenario: View history
- **WHEN** the admin opens an app
- **THEN** its deploys are listed newest first with those fields

### Requirement: Rollback
The admin SHALL be able to roll back to any earlier deploy whose image is still kept, without rebuilding. Rollback uses the environment variables that deploy ran with.

#### Scenario: Roll back to a previous release
- **WHEN** the admin picks an earlier successful deploy and chooses rollback
- **THEN** that deploy's image is started, traffic moves to it once it passes its start check, and it is marked live

#### Scenario: Image no longer kept
- **WHEN** an earlier deploy's image has been pruned
- **THEN** the dashboard does not offer rollback to it

### Requirement: Image retention
Tero SHALL keep the images of the most recent successful deploys of each app for rollback and prune older ones.

#### Scenario: Old images pruned
- **WHEN** an app has more successful deploys than the retention count
- **THEN** images beyond the retention count are removed from `tero`'s Podman store, except the live one
