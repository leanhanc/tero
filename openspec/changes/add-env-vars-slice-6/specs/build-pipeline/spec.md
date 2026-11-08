## ADDED Requirements

### Requirement: Values kept out of images
Only variables scoped for builds SHALL reach a build, as secret mounts. Tero SHALL NOT write any variable's value into an image's layers or configuration.

#### Scenario: Value available to a build step
- **WHEN** a build step reads a build-only variable, or one whose scope is both
- **THEN** it sees the value

#### Scenario: Other variables absent from builds
- **WHEN** a build step reads a runtime-only variable
- **THEN** it isn't set

#### Scenario: No value in the image
- **WHEN** an app with a build variable that its code doesn't copy has deployed
- **THEN** a search of the saved image's layers and configuration finds no value

#### Scenario: Changed value rebuilds what used it
- **WHEN** the admin changes a variable scoped for builds
- **THEN** the build steps that use it run again rather than coming from the cache

### Requirement: Values masked in deploy logs
The build log and the app's last output in a deploy log SHALL show `[hidden]` in place of every sealed value and every unsealed value of 8 bytes or more, including across write boundaries.

#### Scenario: Build prints a value
- **WHEN** a build step prints a sealed value
- **THEN** the build log shows `[hidden]`, and no file in the shared directory holds the value

#### Scenario: App prints a value and exits
- **WHEN** an app prints a sealed value and exits before answering
- **THEN** the deploy log's last output shows `[hidden]`

### Requirement: Build cache mounts per app
Build cache mounts SHALL be separate per app, so that one app's build can't write what another app's build reads from them.

#### Scenario: Planted package
- **WHEN** one app's build writes a file into its package manager's cache mount
- **THEN** another app's build using the same package manager doesn't see it
