package initflow

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/leanhanc/tero/internal/keys"
)

const (
	teroUser  = "tero"
	teroHome  = "/var/lib/tero"
	agentUser = keys.AgentUser
	agentHome = "/var/lib/tero-agent"
	noLogin   = "/usr/sbin/nologin"

	// sharedGroup lets the service and the agent open the database and build
	// logs in sharedDir, and nobody else.
	sharedGroup = "tero-shared"
	sharedDir   = "/var/lib/tero-shared"
	// keysCallersGroup owns the key service's socket. The key service still
	// identifies each caller itself; the group keeps everyone else from
	// connecting at all.
	keysCallersGroup = "tero-keys-callers"
)

// serviceAccount is a system user init creates for one of Tero's processes.
type serviceAccount struct {
	name string
	home string
	// purpose finishes "Tero needs that name for ...".
	purpose string
}

var (
	teroAccount  = serviceAccount{teroUser, teroHome, "the unprivileged user that runs its service"}
	agentAccount = serviceAccount{agentUser, agentHome, "the user that builds and runs apps"}
)

// sharedGroupMembers are the only users allowed in sharedGroup and
// keysCallersGroup.
var sharedGroupMembers = []string{teroUser, agentUser}

// createTeroUser creates the unprivileged user the service runs as. It has
// no subordinate ids and no user manager: it never runs a container.
func createTeroUser(ctx context.Context, h host) error {
	return ensureAccount(ctx, h, teroAccount)
}

// createAgentUser creates the user that owns every container. Its user
// manager starts later, in startAgentManager, once the user is in the shared
// groups.
func createAgentUser(ctx context.Context, h host) error {
	if err := ensureAccount(ctx, h, agentAccount); err != nil {
		return err
	}

	// Podman's storage, BuildKit's cache and the build directories live here.
	_, err := h.run.Run(ctx, "chmod", "0700", agentHome)
	return err
}

// startAgentManager gives tero-agent a subordinate id range big enough for
// rootless BuildKit, keeps its systemd user manager running without a
// login, and checks rootless Podman works for it. A user manager keeps the
// groups it started with, so one an earlier run left running is restarted
// to pick up the shared groups.
func startAgentManager(ctx context.Context, h host) error {
	hasNewRange, err := ensureSubIDRanges(ctx, h, agentUser)
	if err != nil {
		return err
	}

	agent, err := lookupAccount(h, agentUser)
	if err != nil {
		return err
	}

	userManager := fmt.Sprintf("user@%d.service", agent.uid)
	if _, err := h.run.Run(ctx, "systemctl", "try-restart", userManager); err != nil {
		return err
	}
	if err := enableLinger(ctx, h, agentUser, agent.uid); err != nil {
		return err
	}

	return checkRootlessPodman(ctx, h, agent.uid, hasNewRange)
}

// shareState creates the groups the service and the agent share, puts both
// users in them, and creates the shared directory. The directory is root's
// and setgid, so every file either process creates gets the group.
func shareState(ctx context.Context, h host) error {
	for _, name := range []string{sharedGroup, keysCallersGroup} {
		if err := ensureGroup(ctx, h, name); err != nil {
			return err
		}
	}

	memberOf := sharedGroup + "," + keysCallersGroup
	for _, member := range sharedGroupMembers {
		if _, err := h.run.Run(ctx, "usermod", "--append", "--groups", memberOf, member); err != nil {
			return err
		}
	}

	if _, err := h.run.Run(ctx, "install", "-d", "-o", "root", "-g", sharedGroup, "-m", "2770", sharedDir); err != nil {
		return err
	}

	// Only the agent writes build logs. Were the directory writable by tero,
	// the dashboard could plant a link where the agent's next log goes and
	// make the agent write into its own files.
	_, err := h.run.Run(ctx, "install", "-d", "-o", agentUser, "-g", sharedGroup, "-m", "2750",
		sharedDir+"/logs", sharedDir+"/logs/deploys")
	return err
}

func ensureGroup(ctx context.Context, h host, name string) error {
	groups, err := readGroups(h.files)
	if err != nil {
		return err
	}
	if _, exists := groups[name]; exists {
		return nil
	}

	_, err = h.run.Run(ctx, "groupadd", "--system", name)
	return err
}

// systemIDLimit is where Ubuntu starts regular user and group ids.
const systemIDLimit = 1000

// checkServiceAccount refuses init when a user or group of the account's
// name exists that isn't one an earlier init created: taking over someone's
// account would lock them out, and running a Tero process as a user with
// sudo would undo the isolation.
func checkServiceAccount(h host, service serviceAccount) error {
	accounts, err := readAccounts(h.files)
	if err != nil {
		return err
	}
	groups, err := readGroups(h.files)
	if err != nil {
		return err
	}

	existing, hasUser := accounts[service.name]
	if hasUser {
		return checkExistingUser(h, service, existing)
	}

	existingGroup, hasGroup := groups[service.name]
	isUsableGroup := existingGroup.gid < systemIDLimit && len(existingGroup.members) == 0
	if hasGroup && !isUsableGroup {
		return fmt.Errorf("A group named %s already exists (gid %d), and Tero needs that name for %s.\nInit stopped without changing anything. Rename or remove that group, then run init again", service.name, existingGroup.gid, service.purpose)
	}

	return nil
}

func checkExistingUser(h host, service serviceAccount, existing account) error {
	groupNames, err := groupsOf(h.files, existing)
	if err != nil {
		return err
	}

	isSystemUser := existing.uid < systemIDLimit && existing.home == service.home && existing.shell == noLogin
	hasSudo := slices.ContainsFunc(sudoGroups, func(name string) bool { return slices.Contains(groupNames, name) })
	if isSystemUser && !hasSudo {
		return nil
	}

	return fmt.Errorf("A user named %s already exists (uid %d, home %s), and Tero needs that name for %s.\nInit stopped without changing anything. Rename or remove that user, then run init again", service.name, existing.uid, existing.home, service.purpose)
}

