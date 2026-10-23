## ADDED Requirements

### Requirement: Key service at init
Init SHALL create the system user `tero-keys` with no password and a non-login shell, seal the master key, install `tero-keys.socket` and `tero-keys.service`, and start them before the Tero service. The Tero service SHALL want the socket and start after it, without requiring it.

#### Scenario: Key service running after init
- **WHEN** init completes
- **THEN** `tero-keys.socket` is enabled and listening, `tero-keys` answers a `status` request, and `tero.service` lists the socket in `Wants=` and `After=` and not in `Requires=`

#### Scenario: tero-keys can't log in
- **WHEN** init completes
- **THEN** the `tero-keys` user has no password, a non-login shell, and is not in the `tero`, `sudo` or `systemd-journal` groups

#### Scenario: Key service survives reboot
- **WHEN** the server reboots after init
- **THEN** `tero-keys` answers with the same master key and the admin logs in with the same password and TOTP
