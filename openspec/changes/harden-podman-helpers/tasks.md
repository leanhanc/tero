## 1. Podman under its own user

- [x] 1.1 Init creates `tero-podman` with home `/var/lib/tero-podman` (0700), the 1,000,000-id subordinate range and lingering, makes `tero-agent` the only other member of its group, and checks rootless Podman as `tero-podman`; `tero-agent` gets no range
- [x] 1.2 Init installs `tero-podman.socket` and `tero-podman.service` as user units limited to `tero-podman`, with the socket at `/run/tero-podman/podman.sock` (0660), and enables them
- [x] 1.3 Init writes `/etc/tmpfiles.d/tero.conf` for `/run/tero-podman`, `/run/tero-env` and `/run/tero-output`, and `/etc/tero/buildkitd.toml`
- [x] 1.4 The agent sets `CONTAINER_HOST` to the socket at start; containers start with plain `podman run` and `podman start`, without transient scopes; variables and output use the new directories; unit tests updated
- [x] 1.5 The metadata firewall rule, `DenyUsers`, init's account and group checks and the key service's forbidden groups cover `tero-podman`

## 2. Breach response

- [x] 2.1 `assume-breach` stops `tero-podman`'s slice and processes, moves aside its `~/.config`, crontab and temporary files, starts its user manager and resets its Podman store; unit tests cover the order and a planted `containers.conf`

## 3. Verify

- [x] 3.1 Update the e2e suite: Podman service reachable by the agent only, helpers run as `tero-podman`, nothing but the agent runs as `tero-agent`, the new directories' modes, `tero-podman`'s range, lingering and login, and breach leftovers for `tero-podman`
- [x] 3.2 Run the whole e2e suite on EC2, on amd64 and arm64 with Ubuntu 24.04 and 26.04
- [x] 3.3 Update the security mappings, the README and `docs/breach.md`