// checkSharedGroups refuses init when a group Tero shares between its own
// users already exists with anyone else in it: they would read the database
// or reach the key service.
func checkSharedGroups(h host) error {
	groups, err := readGroups(h.files)
	if err != nil {
		return err
	}

	for _, name := range []string{sharedGroup, keysCallersGroup} {
		existing, exists := groups[name]
		if !exists {
			continue
		}

		hasOnlyTeroMembers := !slices.ContainsFunc(existing.members, func(member string) bool {
			return !slices.Contains(sharedGroupMembers, member)
		})
		if existing.gid < systemIDLimit && hasOnlyTeroMembers {
			continue
		}

		return fmt.Errorf("A group named %s already exists (gid %d), and Tero needs that name for a group only its own users belong to.\nInit stopped without changing anything. Rename or remove that group, then run init again", name, existing.gid)
	}

	return nil
}

// ensureAccount creates a system user, or reuses the one an earlier,
// interrupted init created (checkServiceAccount has vetted it).
func ensureAccount(ctx context.Context, h host, service serviceAccount) error {
	accounts, err := readAccounts(h.files)
	if err != nil {
		return err
	}
	groups, err := readGroups(h.files)
	if err != nil {
		return err
	}

	if _, exists := accounts[service.name]; !exists {
		groupOption := []string{"--user-group"}
		if _, hasGroup := groups[service.name]; hasGroup {
			groupOption = []string{"--gid", service.name}
		}

		args := append([]string{"--system"}, groupOption...)
		args = append(args, "--home-dir", service.home, "--create-home", "--shell", noLogin, service.name)
		if _, err := h.run.Run(ctx, "useradd", args...); err != nil {
			return err
		}
	}

	_, err = h.run.Run(ctx, "usermod", "--lock", service.name)
	return err
}

// ensureSubIDRanges gives owner subIDCount subordinate uids and gids, and
// reports whether it had to add them.
func ensureSubIDRanges(ctx context.Context, h host, owner string) (bool, error) {
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
	if hasLargeEnoughRange(uidRanges, owner) && hasLargeEnoughRange(gidRanges, owner) {
		return false, nil
	}

	if err := removeOwnRanges(ctx, h, owner, uidRanges, gidRanges); err != nil {
		return false, err
	}

	// One start for both files keeps uid and gid mappings aligned.
	allRanges := append(uidRanges, gidRanges...)
	start := nextFreeSubIDStart(allRanges, owner)
	span := fmt.Sprintf("%d-%d", start, start+subIDCount-1)

	_, err = h.run.Run(ctx, "usermod", "--add-subuids", span, "--add-subgids", span, owner)
	return true, err
}

func hasLargeEnoughRange(ranges []subIDRange, owner string) bool {
	for _, existing := range ranges {
		if existing.owner == owner && existing.count >= subIDCount {
			return true
		}
	}

	return false
}

func removeOwnRanges(ctx context.Context, h host, owner string, uidRanges, gidRanges []subIDRange) error {
	for _, existing := range uidRanges {
		if existing.owner == owner {
			span := fmt.Sprintf("%d-%d", existing.start, existing.end()-1)
			if _, err := h.run.Run(ctx, "usermod", "--del-subuids", span, owner); err != nil {
				return err
			}
		}
	}

	for _, existing := range gidRanges {
		if existing.owner == owner {
			span := fmt.Sprintf("%d-%d", existing.start, existing.end()-1)
			if _, err := h.run.Run(ctx, "usermod", "--del-subgids", span, owner); err != nil {
				return err
			}
		}
	}

	return nil
}

func lookupAccount(h host, name string) (account, error) {
	accounts, err := readAccounts(h.files)
	if err != nil {
		return account{}, err
	}

	found, exists := accounts[name]
	if !exists {
		return account{}, fmt.Errorf("user %s was not created", name)
	}

	return found, nil
}

func enableLinger(ctx context.Context, h host, name string, uid int) error {
	if _, err := h.run.Run(ctx, "loginctl", "enable-linger", name); err != nil {
		return err
	}

	runtimeDir := fmt.Sprintf("/run/user/%d", uid)
	isReady := waitFor(ctx, 30*time.Second, func() bool { return h.files.Exists(runtimeDir) })
	if !isReady {
		return fmt.Errorf("%s did not appear after enabling linger for %s", runtimeDir, name)
	}

	return nil
}

// checkRootlessPodman fails init early if Podman can't run as the agent's
// user. After a subordinate id change Podman must migrate its storage to the
// new mapping.
func checkRootlessPodman(ctx context.Context, h host, uid int, hasNewRange bool) error {
	asAgent := []string{"--user", agentUser, "--", "env",
		"HOME=" + agentHome, fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", uid)}

	if hasNewRange {
		migrate := append(append([]string{}, asAgent...), "podman", "system", "migrate")
		if _, err := h.run.Run(ctx, "runuser", migrate...); err != nil {
			return err
		}
	}

	info := append(append([]string{}, asAgent...), "podman", "info", "--format", "{{.Host.Security.Rootless}}")
	output, err := h.run.Run(ctx, "runuser", info...)
	if err != nil {
		return fmt.Errorf("rootless Podman doesn't work for %s: %w", agentUser, err)
	}
	if output != "true" {
		return fmt.Errorf("Podman is not running rootless for %s (got %q)", agentUser, output)
	}

	return nil
}
