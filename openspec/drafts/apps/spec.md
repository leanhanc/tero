# apps Specification

## Purpose
An app is one repository and branch deployed as one long-running container on the server. v1 has two kinds: web apps, which receive HTTP traffic through the built-in proxy, and workers, which receive none. Each app has its own environment variables.

## Requirements

### Requirement: Create an app from a repository
The admin SHALL be able to create an app from a repository connected through the GitHub App or from a public GitHub URL, choosing the branch to track and the app type.

#### Scenario: Create a web app
- **WHEN** the admin creates a web app from a repository and branch
- **THEN** the app is listed in the dashboard and its first deploy starts

#### Scenario: Create a worker
- **WHEN** the admin creates a worker app
- **THEN** the app is listed with type worker and its first deploy starts

### Requirement: Web apps
A web app SHALL receive HTTP traffic from the built-in proxy on the port given to it through the `PORT` environment variable.

#### Scenario: Web app reachable
- **WHEN** a web app's deploy succeeds
- **THEN** HTTPS requests to its domain reach the app's container

#### Scenario: Web app never listens
- **WHEN** a web app's container does not accept connections on `PORT` within the startup timeout
- **THEN** the deploy is marked failed and the previous release keeps serving

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

### Requirement: Delete an app
The admin SHALL be able to delete an app, which stops its container and removes its route, images and stored settings.

#### Scenario: Delete a web app
- **WHEN** the admin deletes a web app and confirms
- **THEN** its container is gone, its domain no longer routes to it, and it is no longer listed
