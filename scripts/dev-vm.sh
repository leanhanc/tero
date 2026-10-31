#!/bin/bash
# Runs Tero in a local Lima VM you can open in your browser at
# https://tero.localhost (browsers send *.localhost to this machine). Deployed
# apps open at https://<app>.localhost.
#
#   scripts/dev-vm.sh          recreate the VM, run init, print the setup link
#   scripts/dev-vm.sh update   rebuild, install the new binary, restart Tero
#   scripts/dev-vm.sh up       create the VM if missing, otherwise start it if
#                              stopped and ship the current build into it
#   scripts/dev-vm.sh tunnel   recreate the VM with the dashboard on a public
#                              Cloudflare quick tunnel, so GitHub can reach
#                              its webhook
#
# A tunnel's name is random and lasts until the VM stops, so after a restart
# run tunnel again. The dashboard keeps its name across update.
#
# Certificates come from the VM's test CA, so the browser warns once. Each
# step prints one line; LOG_LEVEL=debug shows the commands' own output.
set -euo pipefail

instance="tero-dev"
release="${TERO_DEV_RELEASE:-26.04}"
domain="tero.localhost"
domain_file="/etc/tero-dev/domain"
cloudflared_version="2026.10.0"
repo_root="$(cd "$(dirname "$0")/.." && pwd)"
pebble_version="v2.10.1"
cd "$repo_root"

# step runs a command under a one-line description. Unless LOG_LEVEL is
# debug, its output is kept back and only shown if it fails.
step() {
	local description="$1"
	shift
	echo "$description..."

	if [[ "${LOG_LEVEL:-}" == "debug" ]]; then
		"$@"
		return
	fi

	local output
	if ! output="$("$@" 2>&1)"; then
		echo "$output" >&2
		echo "Failed: $description" >&2
		return 1
	fi
}

arch="$(go env GOHOSTARCH)"
step "Building Tero" make build-e2e E2E_ARCH="$arch"
binary="bin/tero-e2e-linux-$arch"

install_tero() {
	limactl copy "$binary" "$instance:/tmp/tero"
	limactl shell --workdir / "$instance" sudo install -m 0755 /tmp/tero /usr/local/bin/tero
}

# The agent and the key service run the same binary as the service, so all
# three restart.
restart_tero() {
	limactl shell --workdir / "$instance" sudo systemctl restart tero-keys.service tero
	limactl shell --workdir / "$instance" sudo systemctl --user --machine=tero-agent@ restart tero-agent.service
}

# A VM whose login was never set up would leave you at a login page with no
# way in, so it gets a fresh setup link. The link replaces any unused one.
print_setup_link_if_needed() {
	local state
	state="$(curl -sk --retry 10 --retry-all-errors --retry-delay 1 "https://$domain/api/state" || true)"
	if [[ "$state" != *'"isSetUp":false'* ]]; then
		return
	fi

	local link
	link="$(limactl shell --workdir / "$instance" sudo /usr/local/bin/tero reset-login | tail -n 1 | tr -d ' ')"
	echo
	echo "The dashboard login isn't set up yet. Open this link to set it up:"
	echo
	echo "  $link"
}

# A tunnel VM saved its dashboard name; any other VM uses the default.
load_domain() {
	local saved
	saved="$(limactl shell --workdir / "$instance" cat "$domain_file" 2>/dev/null || true)"
	if [[ -n "$saved" ]]; then
		domain="$saved"
	fi
}

update_tero() {
	load_domain
	if ! limactl shell --workdir / "$instance" test -f /etc/systemd/system/tero-keys.service; then
		echo "$instance was set up before the key service existed. Run scripts/dev-vm.sh to recreate it." >&2
		exit 1
	fi
	step "Installing the build in $instance" install_tero
	step "Restarting Tero" restart_tero
	echo "Updated. Open https://$domain"
	print_setup_link_if_needed
}

# Only one VM can hold port 443 on this machine, so the e2e VMs are stopped.
stop_e2e_vms() {
	for other in $(limactl list --format '{{.Name}} {{.Status}}' | awk '$1 ~ /^tero-ubuntu-/ && $2 == "Running" {print $1}'); do
		limactl stop "$other"
	done
}

mode="${1:-}"
instance_status="$(limactl list --format '{{.Status}}' "$instance" 2>/dev/null || true)"

if [[ "$mode" == "up" && "$instance_status" == "Stopped" ]]; then
	step "Stopping the e2e VMs" stop_e2e_vms
	step "Starting $instance" limactl start --tty=false "$instance"
fi

# A missing VM falls through to the full setup below.
if [[ "$mode" == "update" || ( "$mode" == "up" && -n "$instance_status" ) ]]; then
	update_tero
	exit 0
fi

pebble_bin="$(go env GOPATH)/bin"
if [[ "$(go env GOHOSTOS)" != "linux" ]]; then
	pebble_bin+="/linux_$arch"
