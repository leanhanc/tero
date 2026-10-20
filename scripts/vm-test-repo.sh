#!/bin/bash
# Snapshots this working tree, uncommitted changes included, as a bare git
# repository the e2e build of Tero fetches instead of github.com/leanhanc/tero.
# That lets the suite deploy fixtures that aren't pushed yet. The repository
# has two branches on the same commit: main, and e2e-redeploy, which the
# redeploy tests push new commits to.
#
#   vm-test-repo.sh <instance>
set -euo pipefail

instance="$1"
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

mkdir "$work/tree"
git -C "$repo_root" ls-files -co --exclude-standard -z |
	COPYFILE_DISABLE=1 tar -C "$repo_root" --null -T - -cf - | tar -C "$work/tree" -xf -

git -C "$work/tree" init -q -b main
git -C "$work/tree" add -A
git -C "$work/tree" -c user.name=tero-e2e -c user.email=e2e@tero.test commit -qm "e2e snapshot"
git -C "$work/tree" branch e2e-redeploy
git clone -q --bare "$work/tree" "$work/tero.git"
COPYFILE_DISABLE=1 tar -C "$work" -czf "$work/tero-git.tgz" tero.git

limactl copy "$work/tero-git.tgz" "$instance:/tmp/tero-git.tgz"
limactl shell --workdir / "$instance" bash -euo pipefail -c '
	sudo install -d -o "$(id -un)" -m 0755 /opt/tero-e2e /opt/tero-e2e/repos /opt/tero-e2e/repos/leanhanc
	tar -C /opt/tero-e2e/repos/leanhanc -xzf /tmp/tero-git.tgz
	printf "[safe]\n\tdirectory = *\n" >/opt/tero-e2e/gitconfig
	chmod -R a+rX /opt/tero-e2e'
