## ADDED Requirements

### Requirement: Per-server GitHub App via manifest flow
Tero SHALL connect to GitHub by creating a private GitHub App owned by the admin's personal account or an organization the admin owns, through GitHub's App manifest flow, one App per server, asking only for `contents: read`, `metadata: read` and the `push` event. Starting the connection SHALL need a confirmed session, and the callback SHALL be accepted only with the `state` issued to that session in the last 10 minutes, once. The agent SHALL convert the manifest code and hand the App's secrets to the key service, so the dashboard never receives them.

#### Scenario: Connect GitHub
- **WHEN** the admin, with a confirmed session, chooses Connect GitHub, approves the App on GitHub and installs it on chosen repositories
- **THEN** the settings page shows the App's name, the account it is installed on and those repositories

#### Scenario: Connect an organization
- **WHEN** the admin chooses an organization they own and connects
- **THEN** the App is created in that organization, installed on its chosen repositories, and the settings page names the organization

#### Scenario: Dashboard never sees the private key
- **WHEN** GitHub is connected
- **THEN** no response the dashboard received from GitHub, the key service or the database during the flow contained the App's private key or webhook secret

#### Scenario: Unconfirmed session
- **WHEN** the admin chooses Connect GitHub more than 5 minutes after last entering the password and a code
- **THEN** the dashboard asks for the password and a code before sending the admin to GitHub

#### Scenario: Callback with a foreign state
- **WHEN** the callback arrives with a `state` the key service didn't issue to this session, one already used, or one older than 10 minutes
- **THEN** nothing is stored and the page says to start again

#### Scenario: One account per server
- **WHEN** a GitHub App is already connected
- **THEN** the dashboard doesn't offer to connect another

### Requirement: No long-lived repository credentials
Tero SHALL access private repositories only with installation tokens scoped to one repository and `contents: read`, minted and held only in the agent's memory, and SHALL revoke each one once its fetch ends. It SHALL NOT accept or store personal access tokens or deploy keys, and no token SHALL be written to the database, the app's source, its `.git` directory, its image or the build log.

#### Scenario: Deploy a private repository
- **WHEN** an app from a private repository the App is installed on is deployed
- **THEN** the deploy goes live, and the token it used was scoped to that repository and revoked before the build started

#### Scenario: Token kept out of the build
- **WHEN** a private repository's deploy is built
- **THEN** neither the build context, the image nor the build log contains the token

#### Scenario: No PAT input
- **WHEN** the admin looks for a way to add a GitHub token or SSH deploy key
- **THEN** the dashboard offers none

### Requirement: Signed webhooks
Tero SHALL accept GitHub webhooks at `https://<dashboard-domain>/api/github/webhook` only with a valid `X-Hub-Signature-256` for the App's webhook secret, SHALL ignore a delivery id it has already handled in the last 3 days, and SHALL throttle addresses that send invalid signatures.

#### Scenario: Valid push webhook
- **WHEN** GitHub delivers a correctly signed push to an app's tracked branch
- **THEN** a deploy with trigger `push` and the pusher's login is queued for that app

#### Scenario: Unsigned or forged webhook
- **WHEN** a request reaches the webhook endpoint with a missing or invalid signature
- **THEN** it is answered `401`, a `webhook_signature_failed` event is recorded and no deploy is queued

#### Scenario: Replayed delivery
- **WHEN** a correctly signed delivery is sent a second time with the same delivery id
- **THEN** it is answered `200` and no second deploy is queued

#### Scenario: Flood of bad signatures
- **WHEN** one address sends more than 10 invalid signatures in a minute
- **THEN** further requests from it are answered `429` until the minute passes

#### Scenario: Push to another branch, a tag or a deleted branch
- **WHEN** a signed push arrives for a branch the app doesn't track, for a tag, or deleting the tracked branch
- **THEN** no deploy is queued

#### Scenario: Pushed commit isn't the branch head
- **WHEN** a signed push names a commit that is no longer, or never was, the head of the tracked branch
- **THEN** the deploy builds the branch head Tero reads from GitHub, never the commit in the payload

### Requirement: Public repositories by polling
Tero SHALL deploy a public GitHub repository the App doesn't cover by checking its tracked branch every minute from the agent, queuing a deploy with trigger `poll` when the head differs from the commit of the app's newest deploy.

#### Scenario: New commit on a public repository
- **WHEN** a new commit lands on the tracked branch of a public-repository app
- **THEN** Tero queues a deploy of it within about one minute

#### Scenario: No change
- **WHEN** the tracked branch hasn't moved since the app's newest deploy
- **THEN** no deploy is queued

#### Scenario: Repository gone
- **WHEN** a polled repository is deleted or made private
- **THEN** after 3 failed checks Tero stops polling it and the app page says why

### Requirement: Missed webhooks redelivered at start
When the agent starts with GitHub connected, Tero SHALL ask GitHub to redeliver each failed `push`, `installation` or `installation_repositories` delivery of the App from GitHub's redelivery window that it hasn't handled, at most once per delivery id. Redelivered webhooks SHALL go through the same signature, replay and branch checks as live ones.

#### Scenario: Push while the server was down
- **WHEN** a push to a webhook app's tracked branch happens while the server is down, and the server starts again within 3 days
- **THEN** GitHub redelivers it and a deploy of the branch head is queued with trigger `push`

#### Scenario: Several missed pushes
- **WHEN** three pushes to one app were missed
- **THEN** one deploy is queued for that app, and it builds the newest commit

#### Scenario: Delivery already handled
- **WHEN** a delivery GitHub lists as failed was in fact handled by Tero (for example, it timed out after Tero queued the deploy)
- **THEN** Tero doesn't ask for it again

### Requirement: Disconnect GitHub
The admin SHALL be able to disconnect GitHub from the settings page with a confirmed session. Disconnecting SHALL delete the App's credentials, revoke outstanding tokens, move apps from public repositories back to polling, and link to the App's page on GitHub to delete it.

#### Scenario: Disconnect
- **WHEN** the admin disconnects GitHub
- **THEN** signed webhooks are refused, deploys of private-repository apps fail with a message to connect GitHub in Settings, public-repository apps are polled, and the page links to the App's settings page on GitHub

#### Scenario: Reconnect
- **WHEN** the admin connects a new App and installs it on the same repositories
- **THEN** the existing apps deploy on push again without being recreated

#### Scenario: Installation removed on GitHub
- **WHEN** the admin uninstalls or suspends the App on GitHub
- **THEN** the settings page says the App needs to be installed again, and private-repository deploys fail with that message
