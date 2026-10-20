//go:build e2e

package source

import (
	"os"
	"path/filepath"
)

// e2eRepositories holds bare copies of GitHub repositories that the
// end-to-end suite puts in the test VM, so unpushed fixtures can be
// deployed.
const e2eRepositories = "/opt/tero-e2e/repos"

const e2eGitConfig = "/opt/tero-e2e/gitconfig"

// go-git serves file:// clones with the git binary, which refuses a
// repository owned by another user unless it is marked safe. The suite's
// copy belongs to the VM's login user, while the service and agent run as
// tero. Git before 2.44 only reads safe.directory from global or system
// config, so scripts/vm-test-repo.sh writes a global config for e2e builds.
func init() {
	if _, err := os.Stat(e2eGitConfig); err == nil {
		os.Setenv("GIT_CONFIG_GLOBAL", e2eGitConfig)
	}
}

// testOverride fetches a repository from the VM when the suite placed a
// copy there.
func testOverride(r Repository) (string, bool) {
	localPath := filepath.Join(e2eRepositories, r.Owner, r.Name+".git")
	if _, err := os.Stat(localPath); err != nil {
		return "", false
	}

	return "file://" + localPath, true
}
