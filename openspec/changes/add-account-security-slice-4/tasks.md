Implements all of slice 4: the new capability `account-security` and this change's deltas to `key-service`, `dashboard-access`, `process-isolation`, `breach-response` and `security-log`. Each task names the spec scenarios it makes pass, written as `capability: Scenario`. Parts 1 to 4 are covered by Go and web unit tests; part 5 runs in fresh Lima VMs (Ubuntu 24.04 and 26.04, arm64) on the Mac and on amd64 (the Ubuntu PC, or EC2 through `scripts/ec2-test.sh` after asking).

## 1. Sessions move into the key service

- [x] 1.1 Add `sessions` (with id, IPs, user agent, method and `confirmed_at`), `trusted_devices` and `login_throttles` to `tero-keys`' `0001_keys.sql`; remove `tero`'s `0001_dashboard_access.sql`. Move the throttle, device and session rules from `internal/auth` into `internal/keys`, with the last-used write at most once a minute and the prune. Covers `key-service: Session created only by a login`, `Invented token`, `dashboard-access: No credentials in the dashboard's database`
- [x] 1.2 Replace `login.check` with `login.password`, make `setup.finish` return a session, and add `session.check` and `session.end`; switch the dashboard's login, setup, logout, `/api/state` and `requireSession` to them. Covers `dashboard-access: Correct password and code`, `Correct password, wrong code`, `Key service not answering`, `key-service: Ended session refused at once`, `process-isolation: Session planted by a tero process`
- [x] 1.3 Make `events.list` take the caller's session, and add the session check to every account verb. Covers `key-service: Dashboard asks without a session`
- [x] 1.4 Make `login.reset` delete sessions, devices, throttles and passkeys and replace the WebAuthn user handle; remove the `_login-link` child from `reset-login` and `assume-breach`, and add the passkey line to `assume-breach`'s output. Covers `dashboard-access: Admin loses their authenticator`, every `breach-response` scenario in this change

## 2. Password change, confirmation and sessions

- [x] 2.1 Add the context-word check to the password policy, fed with the dashboard domain and public IP. Covers `account-security: Domain label in the password`, `Service name in the password`
- [x] 2.2 Add `account.confirm` with the 5-minute window, the throttle charge, the code guessing limit and single-use codes. Covers every `account-security: Confirming it's you` scenario, `key-service: Code guessing limit in the key service` scenarios
- [x] 2.3 Add `password.change` with the optional end of other sessions. Covers every `account-security: Password change` scenario
- [x] 2.4 Add `session.list` and `session.revoke` (one id, or every other session). Covers every `account-security: Active sessions` scenario, `key-service: Unconfirmed session ends another`
- [x] 2.5 Add the account events and their journal entries, the dashboard's view of `totp_failed` as `confirm_failed`, and the events a flood can't prune. Covers every `security-log` scenario in this change

## 3. Passkeys

- [x] 3.1 Add `go-webauthn/webauthn`, the `passkeys` table, the relying party read from `/etc/tero/config.json`, and the in-memory challenges with their expiry, single use and cap. Covers `key-service: Expired challenge`, `Flood of login challenges`
- [x] 3.2 Add `passkey.register.begin`, `passkey.register.finish`, `passkey.list`, `passkey.rename` and `passkey.remove`, with the limit of 10 and the confirmation check. Covers every `account-security: Passkeys` scenario
- [x] 3.3 Add `login.passkey.begin` and `login.passkey.finish`, with user verification, the counter check and the IP-only throttle. Covers every `dashboard-access: Passkey login` scenario, `key-service: Passkey without user verification`

## 4. Dashboard pages

- [x] 4.1 Read `~/.claude/skills/code-style/SKILL.md`. Add the settings page under `web/src/features/settings/` (password form, confirmation form, session list, passkey list), linked from the page header, markup only, in English and Spanish, with tests in `web/test/`. Covers `account-security: Open settings`, `No session`
- [x] 4.2 Add "Log in with a passkey" to the login page, and passkey registration in settings, through the browser's WebAuthn API with the options the key service returns

## 5. Isolation tests, docs and end to end

- [x] 5.1 Add e2e tests: a session planted by a `tero` process is refused, `keys.db` can't be opened from `tero.service`, password change and session ending across two browsers, and `reset-login` removing passkeys. Passkeys in e2e use a small software authenticator in the Go test client (ES256, attestation `none`), since the e2e suite drives the dashboard's API without a browser. Covers `process-isolation: Dashboard can't reach the sessions`
- [x] 5.2 Run slices 1 to 4's e2e suites in both VMs on arm64 and on amd64, and check the Lynis score has not dropped
- [x] 5.3 Update `docs/security/asvs-l2.md` (6.1.2, 6.2.2, 6.2.3, 6.2.11, 7.4.3, 7.4.5, 7.5.1, 7.5.2 and Level 3 6.3.3 with tests; the login paths, brute-force, sessions, inputs and cryptography sections), `openspec/roadmap.md`, and `openspec/config.yaml`'s context to slice 4
- [x] 5.4 Adversarial correctness and security review of the whole change by a separate fresh agent with minimal context; fix what it finds
