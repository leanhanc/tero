## ADDED Requirements

### Requirement: Recovery key
`sudo tero init` SHALL make a recovery key, print it once before the setup link, and store only its public half, in `/etc/tero/recovery-recipient`, readable only by root, since anyone holding it can make a file that restores. `sudo tero new-recovery-key` SHALL replace it and print the new one. The recovery key itself SHALL NOT be stored anywhere on the server.

#### Scenario: Recovery key printed once
- **WHEN** init completes
- **THEN** its output holds a recovery key before the setup link, which stays the last line, `/etc/tero/recovery-recipient` is owned by root with mode 0600, and no file under `/etc` or Tero's state directories holds the key

#### Scenario: New recovery key
- **WHEN** the admin runs `sudo tero new-recovery-key`
- **THEN** it prints a new key, which opens backups made from then on and not earlier ones, while the earlier key still opens those

### Requirement: Backup
`sudo tero backup` SHALL write one file, encrypted to the recovery key, holding every app's settings (name, display name, repository, branch, folder, deploy on push, run together and the pinned repository id), every variable with its value, scope and seal, and each approved source. It SHALL NOT hold the login, sessions, passkeys, the security log or the GitHub connection. It SHALL never replace an existing file, SHALL give the file to the admin who ran it with mode 0600, and SHALL list any variable it left out because the master key that sealed it was lost. It SHALL be available only as a root command.

#### Scenario: Backup made
- **WHEN** the admin runs `sudo tero backup --output <file>`
- **THEN** the file is theirs with mode 0600, holds no value or repository in the clear, and the command says how many apps it holds and that it opens only with the recovery key

#### Scenario: Existing file kept
- **WHEN** the file named by `--output` already exists
- **THEN** the command fails and the file is unchanged

### Requirement: Restore
`sudo tero restore <file>` SHALL read the recovery key, without echoing it on a terminal, list every app and its source, and after `yes` (or with `--yes`) store the apps and their variables and approved sources, and queue a deploy of every app. It SHALL refuse a server that has any app, or any variable or approved source other than exactly the backup's, SHALL store no app when the variables can't be stored, SHALL succeed when run again after stopping between the two, and SHALL be available only as a root command.

#### Scenario: Apps and variables moved to a new server
- **WHEN** the admin restores a backup with its recovery key onto a server with no apps
- **THEN** every app is back with its settings and a queued deploy, and each app deploys with its variables, sealed ones included

#### Scenario: Wrong recovery key
- **WHEN** the key given is not a recovery key, or not the one the backup was made with
- **THEN** the command fails, says so, and changes nothing

#### Scenario: Server with apps refused
- **WHEN** the server already has an app
- **THEN** the command fails and changes nothing

#### Scenario: Nothing kept from a failed restore
- **WHEN** the key service refuses the variables
- **THEN** no app or deploy is stored

#### Scenario: Restore run again
- **WHEN** a restore stored the variables but stopped before saving the apps, and the admin runs it again with the same backup
- **THEN** it restores the apps

#### Scenario: Crafted backup refused
- **WHEN** a file sealed to the recipient holds an app or variable the dashboard wouldn't accept
- **THEN** the restore fails before storing anything
