#!/bin/bash
# Runs the end-to-end suite on a fresh cloud server instead of a Lima VM: sets
# up the same test certificate authority, fake release server and repository
# snapshot as vm-test.sh, then runs the suite over SSH. The server must be a
# fresh supported Ubuntu that `tero init` has never run on, and init's
# firewall keeps SSH open, so it can be any VPS.
#
#   scripts/ec2-test.sh ubuntu@<ip> <private key>
set -euo pipefail

export TERO_SSH="$1" TERO_SSH_KEY="$2"
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
pebble_version="v2.10.1"
cd "$repo_root"

# remote runs a command on the server from /, so tero can enter the folder.
remote() {
	ssh -i "$TERO_SSH_KEY" -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$TERO_SSH" "cd / && $*"
}

case "$(remote uname -m)" in
x86_64) arch=amd64 ;;
aarch64) arch=arm64 ;;
*) echo "unsupported architecture" >&2 && exit 1 ;;
esac

make build-e2e E2E_ARCH="$arch"

pebble_bin="$(go env GOPATH)/bin"
if [[ "$(go env GOHOSTOS)" != "linux" || "$(go env GOHOSTARCH)" != "$arch" ]]; then
	pebble_bin+="/linux_$arch"
fi
GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go install \
	"github.com/letsencrypt/pebble/v2/cmd/pebble@$pebble_version" \
	"github.com/letsencrypt/pebble/v2/cmd/pebble-challtestsrv@$pebble_version"

scp -q -i "$TERO_SSH_KEY" bin/tero-e2e-linux-$arch "$pebble_bin/pebble" "$pebble_bin/pebble-challtestsrv" \
	scripts/vm-test-ca.sh scripts/vm-test-release.sh install.sh "$TERO_SSH:/tmp/"
remote sudo bash /tmp/vm-test-ca.sh
remote sudo bash /tmp/vm-test-release.sh "$arch"
scripts/vm-test-repo.sh

go test -tags e2e -count=1 -v -timeout 90m ./e2e/...
