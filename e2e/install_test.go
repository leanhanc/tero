//go:build e2e

package e2e

import (
	"strings"
	"testing"
)

const (
	// releaseServer serves fake releases in the VM, started by
	// scripts/vm-test-release.sh.
	releaseServer = "https://localhost:8443"
	releaseCACert = "/opt/tero-e2e-release/cert.pem"
)

// testInstall runs the install script scenarios on the fresh VM. The last one
// installs the binary the rest of the suite uses.
func testInstall(t *testing.T) {
	t.Run("installation: Untested OS", testInstallUntestedOS)
	t.Run("installation: Untested architecture", testInstallUntestedArch)
	t.Run("installation: Checksum mismatch", testInstallChecksumMismatch)

	hostArch := strings.NewReplacer("x86_64", "amd64", "aarch64", "arm64").Replace(must(t, "uname -m"))
	t.Run("installation: Install on supported "+hostArch+" host / Fresh install / Install script does not configure the server", testFreshInstall)
}

// installCommand pipes install.sh from a fake release into sh, the way the
// README's one-liner does, with downloads pointed at that release.
func installCommand(release string) string {
	releaseURL := releaseServer + "/" + release
	return `curl -fsSL --cacert ` + releaseCACert + ` ` + releaseURL + `/install.sh | ` +
		`TERO_DOWNLOAD_URL=` + releaseURL + ` CURL_CA_BUNDLE=` + releaseCACert + ` sh`
}

// testInstallUntestedOS runs the install in a mount namespace where
// /etc/os-release describes Ubuntu 22.04, leaving the real file untouched.
func testInstallUntestedOS(t *testing.T) {
	before := etcChecksum(t)

	got := run(t, `printf 'ID=ubuntu\nVERSION_ID="22.04"\nPRETTY_NAME="Ubuntu 22.04.5 LTS"\n' >/tmp/os-release-22.04
		curl -fsSL --cacert `+releaseCACert+` `+releaseServer+`/good/install.sh >/tmp/install.sh
		sudo unshare --mount sh -c 'mount --bind /tmp/os-release-22.04 /etc/os-release && sh /tmp/install.sh'`)
	assertRefusedWithTestedList(t, got, "Ubuntu 22.04.5 LTS")

	if after := etcChecksum(t); after != before {
		t.Fatal("a refused install changed files under /etc or /usr/local")
	}
}

// testInstallUntestedArch puts a uname that reports riscv64 first on the PATH.
func testInstallUntestedArch(t *testing.T) {
	before := etcChecksum(t)

	must(t, `mkdir -p /tmp/fake-uname
		printf '#!/bin/sh\necho riscv64\n' >/tmp/fake-uname/uname
		chmod +x /tmp/fake-uname/uname`)

	got := run(t, `curl -fsSL --cacert `+releaseCACert+` `+releaseServer+`/good/install.sh |
		PATH=/tmp/fake-uname:$PATH TERO_DOWNLOAD_URL=`+releaseServer+`/good CURL_CA_BUNDLE=`+releaseCACert+` sh`)
	assertRefusedWithTestedList(t, got, "(riscv64)")

	if after := etcChecksum(t); after != before {
		t.Fatal("a refused install changed files under /etc or /usr/local")
	}
}

func assertRefusedWithTestedList(t *testing.T, got result, platform string) {
	t.Helper()

	if got.exitCode == 0 {
		t.Fatalf("install succeeded on %s:\n%s", platform, got.output)
	}
	assertContains(t, got.output, platform, "installation has been aborted",
		"Ubuntu 26.04 LTS on amd64 or arm64", "Ubuntu 24.04 LTS on amd64 or arm64")
}

func testInstallChecksumMismatch(t *testing.T) {
	before := etcChecksum(t)

	got := run(t, installCommand("tampered"))
	if got.exitCode == 0 {
		t.Fatalf("install accepted a tampered binary:\n%s", got.output)
	}
	assertContains(t, got.output, "corrupted or tampered with", "run the install again", "verify the release by hand")

	if after := etcChecksum(t); after != before {
		t.Fatal("a refused install changed files under /etc or /usr/local")
	}
}

// testFreshInstall checks that the install adds /usr/local/bin/tero and
// nothing else: with that one file moved aside, /etc, /usr/local, the
// installed packages and the firewall rules match their state from before.
func testFreshInstall(t *testing.T) {
	const systemState = `dpkg-query -W -f '${Package} ${Version} ${Status}\n' | sha256sum; sudo nft -s list ruleset | sha256sum`
	etcBefore := etcChecksum(t)
	systemBefore := must(t, systemState)

	got := run(t, installCommand("good"))
	if got.exitCode != 0 {
		t.Fatalf("install failed (%d):\n%s", got.exitCode, got.output)
	}
	assertContains(t, got.output, "sudo tero init")
	t.Logf("install output:\n%s", got.output)

	assertInstalledBinaryRuns(t)

	must(t, "sudo mv /usr/local/bin/tero /tmp/tero-installed")
	etcAfter := etcChecksum(t)
	must(t, "sudo mv /tmp/tero-installed /usr/local/bin/tero")
	if etcAfter != etcBefore {
		t.Error("the install changed files under /etc or /usr/local besides /usr/local/bin/tero")
	}
	if systemAfter := must(t, systemState); systemAfter != systemBefore {
		t.Error("the install changed installed packages or firewall rules")
	}
	if run(t, "id tero").exitCode == 0 {
		t.Error("the install created the tero user")
	}
}

// assertInstalledBinaryRuns checks that the binary for this VM's
// architecture is on the PATH and executable.
func assertInstalledBinaryRuns(t *testing.T) {
	t.Helper()

	path := must(t, "command -v tero")
	if path != "/usr/local/bin/tero" {
		t.Errorf("tero is at %q, want /usr/local/bin/tero", path)
	}
	if mode := must(t, "stat -c '%a %U' /usr/local/bin/tero"); mode != "755 root" {
		t.Errorf("/usr/local/bin/tero mode and owner = %q, want 755 root", mode)
	}
	must(t, "tero version")
}
