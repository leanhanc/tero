## ADDED Requirements

### Requirement: Variables held by the key service
The key service SHALL store every app variable's value encrypted under the master key, tied to its app and name, and SHALL store each app's approved source. No variable value SHALL be in the shared database or in any file `tero` can read.

#### Scenario: Not in the shared database
- **WHEN** a process running as `tero` or `tero-agent` searches the shared database and its own files after variables are saved
- **THEN** it finds no value

#### Scenario: Value moved to another row
- **WHEN** a value's ciphertext is copied to another variable's row
- **THEN** it doesn't open

#### Scenario: Rotation re-seals values
- **WHEN** the admin runs `sudo tero rotate-key` on a server with variables
- **THEN** every value names the new key version, and the next deploy gets the same values

### Requirement: Who gets values
The key service SHALL return unsealed values to the dashboard only for a live session, and SHALL never return a sealed value to it. It SHALL release values, sealed ones included, only to the agent, only for the app's approved source, and for each phase only the variables scoped for it. Root SHALL get names, sealed state, scopes and approved sources, never values.

#### Scenario: Dashboard asks without a session
- **WHEN** a process in the dashboard's unit asks for an app's variables with no live session
- **THEN** it is refused

#### Scenario: Dashboard asks for a release
- **WHEN** the dashboard, or any process running as `tero`, sends the release verb
- **THEN** the key service refuses it

#### Scenario: Build release
- **WHEN** the agent asks for a build's values
- **THEN** it receives only build-only variables and those whose scope is both, and for a run only runtime-only variables and those whose scope is both

#### Scenario: Root asks for values
- **WHEN** root asks for the summary of an app's variables
- **THEN** the answer holds no value
