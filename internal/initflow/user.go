package initflow

import (
	"context"
	"fmt"
	"slices"
	"time"
)

const (
	teroUser = "tero"
	teroHome = "/var/lib/tero"
	noLogin  = "/usr/sbin/nologin"
)

// createTeroUser creates the unprivileged user that runs the service and owns
// every container, gives it a subordinate id range big enough for rootless
// BuildKit, and keeps its systemd user manager running without a login.
func createTeroUser(ctx context.Context, h host) error {
	if err := ensureAccount(ctx, h); err != nil {
		return err
	}

	hasNewRange, err := ensureSubIDRanges(ctx, h)
	if err != nil {
		return err
	}

	tero, err := lookupTero(h)
	if err != nil {
		return err
	}

	if err := enableLinger(ctx, h, tero.uid); err != nil {
		return err
	}

	return checkRootlessPodman(ctx, h, tero.uid, hasNewRange)
}

// systemIDLimit is where Ubuntu starts regular user and group ids.
const systemIDLimit = 1000

// checkTeroAccount refuses init when a tero user or group exists that isn't
// one an earlier init created: taking over someone's account would lock them
// out, and running the service as a user with sudo would undo the isolation.
func checkTeroAccount(h host) error {
	accounts, err := readAccounts(h.files)
	if err != nil {
		return err
	}
	groups, err := readGroups(h.files)
	if err != nil {
		return err
	}

	existing, hasUser := accounts[teroUser]
	if hasUser {
		return checkExistingTeroUser(h, existing)
	}

	existingGroup, hasGroup := groups[teroUser]
	isUsableGroup := existingGroup.gid < systemIDLimit && len(existingGroup.members) == 0
	if hasGroup && !isUsableGroup {
		return fmt.Errorf("A group named %s already exists (gid %d), and Tero needs that name for its own system group.\nInit stopped without changing anything. Rename or remove that group, then run init again", teroUser, existingGroup.gid)
	}

	return nil
}

func checkExistingTeroUser(h host, existing account) error {
	groupNames, err := groupsOf(h.files, existing)
	if err != nil {
		return err
	}

	isSystemUser := existing.uid < systemIDLimit && existing.home == teroHome && existing.shell == noLogin
	hasSudo := slices.ContainsFunc(sudoGroups, func(name string) bool { return slices.Contains(groupNames, name) })
	if isSystemUser && !hasSudo {
		return nil
	}

	return fmt.Errorf("A user named %s already exists (uid %d, home %s), and Tero needs that name for the unprivileged user that runs it.\nInit stopped without changing anything. Rename or remove that user, then run init again", teroUser, existing.uid, existing.home)
}

// ensureAccount creates the tero system user, or reuses the one an earlier,
// interrupted init created (checkTeroAccount has vetted it).
func ensureAccount(ctx context.Context, h host) error {
	accounts, err := readAccounts(h.files)
	if err != nil {
		return err
	}
	groups, err := readGroups(h.files)
	if err != nil {
		return err
	}

	if _, exists := accounts[teroUser]; !exists {
		groupOption := []string{"--user-group"}
		if _, hasGroup := groups[teroUser]; hasGroup {
			groupOption = []string{"--gid", teroUser}
		}

		args := append([]string{"--system"}, groupOption...)
		args = append(args, "--home-dir", teroHome, "--create-home", "--shell", noLogin, teroUser)
		if _, err := h.run.Run(ctx, "useradd", args...); err != nil {
			return err
		}
	}

	_, err = h.run.Run(ctx, "usermod", "--lock", teroUser)
	return err
}

// ensureSubIDRanges gives tero subIDCount subordinate uids and gids, and
// reports whether it had to add them.
func ensureSubIDRanges(ctx context.Context, h host) (bool, error) {
	subUIDs, err := h.files.ReadFile("/etc/subuid")
	if err != nil {
		return false, err
	}
	subGIDs, err := h.files.ReadFile("/etc/subgid")
	if err != nil {
		return false, err
	}

	uidRanges := parseSubIDs(string(subUIDs))
	gidRanges := parseSubIDs(string(subGIDs))
	if hasLargeEnoughRange(uidRanges) && hasLargeEnoughRange(gidRanges) {
		return false, nil
	}

	if err := removeOwnRanges(ctx, h, uidRanges, gidRanges); err != nil {
		return false, err
	}

	// One start for both files keeps uid and gid mappings aligned.
	allRanges := append(uidRanges, gidRanges...)
	start := nextFreeSubIDStart(allRanges, teroUser)
	span := fmt.Sprintf("%d-%d", start, start+subIDCount-1)

	_, err = h.run.Run(ctx, "usermod", "--add-subuids", span, "--add-subgids", span, teroUser)
	return true, err
}

func hasLargeEnoughRange(ranges []subIDRange) bool {
	for _, existing := range ranges {
		if existing.owner == teroUser && existing.count >= subIDCount {
			return true
		}
	}

	return false
}

func removeOwnRanges(ctx context.Context, h host, uidRanges, gidRanges []subIDRange) error {
	for _, existing := range uidRanges {
		if existing.owner == teroUser {
			span := fmt.Sprintf("%d-%d", existing.start, existing.end()-1)
			if _, err := h.run.Run(ctx, "usermod", "--del-subuids", span, teroUser); err != nil {
				return err
			}
		}
	}

	for _, existing := range gidRanges {
		if existing.owner == teroUser {
			span := fmt.Sprintf("%d-%d", existing.start, existing.end()-1)
			if _, err := h.run.Run(ctx, "usermod", "--del-subgids", span, teroUser); err != nil {
				return err
			}
		}
	}

	return nil
}

func lookupTero(h host) (account, error) {
	accounts, err := readAccounts(h.files)
	if err != nil {
		return account{}, err
	}

	tero, exists := accounts[teroUser]
	if !exists {
		return account{}, fmt.Errorf("user %s was not created", teroUser)
	}

	return tero, nil
}

func enableLinger(ctx context.Context, h host, uid int) error {
	if _, err := h.run.Run(ctx, "loginctl", "enable-linger", teroUser); err != nil {
		return err
	}

	runtimeDir := fmt.Sprintf("/run/user/%d", uid)
	isReady := waitFor(ctx, 30*time.Second, func() bool { return h.files.Exists(runtimeDir) })
	if !isReady {
		return fmt.Errorf("%s did not appear after enabling linger for %s", runtimeDir, teroUser)
	}

	return nil
}

// checkRootlessPodman fails init early if Podman can't run as tero. After a
// subordinate id change Podman must migrate its storage to the new mapping.
func checkRootlessPodman(ctx context.Context, h host, uid int, hasNewRange bool) error {
	asTero := []string{"--user", teroUser, "--", "env",
		"HOME=" + teroHome, fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", uid)}

	if hasNewRange {
		migrate := append(append([]string{}, asTero...), "podman", "system", "migrate")
		if _, err := h.run.Run(ctx, "runuser", migrate...); err != nil {
			return err
		}
	}

	info := append(append([]string{}, asTero...), "podman", "info", "--format", "{{.Host.Security.Rootless}}")
	output, err := h.run.Run(ctx, "runuser", info...)
	if err != nil {
		return fmt.Errorf("rootless Podman doesn't work for %s: %w", teroUser, err)
	}
	if output != "true" {
		return fmt.Errorf("Podman is not running rootless for %s (got %q)", teroUser, output)
	}

	return nil
}
