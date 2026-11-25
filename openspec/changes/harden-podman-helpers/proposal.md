## Why

Rootless Podman starts its network helpers (pasta on Podman 5, slirp4netns on Podman 4, and rootlessport) as the user that runs Podman. They parse every packet an app sends or receives, so they are the code on the host most exposed to an attacker who controls an app. While Podman ran as `tero-agent`, a compromised helper ran as the uid `tero-keys` trusts as the agent: it could ask for every app's variables, read the agent's memory and files, and act with the GitHub App's credentials.

## What Changes

- A new system user, `tero-podman`, owns rootless Podman: its storage, the subordinate id range, every image and container, BuildKit, and the helpers Podman starts.
- `tero-podman`'s user manager runs a socket-activated Podman service. The socket is in `/run/tero-podman`, which only `tero-podman` and its group can enter, and `tero-agent` is the group's only other member.
- The agent drives Podman as a client of that service. `tero-agent` keeps the key service, GitHub tokens and the build planner, and no longer has subordinate ids or Podman storage.
- Variables and container output move to `/run/tero-env` and `/run/tero-output`, tmpfs directories systemd creates at boot, shared only between the two users.
- The metadata firewall rule, SSH's `DenyUsers`, `assume-breach` and init's account checks cover `tero-podman`.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `process-isolation`: containers and Podman's helpers run as `tero-podman`, never as `tero-agent`; the agent reaches Podman only through its service.
- `server-init`: init creates `tero-podman`, its subordinate range, lingering, the Podman service and the shared tmpfs directories; `tero-agent` loses its range.
- `breach-response`: the reset stops and clears `tero-podman` as well.

## Impact

- `internal/initflow`, `internal/podman`, `internal/containers`, `internal/build`, `internal/breach`, `cmd/tero`, the embedded unit and tmpfiles files, and the e2e suite.
- No upgrade path: there are no installs to migrate.
