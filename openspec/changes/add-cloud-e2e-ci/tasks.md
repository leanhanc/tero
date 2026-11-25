Moved from `add-security-baseline-slice-1` (part 4) and `add-first-deploy-slice-2` (task 5.2), word for word, so both slices could be archived.

## 1. End-to-end CI on a cloud provider

- [ ] 1.1 Pick the e2e cloud provider (it needs an API to create and destroy servers, Ubuntu 24.04 and 26.04 images, and ideally arm64), then set up CI credentials limited to e2e servers, a budget alert, and a cleanup job for stray servers; verify the credentials cannot touch anything else
  - [x] AWS; `infra/e2e-ci.yaml` (OIDC role limited to `tero-e2e` resources, budget alert), `e2e-cleanup.yml` and `scripts/ec2-e2e.sh cleanup-stale`
  - [ ] Lean deploys the stack and the `e2e` environment (docs/e2e-ci.md), then the role is checked with the IAM policy simulator against untagged resources
- [ ] 1.2 Add a CI job that launches fresh servers on each tested release (amd64, plus arm64 where the provider offers it) with public IPs, installs through `install.sh` from the release under test, runs `sudo tero init`, and runs the whole e2e suite with real Let's Encrypt certificates on nip.io; verify every scenario in the four slice-1 specs passes on both architectures
  - [x] `e2e-cloud.yml` runs the suite and `scripts/ec2-smoke.sh` on all four servers, by hand and before every release
  - [ ] One green run of both modes in CI
- [ ] 1.3 Enforce the Lynis minimum in CI; verify the job fails when a hardening step is skipped on purpose
  - [x] `TERO_E2E_SKIP_STEP` in e2e builds and the workflow's `skip_step` input
  - [ ] One run with `hardening kernel settings` skipped fails on Lynis

## 2. Slice 2 on the cloud servers

- [ ] 2.1 Once slice 1's part 4 exists, run the slice 2 suite on the cloud servers (amd64 and arm64) with real Let's Encrypt certificates on nip.io, and check the Lynis score has not dropped
