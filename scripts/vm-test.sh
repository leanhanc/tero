#!/bin/bash
# Builds Tero for the test VMs and, for each tested Ubuntu release, recreates a
# VM from lima/tero-ubuntu-<release>.yaml, sets up a local test certificate
# authority in it, and runs the end-to-end suite.
#
#   scripts/vm-test.sh                       full run on fresh VMs, every release
#   scripts/vm-test.sh 24.04                 full run on one release
#   TERO_VM_KEEP=1 scripts/vm-test.sh 26.04  reuse the running VM (init must not have run yet)
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
pebble_version="v2.10.1"
releases=("$@")
if [[ ${#releases[@]} -eq 0 ]]; then
	releases=(26.04 24.04)
fi
cd "$repo_root"

# The VMs run on the host's architecture.
arch="$(go env GOHOSTARCH)"
make build-e2e E2E_ARCH="$arch"

# go install puts binaries for another platform in a linux_<arch> subdirectory.
pebble_bin="$(go env GOPATH)/bin"
if [[ "$(go env GOHOSTOS)" != "linux" ]]; then
	pebble_bin+="/linux_$arch"
fi
GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go install \
	"github.com/letsencrypt/pebble/v2/cmd/pebble@$pebble_version" \
	"github.com/letsencrypt/pebble/v2/cmd/pebble-challtestsrv@$pebble_version"

failed=()
for release in "${releases[@]}"; do
	instance="tero-ubuntu-${release//./-}"
	echo "=== Ubuntu $release ($instance)"

	if [[ "${TERO_VM_KEEP:-}" != "1" ]]; then
		limactl delete --force "$instance" >/dev/null 2>&1 || true
		limactl create --tty=false --name="$instance" "lima/tero-ubuntu-$release.yaml"
	fi
	limactl start --tty=false "$instance"

	limactl copy bin/tero-e2e-linux-$arch "$pebble_bin/pebble" "$pebble_bin/pebble-challtestsrv" \
		scripts/vm-test-ca.sh "$instance:/tmp/"
	limactl shell --workdir / "$instance" sudo bash /tmp/vm-test-ca.sh
	limactl shell --workdir / "$instance" sudo install -m 0755 "/tmp/tero-e2e-linux-$arch" /usr/local/bin/tero

	if ! TERO_VM="$instance" go test -tags e2e -count=1 -v -timeout 60m ./e2e/...; then
		failed+=("$release")
	fi
done

# join_by_comma prints its arguments as "a, b, c".
join_by_comma() {
	local joined
	printf -v joined '%s, ' "$@"
	echo "${joined%, }"
}

if [[ ${#failed[@]} -gt 0 ]]; then
	echo "=== Failed on Ubuntu $(join_by_comma "${failed[@]}")"
	exit 1
fi
echo "=== Passed on Ubuntu $(join_by_comma "${releases[@]}")"
