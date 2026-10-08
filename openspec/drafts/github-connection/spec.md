# github-connection Specification

## Purpose
Connect one GitHub account so pushes deploy, without long-lived credentials: each server creates its own GitHub App, verifies every webhook, and uses short-lived installation tokens. Public repositories can also be deployed by URL without connecting anything.

## Requirements

### Requirement: Per-server GitHub App via manifest flow
Tero SHALL connect to GitHub by creating a GitHub App owned by the admin's account using GitHub's App manifest flow, one App per server.

#### Scenario: Connect GitHub
- **WHEN** the admin starts "Connect GitHub" in the dashboard and approves the App creation on GitHub
- **THEN** Tero stores the new App's id, private key and webhook secret, and the admin is sent to install it on chosen repositories

#### Scenario: One account per server
- **WHEN** a GitHub App is already connected
- **THEN** the dashboard does not offer to connect a second account

### Requirement: No long-lived repository credentials
Tero SHALL access private repositories only with short-lived GitHub App installation tokens. It SHALL NOT accept or store personal access tokens or deploy keys.

#### Scenario: Clone a private repository
- **WHEN** Tero fetches source for a private repository
- **THEN** it uses an installation token minted for that fetch, and the token is not written to disk in the app's source or image

#### Scenario: No PAT input
- **WHEN** the admin looks for a way to add a GitHub token or SSH deploy key
- **THEN** the dashboard offers none

### Requirement: Signed webhooks
Tero SHALL verify the signature of every GitHub webhook with the App's webhook secret and ignore any delivery that fails verification.

#### Scenario: Valid push webhook
- **WHEN** GitHub delivers a correctly signed push event for a connected app's branch
- **THEN** Tero starts a deploy of the pushed commit

#### Scenario: Unsigned or forged webhook
- **WHEN** a request reaches the webhook endpoint with a missing or invalid signature
- **THEN** Tero responds with an error status and starts no deploy

#### Scenario: Push to another branch
- **WHEN** a signed push event arrives for a branch the app does not track
- **THEN** Tero starts no deploy

### Requirement: Public repositories by URL
Tero SHALL deploy a public GitHub repository from its URL without a GitHub connection, by polling the tracked branch every minute.

#### Scenario: New commit on a public repository
- **WHEN** a new commit lands on the tracked branch of a public-repo app
- **THEN** Tero detects it within about one minute and starts a deploy of that commit

#### Scenario: No change
- **WHEN** the tracked branch has not moved since the last poll
- **THEN** no deploy starts
