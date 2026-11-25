## ADDED Requirements

### Requirement: Recovery key at init
Init SHALL make the recovery key last, after the setup link is issued, so a run that stops earlier leaves no key that wasn't shown, and SHALL print it before the setup link.

#### Scenario: Link still last
- **WHEN** init completes
- **THEN** the recovery key comes before the setup link, and the link is the last line
