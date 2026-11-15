## Why

Slices 1 and 2 are built and tested in Lima VMs and by hand on throwaway EC2 instances (`scripts/ec2-test.sh`), but nothing runs the end-to-end suite on real cloud servers in CI. Those were the last open tasks of both slices; moving them here lets slices 1 and 2 be archived while this work stays tracked.

## What Changes

- Pick the cloud provider for end-to-end CI and set up credentials that can only touch e2e servers, a budget alert and a cleanup job for stray servers.
- A CI job that launches fresh servers on each tested release and architecture, installs through `install.sh`, runs `sudo tero init` and the whole e2e suite with real Let's Encrypt certificates on nip.io.
- Enforce the Lynis minimum score in that job.
- Run the slice 2 suite on the same cloud servers.

The tasks are moved word for word from `add-security-baseline-slice-1` (4.1, 4.2, 4.3) and `add-first-deploy-slice-2` (5.2).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

None. This is test infrastructure: the scenarios it runs, including `server-init: Lynis score in end-to-end tests`, are already in the main specs.

## Impact

- New GitHub Actions workflow and cloud credentials stored as repository secrets.
- A cloud provider account with a budget alert, and the cost of short-lived servers per run.
- No change to Tero's code or behavior.
