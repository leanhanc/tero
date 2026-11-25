# Backups and moving to a new server

Your apps' variables are encrypted with a master key that never leaves the server, so a copy of the disk can't be read elsewhere. To move your apps to another server, or to rebuild after losing one, Tero gives you a recovery key and a backup command.

## The recovery key

`sudo tero init` prints a recovery key once, just above the setup link. It looks like `AGE-SECRET-KEY-PQ-1…`. Save it in your password manager right away: Tero doesn't keep it, and it is the only way to open your backups.

The server keeps only the key's public half, in `/etc/tero/recovery-recipient`, readable by root only. That is enough to make backups but not to open them, so someone who copies the server's disk or one of your backup files can't read your variables.

If you lose the key, or think someone saw it, run:

```bash
sudo tero new-recovery-key
```

It prints a new key for every backup made from then on. Backups made earlier still need the key you had when you made them.

## Making a backup

```bash
sudo tero backup
```

This writes `tero-backup-<date>.age` in the current folder, owned by you, with every app's settings, every variable (sealed ones too) and the source each app's variables are approved for. Choose another path with `--output`. It never overwrites a file.

Copy the file somewhere off the server. Tero doesn't make backups on a schedule, so make a new one after you change your apps or variables.

A backup doesn't hold your dashboard login, passkeys, sessions, the security log or the GitHub connection. The new server gets its own setup link, and you connect GitHub again there, since the GitHub App sends pushes to the old server's address.

If a variable was sealed with a master key that was later lost, it can't be backed up. `sudo tero backup` lists those, so you can set them again after restoring.

## Restoring on a new server

Set up the new server with the install script and `sudo tero init` as usual, copy the backup file to it, then run:

```bash
sudo tero restore tero-backup-<date>.age
```

It asks for the recovery key without showing what you type, lists every app in the backup with its repository, and asks you to type `yes`. It then restores the apps and their variables and queues a deploy of each app. Each app gets a new address on the new server's IP.

- Restore works only on a server with no apps yet. If anything fails, nothing is restored.
- Apps from private repositories can't deploy until you connect GitHub in Settings. Redeploy them once it's connected.
- Each app's variables stay tied to the same GitHub repository as before. If someone else now owns a repository at that URL, the app won't deploy until you approve the source again on its page.

Anyone with the public half of your key can make a file that restores, so only restore files you made yourself, kept where only you can change them, and check the list of apps before typing `yes`.

The backup is an [age](https://age-encryption.org) file, so you can also open it with the `age` tool and your recovery key.
