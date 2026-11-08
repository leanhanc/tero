## ADDED Requirements

### Requirement: Variables after a breach
`sudo tero assume-breach` SHALL print each app's variable names, marking sealed ones, to rotate at their providers, and each app's approved source to check. It SHALL print no value and delete no variable.

#### Scenario: Names and sources listed
- **WHEN** `assume-breach` finishes on a server with variables
- **THEN** the output lists each app's variable names and approved source, and holds no value

#### Scenario: Master key already lost
- **WHEN** `assume-breach` runs after the master key was lost
- **THEN** it replaces the key and finishes, and the lost variables stay listed in the dashboard as needing to be set again

#### Scenario: Apps keep their variables
- **WHEN** the admin redeploys an app after `assume-breach`
- **THEN** its process sees its variables under the new master key
