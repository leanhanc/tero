## ADDED Requirements

### Requirement: The agent as a caller
The key service SHALL recognise a peer whose uid is `tero-agent`'s as the caller `agent`, and SHALL answer it only `github.connect` and `github.jwt`. A peer running as `tero` outside `tero.service`'s cgroup SHALL still be refused.

#### Scenario: Agent asks for a JWT
- **WHEN** the agent sends `github.jwt` with GitHub connected
- **THEN** it receives an App JWT

#### Scenario: Dashboard asks for a JWT
- **WHEN** the dashboard, or any process running as `tero`, sends `github.jwt`
- **THEN** the key service refuses it

#### Scenario: Agent asks for account verbs
- **WHEN** the agent sends `session.list`, `events.list` or any other verb besides its two
- **THEN** the key service refuses it

### Requirement: GitHub App secrets held by the key service
The key service SHALL store the GitHub App's private key and webhook secret encrypted under the master key, and SHALL NOT return either to any caller. It SHALL sign App JWTs valid for at most 5 minutes for the agent, check webhook signatures for the dashboard, and delete both secrets on `github.disconnect` or the root verb `github.forget`.

#### Scenario: Secrets never leave
- **WHEN** any caller sends any verb to the key service after GitHub is connected
- **THEN** no answer contains the private key or the webhook secret

#### Scenario: Secrets not in tero's reach
- **WHEN** a process running as `tero` or `tero-agent` searches the shared database and its own files after GitHub is connected
- **THEN** it finds neither the private key nor the webhook secret

#### Scenario: Short-lived JWT
- **WHEN** the agent asks for an App JWT
- **THEN** the token expires at most 5 minutes after it was issued

#### Scenario: Rotation re-seals the App's secrets
- **WHEN** the admin runs `sudo tero rotate-key` with GitHub connected
- **THEN** the App's secrets name the new key version, and pushes still deploy and private repositories still clone

### Requirement: Connecting GitHub needs the admin's approval
`github.connect.begin` and `github.disconnect` SHALL need a session confirmed in the last 5 minutes. `github.connect.approve` SHALL need the live session that began the flow and its `state` from the last 10 minutes. `github.connect` from the agent SHALL be accepted only with an approved state that hasn't been used.

#### Scenario: Dashboard connects without a session
- **WHEN** the dashboard sends `github.connect.begin` or `github.disconnect` with no session or an unconfirmed one
- **THEN** the key service refuses it and nothing changes

#### Scenario: State from another session
- **WHEN** `github.connect.approve` carries a `state` issued to a different session
- **THEN** the key service refuses it

#### Scenario: Agent connects without approval
- **WHEN** the agent sends `github.connect` with a state the admin didn't approve, or one already used
- **THEN** the key service refuses it and stores nothing
