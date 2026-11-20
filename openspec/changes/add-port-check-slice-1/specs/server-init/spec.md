## ADDED Requirements

### Requirement: Web ports reachable from the internet
After starting the Tero service, init SHALL ask a service on the internet to connect back to the server's public IPv4 address on TCP ports 80 and 443. When either port is reported closed, init SHALL print which port is closed and what needs it, followed by step-by-step help to open it for the provider it detects (AWS security groups, Google Cloud firewall rules, Azure network security groups, Oracle Cloud security lists), or generic help when it detects none. When the check can't run or its answer is for another address, init SHALL say the ports weren't checked. The check SHALL NOT stop init, and the setup link SHALL stay the last line. Init SHALL color its output only on a terminal and never when `NO_COLOR` is set.

#### Scenario: Both ports open
- **WHEN** init runs on a server whose ports 80 and 443 are reachable
- **THEN** init prints nothing about ports

#### Scenario: Port closed at the provider
- **WHEN** init runs on a server whose port 80 is reported closed and 443 open
- **THEN** init prints a ❌ line for port 80, a ✅ line for port 443 and the help to open them, completes, and the last line is the setup link

#### Scenario: Check unavailable
- **WHEN** the connect-back service can't be reached or answers for another address
- **THEN** init prints a line per port saying it wasn't checked, prints no provider help, and completes

#### Scenario: No color off a terminal
- **WHEN** init's output is not a terminal, or `NO_COLOR` is set
- **THEN** the output has no ANSI escape sequences
