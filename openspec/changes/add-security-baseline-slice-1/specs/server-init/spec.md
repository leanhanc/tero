## MODIFIED Requirements

### Requirement: Host hardening
Init SHALL harden the host to a documented subset of the CIS Benchmark Level 1 Server profile for the host's distro before Tero starts serving traffic. The subset, its CIS IDs per distro, and every skipped control with its reason are listed in `security/cis-subset.md`. The subset covers SSH, the firewall, automatic security updates, kernel settings, auditing, AppArmor, unneeded services, and permissions on sensitive files.

#### Scenario: Hardening applied
- **WHEN** init completes
- **THEN** every control in the CIS subset for the host's distro is in effect

#### Scenario: Lynis score in end-to-end tests
- **WHEN** the end-to-end suite runs Lynis on a freshly initialized server
- **THEN** the hardening index is at or above the minimum score set in the suite

#### Scenario: SSH locked down
- **WHEN** init completes
- **THEN** SSH refuses root login and password authentication, and offers only ciphers, MACs and key exchange algorithms on the subset's allow list

#### Scenario: Firewall default deny
- **WHEN** init completes
- **THEN** the host firewall drops inbound traffic by default and allows only ports 22, 80 and 443

#### Scenario: Automatic security updates
- **WHEN** init completes
- **THEN** unattended installation of the distro's security updates is enabled

#### Scenario: Kernel settings
- **WHEN** init completes
- **THEN** the sysctls in the subset are set and persist across reboots, including `kernel.kptr_restrict`, `kernel.dmesg_restrict`, reverse-path filtering, and refusing ICMP redirects

#### Scenario: Auditing and AppArmor
- **WHEN** init completes
- **THEN** auditd is running with the subset's rule set and AppArmor is enabled in enforcing mode

#### Scenario: Unneeded services and file permissions
- **WHEN** init completes
- **THEN** the services the subset marks as unneeded are disabled, and the sensitive files it lists have the owners and modes it specifies

## ADDED Requirements

### Requirement: SSH lockout guard
Init SHALL NOT turn off SSH password authentication unless at least one user who can run `sudo` has an SSH public key in their `authorized_keys`. In that case it SHALL stop before changing anything, explaining how to add a key and re-run init.

#### Scenario: No SSH key configured
- **WHEN** `sudo tero init` runs and no sudo-capable user has an authorized SSH key
- **THEN** init exits with a non-zero status before making any change
- **AND** the message explains that password SSH login is about to be disabled, how to add a key, and to run `sudo tero init` again

#### Scenario: SSH key present
- **WHEN** `sudo tero init` runs and the invoking sudo user has an authorized SSH key
- **THEN** init proceeds and disables SSH password authentication
