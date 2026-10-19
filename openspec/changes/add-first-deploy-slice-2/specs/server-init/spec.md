## MODIFIED Requirements

### Requirement: Dashboard domain at init
Init SHALL ask for a dashboard domain, and SHALL fall back to `<public-ip>.sslip.io` when none is given. Init SHALL record the server's public IP in Tero's configuration whichever domain is chosen, for app addresses.

#### Scenario: Admin provides a domain
- **WHEN** the admin enters a domain at the init prompt
- **THEN** the dashboard is served over HTTPS on that domain
- **AND** the server's public IP is recorded in `/etc/tero/config.json`

#### Scenario: Admin skips the domain
- **WHEN** the admin leaves the domain prompt empty
- **THEN** the dashboard is served over HTTPS on `<public-ip>.sslip.io`
- **AND** the server's public IP is recorded in `/etc/tero/config.json`

## ADDED Requirements

### Requirement: Tero agent
Init SHALL install a systemd user unit for the Tero agent in the system-wide user unit directory, limited to the `tero` user, and enable it so it runs under `tero`'s user manager and starts on boot.

#### Scenario: Agent running after init
- **WHEN** init completes
- **THEN** the agent unit is active in `tero`'s user manager and its process runs as `tero`

#### Scenario: Agent survives reboot
- **WHEN** the server reboots after init
- **THEN** the agent is running again without manual steps
