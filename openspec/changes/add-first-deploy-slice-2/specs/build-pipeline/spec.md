## ADDED Requirements

### Requirement: Source fetched from GitHub
Tero SHALL fetch the source of a deploy with a shallow clone of the tracked branch's latest commit over HTTPS from `github.com`, and SHALL record that commit's SHA and message on the deploy.

#### Scenario: Commit recorded
- **WHEN** a deploy starts building
- **THEN** the deploy shows the commit SHA and the first line of its message, matching the branch head on GitHub

### Requirement: Railpack plans the build
Tero SHALL plan the build with Railpack as a Go library (`GenerateBuildPlan` then `ConvertPlanToLLB`) and submit the LLB with its own BuildKit `Solve` for the host's platform.

#### Scenario: JavaScript package managers and runtimes
- **WHEN** a deploy runs for a backend that uses npm, pnpm, Yarn or Bun
- **THEN** the image is built with that package manager and runs on the runtime Railpack detects (Bun when the package manager is Bun or the scripts call `bun`, Node otherwise), and the app answers on its address

#### Scenario: Railpack cannot plan
- **WHEN** Railpack returns an unsuccessful plan
- **THEN** the deploy fails at the building step with Railpack's reason in the build log, and the live release keeps serving

#### Scenario: No start command
- **WHEN** Railpack finds no start command
- **THEN** the deploy fails with a message saying to add a `start` script to `package.json`, a `main` field, or an `index.js` or `index.ts`

#### Scenario: Plan targets the host platform
- **WHEN** Tero builds on an arm64 or amd64 host
- **THEN** the image is built for that host's platform

### Requirement: Throwaway rootless BuildKit
Each build SHALL run in a fresh rootless BuildKit container (`moby/buildkit` rootless image pinned by digest) started under Podman as `tero`, and the container SHALL be removed when the build ends. BuildKit's cache SHALL be kept in a named volume between builds and pruned above 10 GB.

#### Scenario: BuildKit lifecycle
- **WHEN** a build finishes, succeeds or fails
- **THEN** its BuildKit container is stopped and removed

#### Scenario: BuildKit container flags
- **WHEN** Tero starts the BuildKit container
- **THEN** it runs with `--security-opt unmask=/proc/*`, its own fixed slice of `tero`'s subordinate ids and a memory cap, without `--privileged`, `/dev/fuse` or `apparmor=unconfined`, and BuildKit keeps its process sandbox

#### Scenario: No published port or socket
- **WHEN** Tero talks to BuildKit
- **THEN** it connects through `podman exec ... buildctl dial-stdio`, and the BuildKit container publishes no port

#### Scenario: Second build reuses the cache
- **WHEN** an app is redeployed with no change to its commit
- **THEN** the build reuses cached layers and Railpack's base images are not pulled again

### Requirement: No registry
The built image SHALL be exported as a tarball and loaded with `podman load` under `tero`. No registry is used to store it.

#### Scenario: Image available after build
- **WHEN** a build succeeds
- **THEN** the image is in `tero`'s Podman image store, tagged with the app and deploy, and the tarball is deleted

### Requirement: Build log
Tero SHALL stream a deploy's build output to the dashboard as it is produced and keep it after the deploy ends, up to 10 MB per deploy.

#### Scenario: Watch a build
- **WHEN** the admin opens a deploy that is building
- **THEN** build output appears in the dashboard as it is produced, without reloading the page

#### Scenario: Failed build log kept
- **WHEN** a build fails
- **THEN** the full build log stays available on that deploy

#### Scenario: Log rendered as text
- **WHEN** build output contains HTML or ANSI escape codes
- **THEN** the dashboard shows it as plain text, with ANSI codes removed and no markup interpreted

### Requirement: Build timeout
A build SHALL be stopped and its deploy failed when it runs longer than 30 minutes.

#### Scenario: Build too long
- **WHEN** a build is still running 30 minutes after it started
- **THEN** BuildKit is stopped and removed, and the deploy fails with a message saying the build timed out
