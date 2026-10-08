# app-logs Specification

## Purpose
Show what a running app prints, live and recent, in the dashboard. Build logs belong to each deploy (see `build-pipeline`); these are the app's runtime stdout and stderr.

## Requirements

### Requirement: Runtime logs in the dashboard
The dashboard SHALL show each app's stdout and stderr, streaming new lines live and showing recent history on open.

#### Scenario: Live logs
- **WHEN** the admin opens an app's logs while the app prints a line
- **THEN** the line appears in the dashboard without reloading

#### Scenario: Recent history
- **WHEN** the admin opens an app's logs
- **THEN** the most recent lines the app printed before the page was opened are shown

#### Scenario: Logs readable by tero
- **WHEN** an app container is running under `tero`
- **THEN** the Tero service can read its logs without root

### Requirement: Logs survive restarts
Runtime logs SHALL stay available after the app's container restarts or is replaced by a new deploy, up to a size limit per app.

#### Scenario: Crash then restart
- **WHEN** an app crashes and is restarted
- **THEN** the lines it printed before the crash are still shown

#### Scenario: Size limit
- **WHEN** an app's stored logs exceed the per-app limit
- **THEN** the oldest lines are dropped first
