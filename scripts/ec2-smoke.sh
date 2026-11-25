#!/bin/bash
# Checks a release build on a fresh cloud server with nothing faked: copies
# the binary `make build` makes, runs `sudo tero init`, and checks that the
# dashboard serves a certificate browsers trust, issued by Let's Encrypt for
# its nip.io name, and that Lynis scores at least the suite's minimum. The
# full suite (scripts/ec2-test.sh) uses a test certificate authority instead,
# and covers install.sh against a fake release server. scripts/ec2-e2e.sh
# smoke creates the servers and calls this script for each.
#
#   scripts/ec2-smoke.sh ubuntu@<ip> <private key>
#   scripts/ec2-smoke.sh build <arch>    only build what a run copies
#
# With TERO_E2E_PREBUILT=1 a run skips the build.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

build() {
	local arch="$1"
	make web
	CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath \
		-ldflags "-s -w -X main.version=$(git describe --tags --always --dirty)" \
		-o "bin/tero-linux-$arch" ./cmd/tero
}

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

minimum="$(grep -oE 'minimumLynisScore = [0-9]+' e2e/init_test.go | grep -oE '[0-9]+$')"
ip="${TERO_SSH#*@}"
domain="$ip.nip.io"

scp -q -i "$TERO_SSH_KEY" "bin/tero-linux-$arch" "$TERO_SSH:/tmp/tero"
remote sudo install -m 0755 /tmp/tero /usr/local/bin/tero
echo "=== init"
remote sudo tero init | grep -v 'AGE-SECRET-KEY'

# curl without -k accepts only a certificate the system trusts. Init asked for
# it already, so this only waits for Let's Encrypt to finish.
echo "=== certificate for $domain"
is_trusted=false
for _ in $(seq 30); do
	if curl -fsS --max-time 10 -o /dev/null "https://$domain/" 2>/dev/null; then
		is_trusted=true
		break
	fi
	sleep 10
done
if [[ "$is_trusted" != "true" ]]; then
	curl -sS --max-time 10 -o /dev/null "https://$domain/" || true
	echo "the dashboard never served a trusted certificate" >&2
	exit 1
fi
issuer="$(openssl s_client -connect "$ip:443" -servername "$domain" </dev/null 2>/dev/null | openssl x509 -noout -issuer)"
echo "$issuer"
if ! grep -qi "let's encrypt" <<<"$issuer"; then
	echo "the certificate isn't from Let's Encrypt" >&2
	exit 1
fi

echo "=== Lynis (minimum $minimum)"
remote "sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -q lynis >/dev/null"
remote "sudo lynis audit system --quick --no-colors --cronjob >/dev/null"
score="$(remote "sudo grep '^hardening_index=' /var/log/lynis-report.dat" | cut -d= -f2)"
echo "hardening index $score"
if ((score < minimum)); then
	echo "Lynis hardening index $score is below the minimum $minimum" >&2
	exit 1
fi
