## MODIFIED Requirements

### Requirement: Assume-breach command
`sudo tero assume-breach` SHALL, after confirmation, stop the dashboard, `tero-agent`'s user manager and every other process running as `tero` or `tero-agent`, move aside the `~/.config` of `tero` and `tero-agent`, Podman's quadlet directory and any crontab for either, remove BuildKit's cache volume and image, delete Caddy's stored certificates, private keys and ACME account, delete the GitHub connection, rotate the master key, reset the admin's password, TOTP and passkeys together with every session, trusted browser and login throttle, record a `breach_reset` event, start `tero-agent`'s user manager and the dashboard again, and print a new setup link and what the admin has to do outside the server. It SHALL be available only as a root command on the server.

#### Scenario: Confirmed run
- **WHEN** the admin runs `sudo tero assume-breach` and types `yes`
- **THEN** existing sessions and trusted browsers stop working, the old password, TOTP and passkeys fail, the master key version changes, the dashboard serves a newly issued certificate, and the output ends with an HTTPS setup link on the dashboard domain

#### Scenario: GitHub connection deleted
- **WHEN** `assume-breach` runs with GitHub connected
- **THEN** the App's private key and webhook secret are deleted, signed webhooks are refused, and the settings page offers Connect GitHub again

#### Scenario: Downtime said up front
- **WHEN** `assume-breach` asks for confirmation
- **THEN** the prompt says apps are down until they start again

#### Scenario: Nothing survives in the dashboard's memory
- **WHEN** `assume-breach` resets the login
- **THEN** `tero.service` and `tero-agent`'s user manager were stopped and no process ran as `tero` or `tero-agent` before the reset, and they started only after the new link was issued

#### Scenario: What to do elsewhere
- **WHEN** `assume-breach` finishes
- **THEN** the output lists, before the setup link: install the latest Tero, change the password anywhere it was reused, remove the old Tero entry from the authenticator app and the old Tero passkeys from the admin's devices, delete the GitHub App by name at its settings page on GitHub (`https://github.com/settings/apps/<slug>`, or under `/organizations/<org>/` for an organization) and connect GitHub again (when one was connected), check SSH's authorized keys, redeploy every app, and each unit file or crontab it moved to `/var/lib/tero-breach/`, which is readable only by root

#### Scenario: Server without a TPM
- **WHEN** `assume-breach` runs on a server whose master key is sealed with the host key only
- **THEN** the output says a copy of the disk taken before the run can still open the new master key

#### Scenario: Not confirmed
- **WHEN** the admin answers anything other than `yes`
- **THEN** nothing changes and the command exits with a non-zero status

#### Scenario: Scripted run
- **WHEN** the admin runs `sudo tero assume-breach --yes`
- **THEN** it runs without asking

#### Scenario: Not root
- **WHEN** a user runs `tero assume-breach` without root
- **THEN** it exits with a non-zero status and tells the user to use `sudo`
