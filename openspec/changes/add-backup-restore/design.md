## Context

The master key is sealed by systemd-creds, with the TPM when present, so it can't leave the server by design. Moving variables to another server therefore needs a second key the admin holds. Slice 6 deferred it; Lean decided on the shape on 2026-11-21 (see proposal).

## Goals / Non-Goals

**Goals:**
- An admin with a backup file and the recovery key can rebuild every app and its variables on a new server.
- A copy of the server's disk, or of a backup file, opens nothing without the recovery key.
- Root never sees a value.

**Non-Goals:**
- Scheduled or off-site backups. The admin runs `sudo tero backup` and copies the file.
- Moving the login, sessions, passkeys, the security log or the GitHub App. The new server issues its own setup link, and the admin connects GitHub again, since the App's webhook names the old address.
- Restoring into a server that already has apps, or merging backups.

## Decisions

- **age, with its post-quantum hybrid keys.** The recovery key is an age identity (ML-KEM-768 with X25519). The server stores only the recipient, so it can encrypt backups but not open them. age is a small, reviewed format with authenticated encryption, and the file can be opened with the `age` tool if Tero isn't at hand. A hybrid key keeps a stolen backup safe against later quantum attacks, since the variables in it may stay valid for years.
- **Two layers.** The key service exports the variables and approved sources sealed to the recipient; root wraps that, with the apps' settings, in an outer file sealed to the same recipient. On restore root opens the outer layer to show what will be restored and hands the inner one, with the recovery key, back to the key service. Root holds the recovery key while it restores, as it must to open anything, but never a value.
- **The recipient in `/etc/tero`, readable only by root.** Replacing it would redirect later backups to someone else's key, and anyone holding it can make a file that restores, so only root can read or write it.
- **Restore only onto an empty server, and safe to run again.** The database refuses a restore while it holds any app. The key service stores the variables first and refuses while it holds any variable or approved source, unless they are exactly the backup's: then it succeeds without storing anything, so a restore that stopped before the apps were saved can simply be run again. The apps and their deploys are saved in one transaction after that, so the shared database is never locked while the key service works.
- **Pinned repository ids travel with the backup.** The first deploy on the new server is refused if GitHub now has another repository at the same URL, as it would be on the old server.
- **Deploys queued as manual redeploys.** Every restored app gets a queued deploy. Apps from private repositories fail until GitHub is connected again, and the admin redeploys them then.
- **Larger messages for root.** A backup can hold up to 200 variables of 32 KiB for each app, so root's requests and answers may be up to 48 MiB; everyone else keeps the 2 MiB limit.

## Risks / Trade-offs

- [Anyone with the recipient can make a file that restores] → age proves a file wasn't changed, not who made it. Only root reads the recipient on the server, but a copy elsewhere would do. `sudo tero restore` lists every app and its repository and asks for `yes` before storing anything; the admin only restores files they made.
- [A lost recovery key makes every backup useless] → The key is printed with a prompt to save it in a password manager, and `sudo tero new-recovery-key` makes a new one for later backups.
- [Variables whose master key was lost can't be exported] → The backup leaves them out and `sudo tero backup` lists them, so the admin sets them again.
