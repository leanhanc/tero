#!/bin/bash
# Runs as root inside the test VM. Starts Pebble, Let's Encrypt's test ACME
# server, and its DNS helper, which resolves every name to 127.0.0.1 so Pebble
# validates challenges against the local Tero service. Then points the Tero
# service (an e2e build) at Pebble.
set -euo pipefail

install -d -m 0755 /opt/pebble /etc/tero-e2e
install -m 0755 /tmp/pebble /tmp/pebble-challtestsrv /opt/pebble/

openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 30 \
	-subj /CN=localhost -addext subjectAltName=DNS:localhost,IP:127.0.0.1 \
	-keyout /opt/pebble/wfe-key.pem -out /opt/pebble/wfe-cert.pem 2>/dev/null
install -m 0644 /opt/pebble/wfe-cert.pem /etc/tero-e2e/pebble-wfe.pem

cat > /opt/pebble/config.json <<'JSON'
{
  "pebble": {
    "listenAddress": "127.0.0.1:14000",
    "managementListenAddress": "127.0.0.1:15000",
    "certificate": "/opt/pebble/wfe-cert.pem",
    "privateKey": "/opt/pebble/wfe-key.pem",
    "httpPort": 80,
    "tlsPort": 443,
    "externalAccountBindingRequired": false
  }
}
JSON

cat > /etc/systemd/system/pebble-challtestsrv.service <<'UNIT'
[Unit]
Description=Pebble DNS helper (Tero e2e only)

[Service]
ExecStart=/opt/pebble/pebble-challtestsrv -defaultIPv4 127.0.0.1 -defaultIPv6 "" -dnsserver 127.0.0.1:8053 -management 127.0.0.1:8055 -http01 "" -https01 "" -tlsalpn01 "" -doh ""

[Install]
WantedBy=multi-user.target
UNIT

cat > /etc/systemd/system/pebble.service <<'UNIT'
[Unit]
Description=Pebble test ACME server (Tero e2e only)
After=pebble-challtestsrv.service

[Service]
Environment=PEBBLE_VA_NOSLEEP=1 PEBBLE_WFE_NONCEREJECT=0 PEBBLE_AUTHZREUSE=100
ExecStart=/opt/pebble/pebble -config /opt/pebble/config.json -dnsserver 127.0.0.1:8053

[Install]
WantedBy=multi-user.target
UNIT

install -d -m 0755 /etc/systemd/system/tero.service.d
cat > /etc/systemd/system/tero.service.d/e2e.conf <<'UNIT'
[Service]
Environment=TERO_E2E_ACME_CA=https://localhost:14000/dir TERO_E2E_ACME_ROOT=/etc/tero-e2e/pebble-wfe.pem
UNIT

systemctl daemon-reload
systemctl enable pebble-challtestsrv.service pebble.service
systemctl restart pebble-challtestsrv.service pebble.service
