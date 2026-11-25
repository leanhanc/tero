## Why

Variables are sealed with a master key that only the server's systemd (and its TPM, when it has one) can open, and the apps' settings live in a database nothing backs up. A server that dies or is replaced takes every app's configuration and every secret with it, and the admin has to set each one again by hand.

## What Changes

- `sudo tero init` makes a recovery key, prints it once and keeps only its public half. `sudo tero new-recovery-key` replaces it.
- `sudo tero backup` writes one file, encrypted to the recovery key, with every app's settings and every variable, sealed ones included, and each app's approved source.
- `sudo tero restore <file>` asks for the recovery key, shows what the backup holds, and on a freshly set up server stores the apps and variables and queues a deploy of every app.
- The key service seals its part of a backup to the recovery key itself, and opens it only on restore, so no value leaves it in the clear.

## Capabilities

### New Capabilities

- `backup-restore`: the recovery key, the backup file and the restore.

### Modified Capabilities

- `key-service`: root's export and import of the apps' variables.
- `server-init`: init prints the recovery key.

## Impact

- New `internal/recovery` and `internal/backup` packages, root commands in `cmd/tero`, two key service verbs, and the new dependency `filippo.io/age`.
- Public docs: `docs/backup.md`.
