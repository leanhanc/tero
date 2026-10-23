## ADDED Requirements

### Requirement: Key service process
Tero SHALL run a key service, `tero keys`, as the system unit `tero-keys.service` under the system user `tero-keys`, started through `tero-keys.socket`. It SHALL have no network access and no capabilities, and SHALL accept requests only on the Unix socket `/run/tero-keys/keys.sock`.

#### Scenario: Process identity and sandbox
- **WHEN** `tero-keys` is running
- **THEN** its main process uid is `tero-keys`, its effective, permitted and bounding capability sets are empty, and `NoNewPrivileges` is set

#### Scenario: No network
- **WHEN** `tero-keys` is running
- **THEN** it has no TCP or UDP socket and its network namespace has only a loopback interface

#### Scenario: Socket permissions
- **WHEN** init has completed
- **THEN** `/run/tero-keys/keys.sock` is owned by `tero-keys`, group `tero`, mode 0660

#### Scenario: Restart while the dashboard waits
- **WHEN** `tero-keys` restarts while the dashboard sends a request
- **THEN** the request is answered once `tero-keys` is back, and `tero.service` keeps running

### Requirement: Callers identified at connect time
The key service SHALL accept a connection only from root or from the dashboard, decided when the connection opens: root by uid 0 as recorded at `connect()`, the dashboard by uid `tero` and the exact cgroup `/system.slice/tero.service` of a process still running. Each caller SHALL be limited to its own requests. Limits SHALL bound request size (64 KiB), idle time (5 seconds) and concurrent connections (32 per caller, counted only once the caller is identified).

#### Scenario: Other process running as tero
- **WHEN** a process running as `tero` outside `tero.service`, for example in `tero`'s user manager, connects
- **THEN** the connection is closed without an answer and counted in a `callers_refused` event

#### Scenario: Request outside the caller's role
- **WHEN** the dashboard sends `key.rotate`
- **THEN** the request is refused and logged

#### Scenario: Oversized or idle request
- **WHEN** a caller sends a request larger than 64 KiB, or nothing for 5 seconds
- **THEN** the connection is closed without processing a request

### Requirement: Sealed master key
Init SHALL generate a 256-bit master key and seal it with `systemd-creds`, using the TPM2 and the host key when a TPM2 is present and the host key otherwise, with no PCR policy. Only `tero-keys` SHALL receive the decrypted key, through `LoadCredentialEncrypted=`. No request SHALL return it (ASVS v5.0.0-13.3.3).

#### Scenario: Key on disk is sealed
- **WHEN** init completes
- **THEN** every file under `/etc/tero/keys/` is a `systemd-creds` ciphertext named by its version, owned by root, mode 0600

#### Scenario: tero can't read the key
- **WHEN** a process running as `tero` tries to read `tero-keys`' credential directory, its memory, or `/var/lib/tero-keys/`
- **THEN** every attempt fails with a permission error

#### Scenario: TPM used when present
- **WHEN** init runs on a server with a TPM2
- **THEN** `systemd-creds` reports the master key as sealed with `tpm2+host`

#### Scenario: Key can't be opened
- **WHEN** a file under `/etc/tero/keys/` can't be decrypted at boot
- **THEN** `tero-keys` doesn't start, the dashboard and every app keep serving, and login says the key service isn't running and to run `sudo tero reset-login`

### Requirement: Admin credentials held by the key service
The admin's password hash, TOTP secret and setup links SHALL be stored and checked only by the key service. TOTP secrets SHALL be encrypted with AES-256-GCM under the master key, with the table, column, row and key version as additional data. A stored TOTP secret or password hash SHALL never leave the key service (ASVS v5.0.0-13.3.1).

#### Scenario: Secret encrypted at rest
- **WHEN** the admin has completed setup
- **THEN** neither `tero`'s database nor `tero-keys`' database holds the TOTP secret in clear, and `tero`'s database holds no password hash

#### Scenario: Ciphertext moved to another row
- **WHEN** a sealed TOTP secret is copied into another row's column
- **THEN** it doesn't decrypt there

#### Scenario: Replayed code
- **WHEN** a code that already logged the admin in is sent to `login.check` again
- **THEN** it fails

### Requirement: Setup only through a root-issued link
Only root SHALL issue a setup link. A setup SHALL be possible only with a valid, unused link while no admin exists. A new TOTP secret SHALL be returned once, during setup, for the QR code.

#### Scenario: Dashboard can't replace the credentials
- **WHEN** the admin exists and the dashboard sends `setup.begin` or `setup.finish` with any token
- **THEN** the request is refused and the admin's password and TOTP keep working

#### Scenario: Dashboard can't issue a link
- **WHEN** the dashboard sends `setup.issue`
- **THEN** the request is refused

### Requirement: Code guessing limit in the key service
The key service SHALL refuse `login.check` for 24 hours once 20 checks within 24 hours had the right password and a wrong code, independent of the dashboard's throttling. Checks with a wrong password SHALL NOT count toward it.

#### Scenario: Guessing codes with the password
- **WHEN** 20 `login.check` requests within 24 hours have the right password and a wrong code
- **THEN** `tero-keys` refuses `login.check` for the next 24 hours, whatever the dashboard's own throttling says, and has recorded a `totp_failed` event for each and a `totp_locked` event

#### Scenario: Guessing passwords from the dashboard
- **WHEN** the dashboard sends more than 30 `login.check` requests in one minute
- **THEN** `tero-keys` refuses the rest as busy without checking them, records one `rate_limited` event with their count, and lists wrong codes with the right password to the dashboard as `login_failed`

#### Scenario: Strangers can't lock the admin out
- **WHEN** any number of `login.check` requests have a wrong password
- **THEN** they don't count toward the 24-hour limit, and the admin can still log in from a trusted browser

### Requirement: Master key rotation
`sudo tero rotate-key` SHALL replace the master key, re-encrypt everything stored under it and remove the old key, such that an interruption at any point leaves every stored value decryptable (ASVS v5.0.0-13.3.4).

#### Scenario: Rotation
- **WHEN** the admin runs `sudo tero rotate-key`
- **THEN** a new key version is sealed, every encrypted row names the new version, the old key file is removed, and the admin can still log in with the same TOTP

#### Scenario: Interrupted rotation
- **WHEN** `rotate-key` stops after sealing the new key and before removing the old one
- **THEN** `tero-keys` still decrypts every row, and running `rotate-key` again completes the rotation

#### Scenario: Apps keep serving during rotation
- **WHEN** `rotate-key` runs while apps are live
- **THEN** `tero.service` isn't restarted and every app keeps answering

#### Scenario: Not root
- **WHEN** a user runs `tero rotate-key` without root
- **THEN** it exits with a non-zero status and tells the user to use `sudo`
