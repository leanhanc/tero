# build-pipeline Specification

## Purpose
What later slices add to the build pipeline: building from a Dockerfile when the repository has one (slice 10). Railpack builds in a throwaway rootless BuildKit are in the slice 2 change.

## Requirements

### Requirement: Dockerfile fallback
When the repository has a Dockerfile at its root, Tero SHALL build from that Dockerfile with the same BuildKit setup instead of Railpack.

#### Scenario: Repository with a Dockerfile
- **WHEN** a deploy starts for a repository with a `Dockerfile` at its root
- **THEN** the image is built from that Dockerfile and Railpack is not used
