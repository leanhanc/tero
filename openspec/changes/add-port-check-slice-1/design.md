## Context

Init runs on the server, but "reachable from the internet" can only be seen from outside: the provider's firewall sits in front of the server. Connecting to the server's own public IP from inside (hairpin) isn't a reliable stand-in: on AWS the security group applies to it, and Google Cloud, Azure and Oracle don't document what happens.

## Decisions

### Connect-back service

Init sends `GET https://ifconfig.co/port/<n>` over IPv4. echoip (https://github.com/mpolden/echoip) dials the caller's address on that port and answers `{"ip": "...", "port": n, "reachable": bool}`.

- The answer counts only when `ip` is the public IP init detected and `port` is the one asked. A server whose outbound traffic leaves through another address (a NAT gateway) gets "not checked" instead of a wrong answer.
- Requests go over IPv4 because the dashboard and app addresses are built on the IPv4 address (`<ip>.nip.io`).
- 10-second timeout per request. The ports are checked one after the other, since ifconfig.co asks automated clients to go slowly; a 429 is retried once after its `Retry-After` (RFC 9110 §10.2.3) when that is at most 20 seconds, and is otherwise Unchecked. The body is read up to 4 KiB.
- The service can only lie about reachability. The worst it can do is print wrong help and, by reporting both ports closed, skip the certificate wait and the DNS hint. Init still completes and the link stays last.
- Nothing the service returns is printed, so it can't inject terminal escapes.
- Alternatives: Let's Encrypt is a real outside probe, but a certificate issued through HTTP-01 says nothing about 443, and failed validations count toward its rate limits. Our own probe service would be better and can replace this later: the client takes any URL that speaks the same protocol.

### When it runs

After every step, so Caddy is listening, and before the certificate wait. Init first waits up to 30 seconds for both ports to accept on 127.0.0.1. When both are closed the certificate wait and the DNS hint ("may take a few minutes") are skipped, since no challenge can pass. With one port closed the other challenge can still pass, so init waits as before and still prints the DNS hint without a certificate.

### Provider detection

`/sys/class/dmi/id/sys_vendor`, `chassis_asset_tag` and `bios_version`: `Amazon EC2`, or `amazon` in the BIOS version on older Xen types → AWS (Lightsail too, so the AWS help points Lightsail users to its Networking tab); `Google` → Google Cloud; `Microsoft Corporation` with Azure's asset tag `7783-7084-3265-9085-8269-3286-77` → Azure; asset tag `OracleCloud.com` → Oracle Cloud. Anything else gets generic help, which also mentions a firewall on the server itself, such as ufw. Only one provider's help is printed, so the output stays short.

### Output

Nothing extra when both ports are open. When one is closed, both ports get a line (✅ or ❌) so the admin sees which one to fix. All copy sits in `internal/portcheck/copy.go` so it can change without touching logic.
