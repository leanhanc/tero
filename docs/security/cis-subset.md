# CIS subset applied by `tero init`

`sudo tero init` applies the controls below from the CIS Benchmark Level 1 Server profile. Tero doesn't claim CIS compliance: this is a chosen subset, and the controls it leaves out are listed with the reason. ADR 0001 explains why CIS is the standard for the host.

The Ubuntu column maps to the CIS Ubuntu Linux 24.04 LTS benchmark, by section, and covers both tested releases, 24.04 and 26.04. CIS has no 26.04 benchmark yet. The section numbers follow v1.0.0 of that benchmark and need to be checked against v2.0.0, which renumbers some controls. Adding a distro means adding a column here and a row in `internal/platform`.

The end-to-end suite (`e2e/init_test.go`) checks every applied control and reports the Lynis hardening index.

## Applied

| Topic | Control | Ubuntu (CIS 24.04 section) | Where | Checked by |
|---|---|---|---|---|
| SSH | No root login | 5.1 (PermitRootLogin) | `files/sshd-hardening.conf` | `SSH locked down` |
| SSH | No password or keyboard-interactive login, no empty passwords | 5.1 (PasswordAuthentication, PermitEmptyPasswords) | `files/sshd-hardening.conf` | `SSH locked down` |
| SSH | Strong ciphers, MACs and key exchange only | 5.1 (Ciphers, MACs, KexAlgorithms) | `files/sshd-hardening.conf` | `SSH locked down` |
| SSH | No host-based auth, rhosts, user environment or X11 forwarding | 5.1 | `files/sshd-hardening.conf` | `SSH locked down` |
| SSH | MaxAuthTries 4, MaxSessions 10, MaxStartups 10:30:60, LoginGraceTime 60, client alive 300s × 3, LogLevel VERBOSE | 5.1 | `files/sshd-hardening.conf` | `SSH locked down` |
| SSH | `sshd_config` and host private keys readable by root only | 5.1 (file permissions) | `services.go` | `Unneeded services and file permissions` |
| Firewall | Default deny inbound; allow replies, loopback, essential ICMP, DHCP, the SSH server's ports, 80, 443. Stopping the unit keeps the rules, and the service only starts behind them | 4 (host-based firewall, nftables) | `files/firewall.nft` | `Firewall default deny` |
| Updates | Security updates installed automatically | 1.2.2 | `files/20auto-upgrades` | `Automatic security updates` |
| Kernel | ASLR on, no setuid core dumps, ptrace restricted, kernel pointers and dmesg hidden | 1.5 | `files/sysctl-hardening.conf` | `Kernel settings` |
| Kernel | Core dumps disabled with a hard limit for every user | 1.5 | `files/limits-core.conf` | `Kernel settings` |
| Network | No IP forwarding, redirects or source routing; reverse-path filtering; log martians; SYN cookies; ignore broadcast and bogus ICMP | 3.3 | `files/sysctl-hardening.conf` | `Kernel settings` |
| Auditing | auditd installed and running | 6.3.1 | `steps.go` | `Auditing and AppArmor` |
| Auditing | Changes to sudoers, users and groups, network identity, AppArmor policy, kernel modules, logins, SSH config, Tero's config and binary are recorded; syscall rules cover both the 64-bit and 32-bit interfaces | 6.3.3 | `files/audit.rules` | `Auditing and AppArmor` |
| AppArmor | Installed and enabled; init stops if it isn't | 1.3.1 | `steps.go` | `Auditing and AppArmor` |
| Services | apport (it re-enables setuid core dumps), avahi, cups, rpcbind, NFS, Samba, DHCP server, LDAP, DNS, FTP, IMAP, Squid, SNMP, rsync daemon, telnet and xinetd disabled when installed | 2.1 | `services.go` | `Unneeded services and file permissions` |
| Files | `/etc/passwd`, `group`, `shadow`, `gshadow` and their backups owned by root with CIS modes | 7.1 | `services.go` | `Unneeded services and file permissions` |
| Files | `/etc/crontab` and `/etc/cron.*` readable by root only | 2.4.1 | `services.go` | `Unneeded services and file permissions` |

## Skipped

| Control | CIS 24.04 section | Why it's skipped |
|---|---|---|
| Separate partitions for `/tmp`, `/var`, `/var/log`, `/var/log/audit`, `/home` | 1.1.2 | VPS images come with a single disk. |
| Bootloader password | 1.4 | Nobody on a VPS sees the boot menu, and a password would block the provider's rescue console. |
| Disable IPv6 router advertisements (`accept_ra`) | 3.3 | Many providers configure IPv6 with router advertisements. Turning them off would break IPv6 on those servers. |
| Make the audit configuration immutable (`-e 2`) | 6.3.3 | Rule changes would then need a reboot, and init has to be able to re-apply its rules after a partial run. |
| auditd disk-full actions (halt or single-user mode) | 6.3.2 | Stopping a single server whose only job is serving apps causes an outage. Default log rotation is kept. |
| AppArmor profiles for every process in enforce mode | 1.3.1 (Level 2) | That is a Level 2 control. Ubuntu's shipped profiles stay as they are. |
| Login banner, `/etc/issue` and MOTD content | 1.6 | It doesn't change security, and it's the provider's or admin's choice. |
| Password aging, complexity and lockout (PAM) | 5.3 to 5.4 | Password login is turned off for SSH, and the dashboard has its own policy (ASVS V6.2). |
| `AllowTcpForwarding no` | 5.1 (Level 2) | That is a Level 2 control, and admins rely on SSH tunnels to reach services. |
| journald and rsyslog remote logging | 6.1 to 6.2 | A single server has no log server to send to. Local logging stays at Ubuntu's defaults. |
| AIDE file-integrity monitoring | 6.1 | Unattended upgrades change files every day, so it would create constant noise on a single unattended box. This can be reconsidered later. |
