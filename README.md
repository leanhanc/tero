# Tero

Tero is like Vercel, but on your own VPS. Push to GitHub and your app is live with HTTPS on a server you own, and the server comes locked down from the first command.

> **Status:** early development. Tero is not ready for use yet. The server setup (`sudo tero init`) works; the dashboard, deploys, releases and the install script are still being built.

## What makes Tero cool

**Deploys** *(planned)*

- 🪄 **No Dockerfile needed.** Tero detects your language and framework and builds the app for you, with [Railpack](https://railpack.com). Already have a Dockerfile? Tero uses it.
- 🚀 **Deploy on push.** Connect a GitHub repo and every push to your branch builds and goes live.
- 🌐 **Automatic HTTPS.** Every app gets a free sslip.io address, or use your own domain. Certificates come from Let's Encrypt.
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
- ✍️ **Verifiable releases** *(planned)*: SLSA Build L3 provenance and an SBOM for every binary.

**Simple**

- 📦 **One binary.** Tero is a single Go binary with the web server and HTTPS built in.
- ⚡ **One command to set up.** `sudo tero init` turns a fresh server into a Tero host, with no config files to write.
- 🧱 **Nothing else to run.** No nginx, no Kubernetes, and no container registry: images are built and kept on the server *(planned)*.
- 🏠 **Your server, your bill.** Runs on any VPS with a supported Ubuntu, from any provider.

## Supported platforms

Ubuntu 24.04 LTS and Ubuntu 26.04 LTS on arm64. amd64 is added once the end-to-end suite runs on amd64 servers. `tero init` refuses to run anywhere else.

## Development

You need Go and, for the end-to-end tests, [Lima](https://lima-vm.io) 2.0 or later on an Apple silicon Mac.

```bash
make build    # Linux binaries for amd64 and arm64 in bin/
make test     # unit tests
make e2e      # end-to-end tests in fresh Ubuntu VMs
```

`make e2e` creates a fresh VM for each supported Ubuntu release, runs `sudo tero init` in it and checks every scenario. It takes a few minutes per release. Follow it with `tail -f .tero-e2e.log`. To test a single release, run `scripts/vm-test.sh 24.04`.

Specs live in [openspec/](openspec/), one folder per capability. Specs for later features are in [openspec/drafts/](openspec/drafts/).
