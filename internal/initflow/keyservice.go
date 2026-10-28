package initflow

import (
	"context"
	"fmt"
	"slices"

	"github.com/leanhanc/tero/internal/keyfiles"
	"github.com/leanhanc/tero/internal/keys"
)

const keysHome = "/var/lib/tero-keys"

// forbiddenKeysGroups are groups the key service's user must never be in:
// Tero's other users' groups would share their files, the others would give
// it root or the journal.
var forbiddenKeysGroups = append([]string{teroUser, agentUser, sharedGroup, keysCallersGroup, "systemd-journal", "adm"}, sudoGroups...)

// checkKeysAccount refuses init when a tero-keys user or group exists that
// isn't one an earlier init created.
func checkKeysAccount(h host) error {
	accounts, err := readAccounts(h.files)
	if err != nil {
		return err
	}
	groups, err := readGroups(h.files)
	if err != nil {
		return err
	}

	existing, hasUser := accounts[keys.User]
	if !hasUser {
		existingGroup, hasGroup := groups[keys.User]
		isUsableGroup := existingGroup.gid < systemIDLimit && len(existingGroup.members) == 0
		if hasGroup && !isUsableGroup {
			return fmt.Errorf("A group named %s already exists (gid %d), and Tero needs that name for its key service.\nInit stopped without changing anything. Rename or remove that group, then run init again", keys.User, existingGroup.gid)
		}
		return nil
	}

	groupNames, err := groupsOf(h.files, existing)
	if err != nil {
		return err
	}
	isSystemUser := existing.uid < systemIDLimit && existing.home == keysHome && existing.shell == noLogin
	hasForbiddenGroup := slices.ContainsFunc(forbiddenKeysGroups, func(name string) bool { return slices.Contains(groupNames, name) })
	if isSystemUser && !hasForbiddenGroup {
		return nil
	}

	return fmt.Errorf("A user named %s already exists (uid %d, home %s), and Tero needs that name for its key service.\nInit stopped without changing anything. Rename or remove that user, then run init again", keys.User, existing.uid, existing.home)
}

// createKeysUser creates the system user the key service runs as, with no
// home directory of its own beyond the service's StateDirectory, no shell
// and no password.
func createKeysUser(ctx context.Context, h host) error {
	accounts, err := readAccounts(h.files)
	if err != nil {
		return err
	}
	groups, err := readGroups(h.files)
	if err != nil {
		return err
	}

	if _, exists := accounts[keys.User]; !exists {
		groupOption := []string{"--user-group"}
		if _, hasGroup := groups[keys.User]; hasGroup {
			groupOption = []string{"--gid", keys.User}
		}

		args := append([]string{"--system"}, groupOption...)
		args = append(args, "--no-create-home", "--home-dir", keysHome, "--shell", noLogin, keys.User)
		if _, err := h.run.Run(ctx, "useradd", args...); err != nil {
			return err
		}
	}

	_, err = h.run.Run(ctx, "usermod", "--lock", keys.User)
	return err
}

// keyFiles is keyfiles.Files on init's host.
func keyFiles(h host) keyfiles.Files {
	files := keyfiles.Host()
	files.Root = h.files.Root
	files.Run = h.run
	return files
}

// sealMasterKey seals the first master key, unless an earlier run did.
func sealMasterKey(ctx context.Context, h host) error {
	files := keyFiles(h)
	versions, err := files.Versions()
	if err != nil || len(versions) > 0 {
		return err
	}

	_, err = files.SealNext(ctx)
	return err
}

// startKeyService installs the key service's socket and unit, starts the
// socket and checks the service answers. It runs before the dashboard
// starts, so logins can be checked from the first one.
func startKeyService(ctx context.Context, h host) error {
	if _, err := writeEmbedded(h, "tero-keys.socket", "/etc/systemd/system/tero-keys.socket", 0o644); err != nil {
		return err
	}
	if _, err := writeEmbedded(h, "tero-keys.service", "/etc/systemd/system/tero-keys.service", 0o644); err != nil {
		return err
	}

	if err := runAll(ctx, h,
		[]string{"systemctl", "daemon-reload"},
		[]string{"systemctl", "enable", "tero-keys.socket"},
		[]string{"systemctl", "restart", "tero-keys.socket"},
		[]string{"systemctl", "try-restart", "tero-keys.service"},
	); err != nil {
		return err
	}

	if _, err := keyFiles(h).Keys.Status(ctx); err != nil {
		return fmt.Errorf("the key service doesn't answer: %w. See `journalctl -u tero-keys`", err)
	}

	return nil
}
