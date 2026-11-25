# harden-podman-helpers

Podman, every container and the network helpers that carry the apps' traffic run as `tero-podman`, a user the key service doesn't trust, instead of as `tero-agent`, which it trusts with every value.