fi
step "Installing Pebble, the test certificate authority" env GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go install \
	"github.com/letsencrypt/pebble/v2/cmd/pebble@$pebble_version" \
	"github.com/letsencrypt/pebble/v2/cmd/pebble-challtestsrv@$pebble_version"

step "Stopping the e2e VMs" stop_e2e_vms
limactl delete --force "$instance" >/dev/null 2>&1 || true
step "Creating $instance" limactl create --tty=false --name="$instance" \
	--set '.portForwards = [{"guestPort": 443, "hostPort": 443}, {"guestPort": 80, "hostPort": 80}]' \
	"lima/tero-ubuntu-$release.yaml"
step "Starting $instance" limactl start --tty=false "$instance"

set_up_test_ca() {
	limactl copy "$pebble_bin/pebble" "$pebble_bin/pebble-challtestsrv" scripts/vm-test-ca.sh "$instance:/tmp/"
	limactl shell --workdir / "$instance" sudo bash /tmp/vm-test-ca.sh
}
step "Setting up the test certificate authority" set_up_test_ca
step "Installing the build in $instance" install_tero

# cloudflared serves the dashboard on a random trycloudflare.com name. A quick
# tunnel can't set the SNI it sends to the origin from that name, and Caddy
# needs it to pick the certificate it got from Pebble, so cloudflared speaks
# plain HTTP to a socat bridge on 127.0.0.1:8080, which opens TLS to Caddy
# with the tunnel's name once it is known.
start_tunnel() {
	local checksum
	case "$arch" in
	arm64) checksum="e6422b9d4f72d3194bc5a38676f13667c06666523217b842a877d72a80b5ac08" ;;
	amd64) checksum="d33ff2d14475178d2012c2c56beba87389ac5ded27649519f198a7d3134a99db" ;;
	esac

	limactl shell --workdir / "$instance" sudo bash -euo pipefail -c "
		curl -fsSL -o /tmp/cloudflared https://github.com/cloudflare/cloudflared/releases/download/$cloudflared_version/cloudflared-linux-$arch
		echo '$checksum  /tmp/cloudflared' | sha256sum -c -
		install -m 0755 /tmp/cloudflared /usr/local/bin/cloudflared
		DEBIAN_FRONTEND=noninteractive apt-get install -y -qq socat
		cat > /etc/systemd/system/cloudflared-dev.service <<'UNIT'
[Unit]
Description=Cloudflare quick tunnel to the dashboard (Tero dev only)
After=network-online.target
Wants=network-online.target

[Service]
DynamicUser=yes
ExecStart=/usr/local/bin/cloudflared --no-autoupdate tunnel --url http://127.0.0.1:8080

[Install]
WantedBy=multi-user.target
UNIT
		systemctl daemon-reload
		systemctl enable --now cloudflared-dev.service
	"
}

# The tunnel prints its name once it has registered with Cloudflare.
read_tunnel_domain() {
	local found
	for _ in $(seq 60); do
		found="$(limactl shell --workdir / "$instance" sudo journalctl -u cloudflared-dev.service --no-pager -o cat |
			grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' | tail -n 1 || true)"
		if [[ -n "$found" ]]; then
			echo "${found#https://}"
			return
		fi
		sleep 1
	done
	echo "The Cloudflare tunnel didn't start. See: limactl shell $instance sudo journalctl -u cloudflared-dev.service" >&2
	return 1
}

start_sni_bridge() {
	limactl shell --workdir / "$instance" sudo bash -euo pipefail -c "
		cat > /etc/systemd/system/tero-dev-sni-bridge.service <<UNIT
[Unit]
Description=Plain HTTP from the Cloudflare tunnel to Caddy over TLS (Tero dev only)

[Service]
DynamicUser=yes
ExecStart=/usr/bin/socat TCP-LISTEN:8080,bind=127.0.0.1,fork,reuseaddr OPENSSL:127.0.0.1:443,verify=0,snihost=$domain

[Install]
WantedBy=multi-user.target
UNIT
		systemctl daemon-reload
		systemctl enable --now tero-dev-sni-bridge.service
	"
}

if [[ "$mode" == "tunnel" ]]; then
	step "Starting a Cloudflare tunnel" start_tunnel
	domain="$(read_tunnel_domain)"
	limactl shell --workdir / "$instance" sudo install -D -m 0644 /dev/stdin "$domain_file" <<<"$domain"
	step "Bridging the tunnel to Caddy" start_sni_bridge
fi

# The VM's public IP isn't reachable from the browser, so apps go under
# localhost instead of nip.io.
limactl shell --workdir / "$instance" sudo tee /etc/systemd/system/tero.service.d/dev.conf >/dev/null <<UNIT
[Service]
Environment=TERO_E2E_APP_DOMAIN=localhost
UNIT

limactl shell --workdir / "$instance" sudo env TERO_E2E_PUBLIC_IP=203.0.113.10 tero init --domain "$domain"
echo "Open https://$domain"
