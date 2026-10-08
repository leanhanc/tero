# build-pipeline Specification

## Purpose
Turn a commit into a runnable image on the same server, without root and without a registry. Railpack, used as a Go library, plans the build; a short-lived rootless BuildKit container under Podman executes it; the result is loaded straight into Podman. A repository with a Dockerfile is built from that Dockerfile instead.

The build path, including the 1,000,000-id subuid range and the `unmask=/proc/*` requirement, must be validated on every tested Ubuntu release with the spike program on the local branch `spike/railpack-podman-buildkit` (`SPIKE.md`) before these requirements are treated as confirmed.

## Requirements

### Requirement: Railpack plans the build
When the repository has no Dockerfile, Tero SHALL plan the build with Railpack as a Go library (`GenerateBuildPlan` then `ConvertPlanToLLB`) and submit the LLB with its own BuildKit `Solve`.

#### Scenario: Supported language without a Dockerfile
- **WHEN** a deploy starts for a repository with no Dockerfile in a language Railpack supports
- **THEN** Tero builds an image from the Railpack plan and the deploy continues to start the app

#### Scenario: Railpack cannot plan
- **WHEN** Railpack returns an unsuccessful plan
- **THEN** the deploy fails with Railpack's reason shown in the build log, and the currently running release keeps serving

#### Scenario: Plan targets the host platform
- **WHEN** Tero builds on an arm64 or amd64 host
- **THEN** the image is built for that host's platform

### Requirement: Dockerfile fallback
When the repository has a Dockerfile at its root, Tero SHALL build from that Dockerfile with the same BuildKit setup instead of Railpack.

#### Scenario: Repository with a Dockerfile
- **WHEN** a deploy starts for a repository with a `Dockerfile` at its root
- **THEN** the image is built from that Dockerfile and Railpack is not used

### Requirement: Throwaway rootless BuildKit
Each build SHALL run in a fresh rootless BuildKit container (`moby/buildkit` rootless image pinned by digest) started under Podman as `tero`, and the container SHALL be removed when the build ends.

#### Scenario: BuildKit lifecycle
- **WHEN** a build finishes, succeeds or fails
- **THEN** its BuildKit container is stopped and removed

#### Scenario: BuildKit container flags
- **WHEN** Tero starts the BuildKit container
- **THEN** it runs with `--security-opt unmask=/proc/*` and without `--privileged`, and BuildKit keeps its process sandbox

#### Scenario: No published port or socket
- **WHEN** Tero talks to BuildKit
- **THEN** it connects through `podman exec ... buildctl dial-stdio`, and the BuildKit container publishes no port

### Requirement: No registry
The built image SHALL be exported as a tarball and loaded with `podman load` under `tero`. No registry is used.

#### Scenario: Image available after build
- **WHEN** a build succeeds
- **THEN** the image is present in `tero`'s Podman image store, tagged with the app and deploy, and no registry was contacted to store it

### Requirement: Build logs
Tero SHALL stream build output to the deploy's log as the build runs and keep it after the build ends.

#### Scenario: Watch a build
- **WHEN** the admin opens a deploy that is building
- **THEN** build output appears in the dashboard as it is produced

#### Scenario: Failed build log kept
- **WHEN** a build fails
- **THEN** the full build log stays available on that deploy
