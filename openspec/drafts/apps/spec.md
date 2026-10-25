# apps Specification

## Purpose
What later slices add to apps: workers (slice 10), which receive no traffic, environment variables (slice 6), and deleting an app (slice 10). Web apps from public GitHub URLs are in the slice 2 change.

## Requirements

### Requirement: Create an app from a repository
The admin SHALL be able to create an app from a repository connected through the GitHub App (slice 5) as well as from a public GitHub URL, and SHALL choose the app type: web or worker.

#### Scenario: Create from a connected repository
- **WHEN** the admin creates an app from a private repository the GitHub App can read
- **THEN** the app is listed in the dashboard and its first deploy starts

#### Scenario: Create a worker
- **WHEN** the admin creates a worker app
- **THEN** the app is listed with type worker and its first deploy starts

### Requirement: Worker apps
A worker app SHALL run as a long-lived container with no domain and no inbound traffic from the proxy.

#### Scenario: Worker has no route
- **WHEN** a worker is running
- **THEN** the proxy has no route to it and its container publishes no port

#### Scenario: Worker crashes
- **WHEN** a worker's process exits
- **THEN** Tero restarts its container

### Requirement: Environment variables
The admin SHALL be able to set, change and remove environment variables per app. Changes SHALL take effect on the next deploy, and the dashboard SHALL offer to redeploy after a change.

#### Scenario: Variable available at runtime
- **WHEN** the admin sets `FOO=bar` on an app and redeploys
- **THEN** the app's process sees `FOO` with value `bar`

#### Scenario: Variable removed
- **WHEN** the admin removes a variable and redeploys
- **THEN** the app's process no longer sees it

#### Scenario: Variables isolated per app
- **WHEN** two apps exist and a variable is set on one
- **THEN** the other app does not see it

### Requirement: Sealed variables
The admin SHALL be able to seal a variable when creating it or later. Sealing SHALL be one way: a sealed variable SHALL never be unsealed and its value SHALL never be shown again. A sealed variable's value SHALL be held only by `tero-keys`, encrypted under the master key, and no dashboard endpoint and no `tero-keys` verb the dashboard can call SHALL return it. For each sealed variable the dashboard SHALL show its name, a "Sealed" label and when it was last replaced, and SHALL offer only two actions: Replace, which takes a new value without showing the old one, and Delete. A sealed variable's name SHALL NOT be changed. Sealed variables SHALL NOT be editable from the plain-text editor; only the field view can replace them. Sealing a variable and replacing a sealed value SHALL each require a fresh TOTP code. Unsealed variables are not secrets: the dashboard stores and shows them.

#### Scenario: Sealed value never returned
- **WHEN** the admin seals `API_KEY` on an app and then calls every dashboard endpoint that lists or shows the app's variables
- **THEN** the responses contain the name `API_KEY`, its sealed state and its last replacement time, and never its value

#### Scenario: Sealed variable available at runtime
- **WHEN** the admin seals `API_KEY` on an app and redeploys
- **THEN** the app's process sees `API_KEY` with the value the admin entered

#### Scenario: Replace a sealed variable
- **WHEN** the admin replaces the value of a sealed variable with a valid TOTP code and redeploys
- **THEN** the app's process sees the new value, and the dashboard shows the new replacement time and never the old or new value

#### Scenario: No way back
- **WHEN** the admin opens a sealed variable
- **THEN** the dashboard offers only Replace and Delete, and no action that shows the value, unseals it or renames it

#### Scenario: Not editable as plain text
- **WHEN** the admin opens the app's variables in the plain-text editor
- **THEN** sealed variables are not in the editable text, and saving the text leaves them unchanged

#### Scenario: Fresh TOTP required
- **WHEN** the admin seals a variable or replaces a sealed value without a valid TOTP code
- **THEN** the change is refused and the stored variable is unchanged

### Requirement: Delete an app
The admin SHALL be able to delete an app, which stops its container and removes its route, images and stored settings.

#### Scenario: Delete a web app
- **WHEN** the admin deletes a web app and confirms
- **THEN** its container is gone, its domain no longer routes to it, and it is no longer listed
