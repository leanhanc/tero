## Context

`tero-keys` knows the agent by its uid alone: the agent runs in `tero-agent`'s user manager, which delegates its cgroup subtree to `tero-agent`, so a cgroup proves nothing there. Rootless Podman runs its network helpers as the user that runs Podman, and in the same cgroup as the Podman process that starts them. Whoever runs Podman therefore also runs the helpers.

## Goals / Non-Goals

**Goals:**
- No process that handles the apps' traffic, and no container, runs as a uid `tero-keys` answers.
- The agent keeps working as it does: same commands, same container options, same tmpfs handling of values.

**Non-Goals:**
- Hiding running apps' values from `tero-podman`. Whoever runs a container can read what it was given, so a compromised helper can still read the values of the apps that are running. It can no longer ask for anything else: build-only values, apps that aren't running, sealed values of other apps, or GitHub tokens.
- Faster container starts: Slice 7 tracks `--userns=auto`'s copy of every layer.

## Decisions

- **A separate user for Podman, not for the helpers alone.** Podman can't start its helpers as another user. A cgroup check for the agent was considered and rejected: the helpers would still share the agent's uid, so they could read its files, signal it, and read its memory wherever ptrace is allowed.
- **Podman's own API service over its socket.** The agent runs the same podman CLI with `CONTAINER_HOST` pointing at the socket, so every command, BuildKit's `podman exec` connection helper included, goes to `tero-podman`'s Podman unchanged. sudo or a setuid helper would have given `tero-agent` a way to root, and the Go bindings behave differently across the tested Podman versions.
- **Socket activation with `KillMode=process`.** The service exits when idle and starts on the next connection. Only Podman itself is stopped then, so the conmon and helper processes of running apps stay. This replaces the transient scopes the agent used to start containers in.
- **Shared tmpfs directories made by `tmpfiles.d`.** `/run/tero-env` (0750, `tero-agent:tero-podman`) lets Podman mount each file the agent writes, while nothing running as `tero-podman` can change one. `/run/tero-output` (2770, `tero-podman:tero-podman`) lets conmon write each container's output and the agent remove what a removed container left. The agent never reads or follows anything in it. The socket's directory, `/run/tero-podman` (0750), is the only way in for the agent.
- **BuildKit's settings in `/etc/tero/buildkitd.toml`.** They never change, so init writes them once, readable by the BuildKit container, instead of the agent writing them into its own home, which `tero-podman` can't enter.
- **The planner can't reach the socket.** It runs with `env -i`, and Landlock refuses it every Unix socket.

## Risks / Trade-offs

- [A start that times out keeps going in the Podman service, since cancelling a client doesn't cancel the server's work] → The agent removes the half-made container as before; the removal waits for Podman's storage lock instead of killing a scope.
- [`tero-agent` can drive any container through the socket, including one mapped to `tero-podman`'s own uid] → That direction gains nothing: the agent is the more trusted side. The agent still refuses such containers itself.
- [Podman 4.9 adds search permission for others to `tero-podman`'s home] → As before for `tero-agent`: listing and reading stay closed.
