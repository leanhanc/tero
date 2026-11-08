# apps Specification

## Purpose
What later slices add to apps: workers (slice 10), which receive no traffic, and deleting an app (slice 10), which also removes its variables and approved source from the key service. Web apps from public GitHub URLs are in the slice 2 change.

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

### Requirement: Delete an app
The admin SHALL be able to delete an app, which stops its container and removes its route, images and stored settings.

#### Scenario: Delete a web app
- **WHEN** the admin deletes a web app and confirms
- **THEN** its container is gone, its domain no longer routes to it, and it is no longer listed
