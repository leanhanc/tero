## ADDED Requirements

### Requirement: Variable events
The key service SHALL record changes to variables, seals, replacements, source approvals, refused releases and each release of a sealed variable, naming the app and variable they concern. No event SHALL hold a value.

#### Scenario: Change recorded
- **WHEN** the admin changes, seals or replaces a variable
- **THEN** an event names the app and variable and holds no value

#### Scenario: Refused release is a warning
- **WHEN** a release is refused for its source
- **THEN** `variables_release_refused` is recorded and is in the journal at warning priority

#### Scenario: Events can't be forged by the dashboard
- **WHEN** the dashboard tries to append any variable event
- **THEN** it is refused

#### Scenario: No value in the journal
- **WHEN** a deploy with variables has run
- **THEN** a search of the journal and the security log for the values finds nothing
