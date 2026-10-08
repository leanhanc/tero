package initflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// unneededUnits are network services a Tero host never needs. They are only
// disabled when installed; see docs/security/cis-subset.md.
var unneededUnits = []string{
	"avahi-daemon.service", "avahi-daemon.socket",
	"cups.service", "cups.socket", "cups-browsed.service",
	"rpcbind.service", "rpcbind.socket",
	"nfs-server.service", "smbd.service", "nmbd.service",
	"isc-dhcp-server.service", "slapd.service", "named.service",
	"vsftpd.service", "dovecot.service", "squid.service", "snmpd.service",
	"rsync.service", "telnet.socket", "xinetd.service",
	// Apport turns setuid core dumps back on (fs.suid_dumpable=2) when it starts.
	"apport.service", "apport-autoreport.path", "apport-forward.socket",
}

func disableUnneededServices(ctx context.Context, h host) error {
	for _, unit := range unneededUnits {
		loadState, err := h.run.Run(ctx, "systemctl", "show", "--property=LoadState", "--value", unit)
		if err != nil {
			return err
		}

		isInstalled := loadState != "not-found"
		if !isInstalled {
			continue
		}

		if _, err := h.run.Run(ctx, "systemctl", "disable", "--now", unit); err != nil {
			return err
		}
	}

	return nil
}

type filePermission struct {
	glob  string
	mode  os.FileMode
	group string
}

// sensitivePermissions are owned by root, with the group shown, and get at
// most the mode shown; see docs/security/cis-subset.md.
var sensitivePermissions = []filePermission{
	{"/etc/passwd", 0o644, "root"},
	{"/etc/passwd-", 0o644, "root"},
	{"/etc/group", 0o644, "root"},
	{"/etc/group-", 0o644, "root"},
	{"/etc/shadow", 0o640, "shadow"},
	{"/etc/shadow-", 0o640, "shadow"},
	{"/etc/gshadow", 0o640, "shadow"},
	{"/etc/gshadow-", 0o640, "shadow"},
	{"/etc/ssh/sshd_config", 0o600, "root"},
	{"/etc/ssh/ssh_host_*_key", 0o600, "root"},
	{"/etc/crontab", 0o600, "root"},
	{"/etc/cron.hourly", 0o700, "root"},
	{"/etc/cron.daily", 0o700, "root"},
	{"/etc/cron.weekly", 0o700, "root"},
	{"/etc/cron.monthly", 0o700, "root"},
	{"/etc/cron.d", 0o700, "root"},
}

func setSensitivePermissions(_ context.Context, h host) error {
	groups, err := readGroups(h.files)
	if err != nil {
		return err
	}

	for _, permission := range sensitivePermissions {
		owningGroup, exists := groups[permission.group]
		if !exists {
			return fmt.Errorf("group %q not found", permission.group)
		}

		matches, err := filepath.Glob(h.files.Path(permission.glob))
		if err != nil {
			return err
		}

		for _, match := range matches {
			if err := os.Chown(match, 0, owningGroup.gid); err != nil {
				return err
			}
			if err := os.Chmod(match, permission.mode); err != nil {
				return err
			}
		}
	}

	return nil
}
