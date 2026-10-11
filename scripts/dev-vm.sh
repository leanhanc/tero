#!/bin/bash
# Runs Tero in a local Lima VM you can open in your browser at
# https://tero.localhost (browsers send *.localhost to this machine).
#
#   scripts/dev-vm.sh          recreate the VM, run init, print the setup link
#   scripts/dev-vm.sh update   rebuild, install the new binary, restart Tero
#
# Certificates come from the VM's test CA, so the browser warns once.
set -euo pipefail

instance="tero-dev"
release="${TERO_DEV_RELEASE:-26.04}"
domain="tero.localhost"
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
pebble_version="v2.10.1"
cd "$repo_root"

arch="$(go env GOHOSTARCH)"
make build-e2e E2E_ARCH="$arch"
binary="bin/tero-e2e-linux-$arch"

install_tero() {
	limactl copy "$binary" "$instance:/tmp/tero"
	limactl shell --workdir / "$instance" sudo install -m 0755 /tmp/tero /usr/local/bin/tero
}

if [[ "${1:-}" == "update" ]]; then
	install_tero
	limactl shell --workdir / "$instance" sudo systemctl restart tero
	echo "Updated. Open https://$domain"
	exit 0
fi

pebble_bin="$(go env GOPATH)/bin"
if [[ "$(go env GOHOSTOS)" != "linux" ]]; then
	pebble_bin+="/linux_$arch"
fi
GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go install \
	"github.com/letsencrypt/pebble/v2/cmd/pebble@$pebble_version" \
	"github.com/letsencrypt/pebble/v2/cmd/pebble-challtestsrv@$pebble_version"

# Only one VM can hold port 443 on this machine, so the e2e VMs are stopped.
for other in $(limactl list --format '{{.Name}} {{.Status}}' | awk '$1 ~ /^tero-ubuntu-/ && $2 == "Running" {print $1}'); do
	limactl stop "$other"
done

limactl delete --force "$instance" >/dev/null 2>&1 || true
limactl create --tty=false --name="$instance" \
	--set '.portForwards = [{"guestPort": 443, "hostPort": 443}, {"guestPort": 80, "hostPort": 80}]' \
	"lima/tero-ubuntu-$release.yaml"
limactl start --tty=false "$instance"

limactl copy "$pebble_bin/pebble" "$pebble_bin/pebble-challtestsrv" scripts/vm-test-ca.sh "$instance:/tmp/"
limactl shell --workdir / "$instance" sudo bash /tmp/vm-test-ca.sh
install_tero

limactl shell --workdir / "$instance" sudo env TERO_E2E_PUBLIC_IP=203.0.113.10 tero init --domain "$domain"
