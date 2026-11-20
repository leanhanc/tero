## Why

AWS, Google Cloud, Azure and Oracle Cloud block inbound ports 80 and 443 by default. On those servers `sudo tero init` finishes, prints the setup link, and the dashboard never loads: 443 serves the dashboard and answers TLS-ALPN-01 (RFC 8737), and 80 answers HTTP-01 (RFC 8555 §8.3) and redirects to HTTPS. Today init only says the page "may take a few minutes", which sends the admin after DNS instead of their provider's firewall.

## What Changes

- Once the Tero service is listening, init asks a service on the internet, one port at a time, to connect back to the server's public IPv4 address on TCP 80 and 443.
- When a port is closed, init prints one ❌ line per port saying what needs it, then step-by-step help for the provider it detects from the server's DMI data (AWS security groups, Google Cloud firewall rules, Azure network security groups, Oracle Cloud security lists), or generic help, and a link to the public docs.
- When the check can't run, init says which ports weren't checked. The check never stops init, and the setup link stays the last line.
- Color only on a terminal and never with `NO_COLOR` set (https://no-color.org), as in `install.sh`.
- New public doc `docs/ports.md` with the same steps.

## Capabilities

### Modified Capabilities

- `server-init`: new requirement "Web ports reachable from the internet".

## Impact

- New package `internal/portcheck`: the connect-back client, provider detection and the copy (`copy.go`).
- Init makes two more outbound HTTPS requests, to `https://ifconfig.co/port/80` and `/port/443` (the open-source echoip service). The service learns the server's address, which Let's Encrypt's certificate transparency logs already publish for `<ip>.nip.io`.
- e2e: a fake connect-back service (`e2e/fakeprobe`) on the test server, picked with `TERO_E2E_PORT_PROBE` in e2e builds only.
