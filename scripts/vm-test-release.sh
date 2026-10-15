#!/bin/bash
# Runs as root inside the test VM. Serves fake Tero releases over HTTPS on
# localhost:8443 for the install script tests, laid out like a GitHub release:
#
#   /good/       the e2e binary as tero-linux-<arch>, install.sh, checksums.txt
#   /tampered/   the same files, with one byte appended to the binary
#
# The server runs as a transient systemd unit and its files live in /opt, so
# /etc and /usr/local stay as a fresh server has them.
#
#   vm-test-release.sh <arch>   expects /tmp/tero-e2e-linux-<arch> and /tmp/install.sh
set -euo pipefail

arch="$1"
root=/opt/tero-e2e-release
binary="tero-linux-$arch"

rm -rf "$root"
install -d -m 0755 "$root/good" "$root/tampered"

install -m 0755 "/tmp/tero-e2e-linux-$arch" "$root/good/$binary"
install -m 0644 /tmp/install.sh "$root/good/install.sh"
(cd "$root/good" && sha256sum "$binary" install.sh >checksums.txt)

cp "$root/good/"* "$root/tampered/"
printf '\0' >>"$root/tampered/$binary"

openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 30 \
	-subj /CN=localhost -addext subjectAltName=DNS:localhost \
	-keyout "$root/key.pem" -out "$root/cert.pem" 2>/dev/null
chmod 0644 "$root/cert.pem"

cat >"$root/server.py" <<'PY'
import http.server
import ssl

server = http.server.ThreadingHTTPServer(("127.0.0.1", 8443), http.server.SimpleHTTPRequestHandler)
context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
context.load_cert_chain("/opt/tero-e2e-release/cert.pem", "/opt/tero-e2e-release/key.pem")
server.socket = context.wrap_socket(server.socket, server_side=True)
server.serve_forever()
PY

systemctl stop tero-e2e-release.service 2>/dev/null || true
systemctl reset-failed tero-e2e-release.service 2>/dev/null || true
systemd-run --quiet --unit=tero-e2e-release --property=WorkingDirectory="$root" python3 "$root/server.py"

for _ in $(seq 50); do
	if curl -fsS --cacert "$root/cert.pem" -o /dev/null https://localhost:8443/good/checksums.txt 2>/dev/null; then
		exit 0
	fi
	sleep 0.1
done
echo "fake release server did not start" >&2
exit 1
