## Context

See proposal.md for why. The e2e suite already runs against any fresh supported Ubuntu over SSH: `scripts/ec2-test.sh` drives a cloud server, and `scripts/ec2-e2e.sh` creates throwaway EC2 servers for every architecture and release, runs it on each and removes them. Every e2e run in this project happens on throwaway servers, never on a developer's machine. Slice 1's design fixes Lynis as the CI gate, with the minimum taken from the first green run and only ever raised.

## Goals / Non-Goals

**Goals:**
- The same suite runs in CI on fresh EC2 servers, amd64 and arm64, Ubuntu 24.04 and 26.04, before every release.
- What the suite fakes, a real Let's Encrypt certificate on nip.io and a release build, is checked on the same servers.

**Non-Goals:**
- New scenarios or changes to Tero itself.
- Testing on more than one provider.
- A run on every push: a run is four servers for about 35 minutes.

## Decisions

- **AWS, through `scripts/ec2-e2e.sh`.** CI runs the script developers run, so local and CI runs exercise the same steps. It takes credentials from the environment when there are any, and the profile otherwise.
- **The suite keeps its test certificate authority; a smoke test gets the real certificate.** The suite needs a fake GitHub, a fake release server and an e2e build of Tero to test everything, and a real certificate for every app it creates would spend Let's Encrypt's limits. `scripts/ec2-smoke.sh` installs the release build `make build` makes, runs `sudo tero init`, checks with curl that the dashboard serves a certificate the system trusts and that Let's Encrypt issued it, and checks the Lynis minimum. nip.io is on the Public Suffix List, so each server's name counts toward its own limits. The suite's installation test covers `install.sh` against a fake release server, since the release under test isn't published until the run passes.
- **When it runs.** By hand, and as a required job of `release.yml`, so a tag publishes nothing until both modes pass on all four servers.
- **Credentials.** GitHub's OIDC token, only from the repository's `e2e` environment, assumes a role (`infra/e2e-ci.yaml`) that can create resources only with the `tero-e2e` tag, and change or remove only resources that carry it, in the project's Region. No key is stored.
- **Strays.** A scheduled workflow removes what runs that started over four hours ago left, judged by the start time in each run id, so a run still going is never touched. A monthly budget alert catches anything it misses.
- **Proving the gate.** The e2e build of Tero leaves out the init step named in `TERO_E2E_SKIP_STEP`; a run with a hardening step left out must fail on the Lynis minimum.

## Risks / Trade-offs

- [Stray servers keep billing after a cancelled run] → The scheduled cleanup and the budget alert.
- [Let's Encrypt rate limits on nip.io] → Only the smoke test asks, once per server, and each server's name is its own registered domain; a failed issuance fails the run with its message.
- [The role can launch any instance type] → Instances are tagged and removed within hours, and the budget alert reports the cost.
