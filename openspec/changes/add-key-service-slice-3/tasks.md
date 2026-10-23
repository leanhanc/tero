Implements all of slice 3: the new capabilities `key-service`, `security-log` and `breach-response`, and this change's deltas to `dashboard-access`, `process-isolation` and `server-init`. Each task names the spec scenarios it makes pass, written as `capability: Scenario`. Parts 2 to 4 are covered by unit tests; part 1 and task 5.1 run in fresh Lima VMs (Ubuntu 24.04 and 26.04) on the Mac and on the Ubuntu PC, and task 5.2 on EC2.

## 1. Prove the systemd pieces on both releases

- [ ] 1.1 In fresh 24.04 (systemd 255) and 26.04 (systemd 259) VMs, after `sudo tero init`: check `tero-keys.socket` creates the socket as `tero-keys:tero` 0660, `LoadCredentialEncrypted=kek:/etc/tero/keys/` gives `kek_1` and `kek_2` during a rotation, a corrupted `kek_<n>` stops `tero-keys` with status 243 while `tero.service` keeps running, and the unit's sandbox leaves Argon2id and SQLite working. Record results in the change's `SPIKE.md`
- [ ] 1.2 In the same VMs, check from a process inside `tero.service` that `SO_PEERPIDFD` and `/proc/<pid>/cgroup` identify it as the dashboard, that it can't write any `cgroup.procs`, and that `/run/user/<uid>/bus` is hidden; check a `tero` process moved into `tero-agent.service`'s cgroup is refused
- [ ] 1.3 On an EC2 instance with NitroTPM, check `--with-key=auto --tpm2-pcrs=` picks `tpm2+host` and the key survives a reboot and a kernel update

## 2. Key service

- [x] 2.1 Add `tero keys` (runs only as `tero-keys`, refuses other users) with its own SQLite database and migrations, socket activation, and the length-prefixed JSON protocol with the size, idle and connection limits. Covers `key-service: Oversized or idle request`
- [x] 2.2 Add the caller check (`SO_PEERCRED`, pidfd, exact cgroup, liveness) and per-caller verb lists. Covers `key-service: Other process running as tero`, `Request outside the caller's role`, `process-isolation: Other tero processes refused`
- [x] 2.3 Add the AES-256-GCM sealing with table, column, row and key version as additional data, and load every `kek_<n>` from `$CREDENTIALS_DIRECTORY`. Covers `key-service: Ciphertext moved to another row`
- [x] 2.4 Write `tero-keys.socket` and `tero-keys.service` with the design's sandbox; `tero.service` gets `Wants=`/`After=tero-keys.socket` and the journal rate limit. Covers `key-service: Process identity and sandbox`, `No network`, `Socket permissions`, `Restart while the dashboard waits`
- [x] 2.5 Init creates the `tero-keys` user, seals KEK version 1, installs and starts the socket before `tero.service`, and checks `status` answers. Covers `server-init: Key service running after init`, `tero-keys can't log in`, `Key service survives reboot`, `key-service: Key on disk is sealed`, `TPM used when present` (on EC2)

## 3. Credentials and the security log move

- [x] 3.1 Add `setup.issue`, `setup.begin`, `setup.finish`, `login.check` and `login.reset` in `tero-keys`, with replay protection and the code guessing limit. Covers `key-service: Secret encrypted at rest`, `Replayed code`, `Dashboard can't replace the credentials`, `Dashboard can't issue a link`, `Guessing codes with the password`, `Strangers can't lock the admin out`
- [x] 3.2 Switch dashboard setup and login to the key service; remove `admin`, `setup_links` and `security_events` from migration 0001; show the "key service isn't running" error. Covers `dashboard-access: Correct password and code`, `Correct password, wrong code`, `Setup without TOTP`, `Key service not answering`, `No credentials in the dashboard's database`
- [x] 3.3 Add `events.append` and `events.list`, the event types and who may append them, the journal copy, the per-caller and table limits and the prune; write unstored events to the journal. Covers every `security-log` scenario
- [x] 3.4 Route `sudo tero init`'s link and `sudo tero reset-login` through the root parent: `setup.issue` and `login.reset` on the key service, sessions, trusted browsers and throttles through the `_login-link` child; set unreadable keys aside and seal a new one when `tero-keys` failed with status 243. Covers `dashboard-access: Admin loses their authenticator`, `Server settings unaffected by reset`, `Master key lost`, `key-service: Key can't be opened`

## 4. Rotation and breach response

- [x] 4.1 Add `sudo tero rotate-key`: seal version n+1 through `keys.new/`, restart `tero-keys`, `key.rotate`, remove older files, restart. Covers `key-service: Rotation`, `Interrupted rotation`, `Apps keep serving during rotation`, `Not root`
- [x] 4.2 Add `sudo tero assume-breach` with the confirmation prompt, `--yes`, stopping and starting `tero.service`, the dashboard state and Caddy storage reset, the rotation from 4.1, the login reset, the persistence checks and the printed steps. Covers every `breach-response` scenario

## 5. Isolation tests, docs and end to end

- [ ] 5.1 Add e2e isolation tests run as `tero` inside `tero.service` and inside `tero`'s user manager, and e2e runs of `rotate-key`, `reset-login` with a corrupted key, and `assume-breach`. Covers `key-service: tero can't read the key`, `security-log: Dashboard compromised`, and every `process-isolation` scenario in this change
- [ ] 5.2 Run slices 1 to 3's e2e suites in both VMs and on EC2 (amd64 and arm64), and check the Lynis score has not dropped
- [x] 5.3 Update `docs/security/asvs-l2.md` (11.1.1, 11.3.1 to 11.3.3, 13.3.1 to 13.3.4 and 16.4.2 with tests; the Logging and Cryptography sections), `docs/security/cis-subset.md` for the new user, and ADR 0001's secrets section; move `openspec/config.yaml`'s context to slice 3
