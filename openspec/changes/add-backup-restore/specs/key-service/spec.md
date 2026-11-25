## ADDED Requirements

### Requirement: Backups of variables
The key service SHALL let only root export every approved source and variable sealed to a recovery key recipient root names, and import such an export with the recovery key. Export SHALL leave out variables whose master key was lost and name them. Import SHALL apply the rules of a save, keep each source's pinned repository id, seal every value with the current master key, and refuse while any variable or approved source is stored, unless what is stored is exactly the backup's, which it accepts without storing anything. Both SHALL be recorded in the security log. Root's requests and answers MAY be up to 48 MiB; every other caller's stay within 2 MiB.

#### Scenario: Only root exports or imports
- **WHEN** the dashboard or the agent asks for an export or an import
- **THEN** it is refused

#### Scenario: No value in the clear
- **WHEN** root exports
- **THEN** the answer holds no value in the clear, and only the recovery key opens it
