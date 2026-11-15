## Context

See proposal.md for why. The e2e suite already runs against any fresh supported Ubuntu over SSH: `scripts/vm-test.sh` drives Lima VMs and `scripts/ec2-test.sh` drives a cloud server someone launched by hand. Every e2e run in this project happens on throwaway servers, never on a developer's machine. Slice 1's design fixes Lynis as the CI gate, with the minimum taken from the first green run and only ever raised.

## Goals / Non-Goals

**Goals:**
- The same suite that passes in the VMs passes on fresh cloud servers, on every tested release and on amd64 and arm64 where the provider offers it, with real Let's Encrypt certificates.

**Non-Goals:**
- New scenarios or changes to Tero itself.
- Testing on more than one provider.

## Decisions

- **Reuse `scripts/ec2-test.sh` as the runner.** CI only adds launching and destroying the servers around it, so local and CI runs exercise the same steps.
- **Credentials scoped to e2e servers.** The CI credentials can create, tag and destroy only servers carrying the e2e tag, so a leaked secret can't reach anything else in the account.

## Risks / Trade-offs

- [Stray servers keep billing after a cancelled run] → A scheduled cleanup job destroys tagged servers older than the longest run, and a budget alert catches anything it misses.
- [Let's Encrypt rate limits on nip.io] → Runs use each server's own nip.io name and stay well under the limits per registered domain; a failed issuance fails the run with its message.

## Open Questions

- Which provider: it needs an API to create and destroy servers, Ubuntu 24.04 and 26.04 images, and ideally arm64 (task 1.1).
