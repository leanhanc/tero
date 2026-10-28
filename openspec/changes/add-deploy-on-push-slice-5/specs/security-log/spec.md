## ADDED Requirements

### Requirement: GitHub events
The key service SHALL record `github_connected` and `github_disconnected`, and `webhook_signature_failed` with the source IP. The dashboard SHALL append `github_installation_changed` when the installation is deleted, suspended or its repositories change. `github_connected` and `github_disconnected` SHALL be kept when a flood prunes events; `webhook_signature_failed` MAY be pruned. Events SHALL never hold a private key, webhook secret, JWT or installation token.

#### Scenario: Connection recorded
- **WHEN** the admin connects GitHub
- **THEN** a `github_connected` event is recorded with source `tero-keys`

#### Scenario: Forged webhook recorded
- **WHEN** a webhook with an invalid signature arrives
- **THEN** a `webhook_signature_failed` event with the sender's IP is recorded

#### Scenario: Dashboard can't append connection events
- **WHEN** the dashboard appends a `github_connected` or `github_disconnected` event
- **THEN** the request is refused and nothing is stored
