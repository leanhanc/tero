# Tero

Tero is like Vercel, but on your own VPS. Push to GitHub and your app is live with HTTPS on a server you own, and the server comes locked down from the first command.

> **Status:** early development. Tero is not ready for use yet. The install script, the server setup (`sudo tero init`) and the dashboard login work; deploys are still being built.

## What makes Tero cool

**Deploys** *(planned)*

- 🪄 **No Dockerfile needed.** Tero detects your language and framework and builds the app for you, with [Railpack](https://railpack.com). Already have a Dockerfile? Tero uses it.
- 🚀 **Deploy on push.** Connect a GitHub repo and every push to your branch builds and goes live.
- 🌐 **Automatic HTTPS.** Every app gets a free nip.io address, or use your own domain. Certificates come from Let's Encrypt.
- 🔑 **Environment variables and secrets,** set from the dashboard.
- 📜 **Live logs** for builds and running apps.
- ⏪ **One-click rollback** to any earlier deploy.
- ⚙️ **Web apps and background workers.**
- 👀 **Preview deploys for pull requests** *(later)*.

**Security**

- 🔒 **One-command lockdown.** `sudo tero init` secures the server to a documented subset of the CIS benchmark: SSH keys only, a default-deny firewall, automatic security updates, locked-down kernel settings, auditing and AppArmor. Root is used only for `sudo tero` commands. Tero itself runs as an unprivileged user, and apps run in rootless containers. Every control, and every one it skips, is listed in [docs/security/cis-subset.md](docs/security/cis-subset.md).
- 🛟 **Won't lock you out.** Init checks everything before it changes anything, and refuses to turn off SSH passwords until you have a key in place.
- 📏 **Checked, not claimed.** Every requirement is a spec scenario with an end-to-end test on a fresh server, plus a minimum Lynis security score.
- 🛡️ **A dashboard built to OWASP ASVS Level 2** *(planned)*: long passwords checked against breach lists, TOTP, rate limiting and strict sessions.
- ✍️ **Verifiable releases:** SLSA Build L3 provenance and an SBOM for every binary.

**Simple**

- 📦 **One binary.** Tero is a single Go binary with the web server and HTTPS built in.
- ⚡ **One command to set up.** `sudo tero init` turns a fresh server into a Tero host, with no config files to write.
- 🧱 **Nothing else to run.** No nginx, no Kubernetes, and no container registry: images are built and kept on the server *(planned)*.
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

You need Go and [Bun](https://bun.sh) (to build the dashboard) and, for the end-to-end tests, [Lima](https://lima-vm.io) 2.0 or later on an Apple silicon Mac.

```bash
make build    # Linux binaries for amd64 and arm64 in bin/
make test     # unit tests
make e2e      # end-to-end tests in fresh Ubuntu VMs
```

`make e2e` creates a fresh VM for each supported Ubuntu release, installs Tero with `install.sh` from a fake release served inside the VM, runs `sudo tero init` and checks every scenario. It takes a few minutes per release. Follow it with `tail -f .tero-e2e.log`. To test a single release, run `scripts/vm-test.sh 24.04`.

Releases are published by pushing a version tag (`git tag v0.1.0 && git push origin v0.1.0`): [.github/workflows/release.yml](.github/workflows/release.yml) builds them with GoReleaser. To build the release files locally without publishing, run `goreleaser release --snapshot --clean`.

Specs live in [openspec/](openspec/), one folder per capability. Specs for later features are in [openspec/drafts/](openspec/drafts/).
