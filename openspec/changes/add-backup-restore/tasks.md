## 1. Recovery key

- [x] 1.1 Add `internal/recovery`: generate an age hybrid identity, store its recipient in `/etc/tero/recovery-recipient` (root, 0600), seal to it and open with the key; unit tests cover a wrong key, a garbage key and a changed file
- [x] 1.2 Init makes the key after the setup link, prints it before the link, and keeps the link last
- [x] 1.3 Add `sudo tero new-recovery-key`

## 2. Key service

- [x] 2.1 Add root's `backup.export` and `backup.import` verbs: export seals every approved source and variable to the recipient and lists what was lost; import opens with the key, applies a save's rules, refuses a server with any variable or source, keeps pinned repository ids, and records `backup_exported` and `backup_restored`; unit tests cover the round trip, the callers refused and crafted backups
- [x] 2.2 Let root's messages be up to 48 MiB

## 3. Backup and restore

- [x] 3.1 Add `internal/backup` and `sudo tero backup [--output FILE]`, which never replaces a file and gives it to the admin who ran sudo
- [x] 3.2 Add `sudo tero restore [--yes] FILE`: read the key without echo, list the apps, ask for `yes`, store apps and variables together, queue a deploy of every app; unit tests cover the round trip, a failed import and crafted apps
- [x] 3.3 Write `docs/backup.md` and link it from the README

## 4. Verify

- [x] 4.1 E2e: the key is printed and not stored, a backup is encrypted and the admin's, a wrong key and a server with apps are refused, a new key doesn't open an older backup, and after assume-breach a restore onto an emptied server deploys the variables app with its sealed value
- [x] 4.2 Run the whole e2e suite on EC2, on amd64 and arm64 with Ubuntu 24.04 and 26.04
