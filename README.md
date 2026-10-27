# Tero

Tero is like Vercel, but on your own VPS. Push to GitHub and your app is live with HTTPS on a server you own, and the server comes locked down from the first command.

> **Status:** early development. Tero is not ready for use yet. The install script, the server setup (`sudo tero init`), the dashboard with its account security, and deploying a JavaScript backend from a public GitHub repository by hand all work. Deploy on push, environment variables, custom domains and rollback are still being built.

## What makes Tero cool

**Deploys**

- 🪄 **No Dockerfile needed.** Tero detects your language and framework and builds the app for you, with [Railpack](https://railpack.com). JavaScript backends on Bun, npm, Yarn or pnpm work today. Other languages, and using your own Dockerfile, are *(planned)*.
- 🚀 **Deploy from GitHub.** Paste a public repository URL, pick a branch, and Tero builds it and takes it live. Redeploy the latest commit with one click. Deploy on every push, and private repositories, are *(planned)*.
- 🌐 **Automatic HTTPS.** Every app gets a free `<app>.<server-ip>.nip.io` address with a Let's Encrypt certificate, with no DNS to set up. Your own domain is *(planned)*.
- 🟢 **Safe releases.** A new release takes traffic only once it answers HTTP. If it fails, the old one keeps serving.
- 📜 **Live build logs,** and a clear reason when a deploy fails. Logs of running apps are *(planned)*.
- 🔑 **Environment variables and secrets,** set from the dashboard *(planned)*.
- ⏪ **One-click rollback** to an earlier deploy *(planned)*.
- ⚙️ **Web apps** today; **background workers** *(planned)*.
- 👀 **Preview deploys for pull requests** *(later)*.

**Security**

- 🔒 **One-command lockdown.** `sudo tero init` secures the server to a documented subset of the CIS benchmark: SSH keys only, a default-deny firewall, automatic security updates, locked-down kernel settings, auditing and AppArmor. Root is used only for `sudo tero` commands. Tero itself runs as an unprivileged user, and apps run in rootless containers. Every control, and every one it skips, is listed in [docs/security/cis-subset.md](docs/security/cis-subset.md).
- 🛟 **Won't lock you out.** Init checks everything before it changes anything, and refuses to turn off SSH passwords until you have a key in place. Lost your phone? `sudo tero reset-login` on the server prints a new setup link.
- 🛡️ **A dashboard built to OWASP ASVS Level 2 and NIST SP 800-63B-4.** Every login needs a password and an authenticator code, or a passkey. Passwords are checked against breach lists, failed logins are throttled, sessions are short, and you can see and end every session from the settings page.
- 🗝️ **Keys the dashboard can't read.** A separate key service holds the master key, sealed to the server's TPM when it has one, checks every login and keeps the security log. `sudo tero rotate-key` replaces the key, and `sudo tero assume-breach` responds to a suspected break-in in one command.
- 🫧 **Isolated apps.** Each app runs in its own rootless container with no capabilities, its own range of user ids and its own network, so apps can't reach each other. Mapped to NIST SP 800-190 in [docs/security/sp800-190.md](docs/security/sp800-190.md).
- 📏 **Checked, not claimed.** Every requirement is a spec scenario with an end-to-end test on a fresh server, plus a minimum Lynis security score.
- ✍️ **Verifiable releases:** SLSA Build L3 provenance and an SBOM for every binary.

Every standard Tero follows, and where each one is mapped, is listed in [SECURITY.md](SECURITY.md#security-baseline).

**Simple**

- 📦 **One binary.** Tero is a single Go binary with the web server and HTTPS built in.
- ⚡ **One command to set up.** `sudo tero init` turns a fresh server into a Tero host, with no config files to write.
- 🧱 **Nothing else to run.** No nginx, no Kubernetes, and no container registry: images are built and kept on the server.
- 🏠 **Your server, your bill.** Runs on any VPS with a supported Ubuntu, from any provider.

## Supported platforms

Ubuntu 24.04 LTS and Ubuntu 26.04 LTS on amd64 and arm64. The install script and `tero init` refuse to run anywhere else.

## Install

On a fresh server, run:

```bash
curl -fsSL https://github.com/leanhanc/tero/releases/latest/download/install.sh | sh
```

The script downloads the latest release binary for the server's architecture, checks it against the release's checksums, and installs it as `/usr/local/bin/tero`. It changes nothing else. Then set up the server:

```bash
sudo tero init
```

## Verify a release

Every release binary has a signed [SLSA](https://slsa.dev) Build Level 3 provenance attestation, made by the release workflow through GitHub artifact attestations, and an SPDX SBOM attached to the release. To check a binary yourself, download it from the [releases page](https://github.com/leanhanc/tero/releases) and run, with the [GitHub CLI](https://cli.github.com) signed in:

```bash
gh attestation verify tero-linux-amd64 --repo leanhanc/tero --signer-workflow leanhanc/tero/.github/workflows/build-release.yml
```

Verification succeeds only for the exact bytes the release workflow built, and fails for a binary that was changed afterwards. The install script checks checksums, not attestations, so it doesn't need the GitHub CLI on the server.

## Development

You need Go and [Bun](https://bun.sh) (to build the dashboard) and, for the local VMs, [Lima](https://lima-vm.io) 2.0 or later.

```bash
bun run dev   # start a local Tero VM and ship the current build into it
make build    # Linux binaries for amd64 and arm64 in bin/
make test     # Go and dashboard unit tests
make e2e      # end-to-end tests in fresh Ubuntu VMs
```

`bun run dev` creates the `tero-dev` VM the first time, and after that starts it and installs the current build. The dashboard opens at https://tero.localhost and deployed apps at `https://<app>.localhost`. Certificates come from the VM's test certificate authority, so the browser warns once. `make dev-vm` recreates the VM from scratch.

`make e2e` creates a fresh VM for each supported Ubuntu release on the host's architecture, installs Tero with `install.sh` from a fake release served inside the VM, runs `sudo tero init` and checks every scenario, deploys included. It takes a few minutes per release. Follow it with `tail -f .tero-e2e.log`. To test a single release, run `scripts/vm-test.sh 24.04`. To run the suite on a fresh cloud server instead, run `scripts/ec2-test.sh ubuntu@<ip> <private key>`.

Releases are published by pushing a version tag (`git tag v0.1.0 && git push origin v0.1.0`): [.github/workflows/release.yml](.github/workflows/release.yml) builds them with GoReleaser. To build the release files locally without publishing, run `goreleaser release --snapshot --clean`.

Specs live in [openspec/](openspec/), one folder per capability. Specs for later features are in [openspec/drafts/](openspec/drafts/), and the order they are built in is in [openspec/roadmap.md](openspec/roadmap.md). The security standards and their mappings are in [docs/security/](docs/security/).
