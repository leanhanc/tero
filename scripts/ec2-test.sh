#!/bin/bash
# Runs the end-to-end suite on a cloud server instead of a Lima VM: sets up the
# same test certificate authority, fake release server and repository snapshot
# as vm-test.sh, then runs the suite over SSH. The server must be a fresh
# supported Ubuntu that `tero init` has never run on, and init's firewall keeps
# SSH open, so it can be any VPS. scripts/ec2-e2e.sh creates such servers on
# EC2 and calls this script for each.
#
#   scripts/ec2-test.sh ubuntu@<ip> <private key>
#   scripts/ec2-test.sh build <arch>    only build what a run copies to the server
#
# With TERO_E2E_PREBUILT=1 a run skips the build, for runs in parallel after
# `build` has run once per architecture.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
pebble_version="v2.10.1"
cd "$repo_root"

# pebble_dir prints where go install puts pebble for the server's
# architecture: binaries for another platform go in a linux_<arch> directory.
pebble_dir() {
	local arch="$1"
	local dir
	dir="$(go env GOPATH)/bin"
	if [[ "$(go env GOHOSTOS)" != "linux" || "$(go env GOHOSTARCH)" != "$arch" ]]; then
		dir+="/linux_$arch"
	fi
	echo "$dir"
}

build() {
	local arch="$1"
	make build-e2e E2E_ARCH="$arch"
	GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -o "bin/fake-github-linux-$arch" ./e2e/fakegithub
	GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -o "bin/fake-probe-linux-$arch" ./e2e/fakeprobe
	GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go install \
		"github.com/letsencrypt/pebble/v2/cmd/pebble@$pebble_version" \
		"github.com/letsencrypt/pebble/v2/cmd/pebble-challtestsrv@$pebble_version"
}

# remote runs a command on the server from /, so tero can enter the folder.
remote() {
	ssh -i "$TERO_SSH_KEY" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$TERO_SSH" "cd / && $*"
}

if [[ "${1:-}" == "build" ]]; then
	build "$2"
	exit 0
fi

export TERO_SSH="$1" TERO_SSH_KEY="$2"

case "$(remote uname -m)" in
x86_64) arch=amd64 ;;
aarch64) arch=arm64 ;;
*) echo "unsupported architecture" >&2 && exit 1 ;;
esac

if [[ "${TERO_E2E_PREBUILT:-}" != "1" ]]; then
	build "$arch"
fi
pebble_bin="$(pebble_dir "$arch")"

scp -q -i "$TERO_SSH_KEY" bin/tero-e2e-linux-$arch "$pebble_bin/pebble" "$pebble_bin/pebble-challtestsrv" \
	scripts/vm-test-ca.sh scripts/vm-test-release.sh install.sh "$TERO_SSH:/tmp/"
scp -q -i "$TERO_SSH_KEY" "bin/fake-github-linux-$arch" "$TERO_SSH:/tmp/fake-github"
scp -q -i "$TERO_SSH_KEY" "bin/fake-probe-linux-$arch" "$TERO_SSH:/tmp/fake-probe"
remote sudo bash /tmp/vm-test-ca.sh
remote sudo bash /tmp/vm-test-release.sh "$arch"
scripts/vm-test-repo.sh

go test -tags e2e -count=1 -v -timeout 90m ./e2e/...
