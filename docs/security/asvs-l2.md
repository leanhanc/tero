# OWASP ASVS 5.0.0 Level 2 for the dashboard

The dashboard and its HTTP endpoints are built to Level 2 of the [OWASP Application Security Verification Standard 5.0.0](https://github.com/OWASP/ASVS/tree/v5.0.0/5.0). ADR 0001 explains why ASVS is the standard for this surface. This file has one row for every Level 1 and Level 2 requirement (Level 2 includes Level 1). Each row says whether Tero meets it, where, and which test checks it.

Specs cite requirements as `v5.0.0-<id>`, for example `v5.0.0-6.5.1`. A test that checks a requirement cited by a spec has the ID in its name: `TestLogin_v5_0_0_6_5_1_ReplayedCodeFails` in Go, or `(v5.0.0-6.5.1)` in an end-to-end subtest name.

The [sections before the matrix](#documentation-asvs-asks-for) are the documentation ASVS requires (rate limiting, login paths, sessions, cryptography, logging and so on). Matrix rows point to them.

## Status values

| Status | Meaning |
|---|---|
| Met | Tero does what the requirement asks today. |
| Gap | The requirement applies today, Tero doesn't meet it, and no slice is assigned yet. |
| Planned | The requirement applies today and Tero doesn't meet it yet; the fix is scheduled for the slice named in the row. |
| Deviation | Tero deliberately does something different, for the reason given. |
| Open | Planned in slice 1 and not done yet (releases, `SECURITY.md`, CI). |
| Later | Applies once a feature from a later slice exists. The slice is named in the row. |
| N/A | The feature the requirement is about doesn't exist in Tero, for the reason given. |

## Summary

253 requirements: 140 Met, 3 Gap, 6 Planned, 1 Deviation, 3 Open, 7 Later, 93 N/A.

Planned for slice 3, Account security:

| ID | What's missing |
|---|---|
| 6.2.2, 6.2.3 | The admin can't change their password in the dashboard. Until then, `sudo tero reset-login` replaces it, and resets TOTP too. |
| 7.4.5, 7.5.2 | The admin can't list sessions or end them, other than with `sudo tero reset-login`. |
| 6.1.2, 6.2.11 | No list of context-specific words (such as "tero" or the dashboard domain) is checked for new passwords. |

Gaps, in the order they matter most:

| ID | Gap |
|---|---|
| 13.2.4, 13.2.5 | Outbound connections aren't limited to an allowlist. App builds reach whatever package registries the app's build uses. |
| 16.4.3 | Logs stay on the server. Nothing ships them to a separate system. |

Deviation: TOTP codes from the previous and next 30-second step are accepted for clock drift (6.5.5), as explained in its row.

Requirements met with no automated test yet: 1.2.4, 3.4.4, 3.4.5, 6.2.6, 6.2.8, 11.4.3, 12.1.1, 12.1.2, 13.4.2, 13.4.4 and 14.3.3. The matrix marks them with "—" in the Test column, along with rows that are checked by design or documentation rather than behaviour.

## Documentation ASVS asks for

### Login paths

ASVS 6.1.3 and 6.3.4. There are exactly two ways into the dashboard, and both end with a password and a confirmed TOTP code.

1. **Login**: `POST /api/login` with the password and a TOTP code. It is throttled (see [Brute-force protection](#brute-force-protection)) and records `login_succeeded`, `login_failed` or `rate_limited`.
2. **Setup link**: printed only on the server, by `sudo tero init` or `sudo tero reset-login`. The token is 256 random bits, sits in the URL fragment so it is never sent in a request line, works once, and expires after 24 hours. Issuing a new link cancels any unused one. Setup asks for a new password, then for a code from the new TOTP secret, and creates the account only when the code is right. It records `setup_link_issued`, `setup_completed` and `setup_code_failed`. Wrong codes count toward the login throttle.

Recovery is the second path. `sudo tero reset-login` needs root on the server, which is stronger proof than the password. It deletes the password, the TOTP secret, every session, every trusted device and every throttle counter, records `login_reset`, and prints a new setup link. The admin then enrols a new TOTP secret, exactly as at first setup. The dashboard home shows the reset event at the next login.

There is no default account, no password-only login, no API key and no recovery by email or security question.

### Brute-force protection

ASVS 6.1.1 and 6.3.1.

- Every login attempt is counted before the credentials are checked, in one write transaction, so parallel requests can't slip past a limit. A rate-limited client gets the same answer whether its credentials were right or not.
- Per source IP (per /64 for IPv6): lockouts start at the 5th failure in a row. Per account: at the 10th.
- The first lockout lasts 30 seconds and doubles with each further failure, up to 15 minutes. A lockout is only ever extended, never shortened.
- Counters are forgotten after 24 hours without a failure, or after a successful login. `sudo tero reset-login` clears them.
- Counters live in SQLite, so restarting the service doesn't reset them.
- Account lockout can't be used against the admin: a browser the admin has logged in from carries a `__Host-tero_device` cookie. That browser is limited by its own failures (5) instead of the account's, so strangers failing logins don't lock it out. The cookie grants nothing else, lasts 180 days without a login, and `reset-login` forgets every device.
- The client IP is the TCP peer address. Caddy serves the dashboard in the same process with no proxy in front, so forwarding headers are never read.

### Passwords

ASVS 6.1.2 and V6.2.

- At least 8 characters and at most 1024, counted in Unicode code points. ASVS asks for at least 8 and recommends 15. Tero uses 8 because the password is never the only factor: every login also needs a TOTP code. A shorter minimum also makes the password easier to remember. NIST SP 800-63B-4 (section 3.1.1.2) draws the same line: 15 characters for a password used on its own, and at least 8 for one used only as part of multi-factor authentication.
- No composition rules. Paste and password managers work: the fields use `autocomplete="new-password"` and `"current-password"` and nothing blocks paste.
- Every new password is checked against a bundled list of the 3,000 most common passwords of at least 8 characters, and against the Have I Been Pwned range API. Only the first 5 hex characters of the SHA-1 hash are sent, with `Add-Padding: true`. When the API can't be reached, the bundled list alone decides and a `breach_check_skipped` event is recorded.
- Passwords are compared exactly as typed: nothing is trimmed or case-folded.
- Passwords never expire.
- Context-specific words aren't checked yet; that is planned for slice 3 (6.1.2 and 6.2.11).

### Sessions

ASVS 7.1.1 and 7.1.2.

- A session ends after 30 minutes without a request, and 12 hours after login whatever the activity. The dashboard can deploy code to the server, so sessions are short. The 12-hour limit lets one working day go by without logging in again.
- The token is 256 random bits in the `__Host-tero_session` cookie (`Secure`, `HttpOnly`, `SameSite=Strict`, `Path=/`, no `Domain`). The database keeps only its SHA-256 hash.
- There is no limit on parallel sessions. There is one admin, and each session expires on its own timers.
- Logging in ends the session the browser already had, if any. Logout ends that session on the server. `sudo tero reset-login` ends all of them.
- The signed-in page checks the login state again once 30 idle minutes have passed, so it stops showing data when the server has ended the session.

### Authorization

ASVS 8.1.1 and 8.1.2. There is one role: the admin, who may do everything the dashboard offers and see every field it shows.

- Without a session: `GET /api/state`, the two setup calls (which need a valid setup link), `POST /api/login`, `POST /api/logout`, and the web app's static files.
- Every other API route needs a live session, checked in Go on every request.
- Nothing in the dashboard can run work as root. Root-only operations exist only as `sudo tero ...` commands on the server.

### Inputs and limits

ASVS 2.1.1, 2.1.3 and 15.1.3.

| Input | Rule |
|---|---|
| Request body | JSON only (`Content-Type: application/json`), at most 64 KiB, decoded into a fixed struct, unknown fields refused |
| Password | 8 to 1024 characters; see [Passwords](#passwords) |
| TOTP code | Exactly 6 ASCII digits |
| Setup link, session and device tokens | Looked up by SHA-256 hash; anything unknown is refused |
| State-changing request | `POST` only, with no `Origin` other than the dashboard's and no `Sec-Fetch-Site: cross-site` |

Password hashing is the one expensive operation: each Argon2id hash uses 64 MiB. At most two run at once. Other requests wait up to 10 seconds for a slot and then get a `503` with `Retry-After`. The service runs with `MemoryMax=512M`, so a flood can take down Tero but not sshd or the apps. The Have I Been Pwned request times out after 5 seconds.

### Cryptography

ASVS 11.1.2. Every key and algorithm Tero uses:

| What | Algorithm | Where it lives | Lifecycle |
|---|---|---|---|
| Session, device and setup-link tokens | 256 bits from `crypto/rand` | SHA-256 hash in SQLite | Deleted on expiry, logout or `reset-login` |
| Admin password | Argon2id (RFC 9106: 64 MiB, t=3, p=4, 16-byte salt) | PHC string in SQLite | Replaced by `reset-login` |
| TOTP secret | 160 bits from `crypto/rand`, HMAC-SHA1, 6 digits, 30 s (RFC 6238) | Plaintext in SQLite (file mode 0600, owned by `tero`) | Replaced by `reset-login` |
| TLS certificate and key | Chosen by Caddy (ECDSA P-256), TLS 1.2 and 1.3 | `/var/lib/tero/caddy` | Obtained and renewed by Caddy over ACME |
| Breach check | SHA-1 prefix, as the Have I Been Pwned API defines | Not stored | — |

SHA-1 appears only where a protocol fixes it: inside HMAC for TOTP, which is what authenticator apps support, and in the breach-check prefix, which is a lookup key rather than a security function. Everything Tero chooses itself uses SHA-256 or stronger.

Encrypting stored secrets, including the TOTP secret, arrives with secrets management in slice 5 (ADR 0001, section 5).

### Sensitive data

ASVS 14.1.1 and 14.1.2.

| Data | Protection |
|---|---|
| Admin password | Never stored or logged; only its Argon2id hash is kept |
| TOTP secret | Shown once during setup; never returned again or logged; database readable only by `tero` |
| Session, device and setup tokens | Only in `HttpOnly` cookies or the URL fragment; only hashes stored; never logged |
| TLS private key | Readable only by `tero` |
| Source IPs in security events | Shown to the admin; kept for one year, then deleted |

API responses carry `Cache-Control: no-store`, and logout sends `Clear-Site-Data: "cache", "storage"`. Nothing is kept in browser storage other than the two cookies.

### Connections

ASVS 13.1.1.

| Direction | Peer | Purpose |
|---|---|---|
| Inbound 443 | Browsers | Dashboard over HTTPS, and each app on `<app>.<public-ip>.sslip.io` |
| Inbound 80 | Browsers, the ACME CA | Redirect to HTTPS, ACME HTTP-01 challenges |
| Inbound SSH | The admin | Server administration, `sudo tero ...` |
| Outbound HTTPS | Let's Encrypt | Certificates |
| Outbound HTTPS | `api.pwnedpasswords.com` | Breached-password check |
| Outbound HTTPS | `1.1.1.1` | Public IP lookup during `sudo tero init` |
| Outbound HTTP(S) | Ubuntu archive mirrors | Packages during init and automatic security updates |
| Outbound HTTPS | `github.com` | Branches and source of apps' public repositories |
| Outbound HTTPS | `ghcr.io`, `docker.io` | Railpack's builder and runtime images, and the pinned BuildKit image |
| Outbound HTTP(S) | Whatever each app's build uses | Package registries (npm, for example) and runtime downloads during builds |

Repository URLs are the only addresses that come from input. Only `https://github.com/<owner>/<repo>` is accepted, and Tero builds every URL it fetches from the owner and name on that fixed host (1.3.6).

### Logging

ASVS 16.1.1.

| Log | Contents | Where | Retention |
|---|---|---|---|
| Security events | Time (UTC), type, source IP | `security_events` table in SQLite, shown on the dashboard home | One year |
| Journal copy of each event | Same, with `TERO_EVENT` and `TERO_SOURCE_IP` fields; failures, rate limits and resets at warning priority | systemd journal | journald settings |
| Refused requests | Time, reason (`no_session`, `cross_site`, `not_json`, `malformed_body`), path (first 256 bytes) and source IP, at warning priority with `TERO_EVENT=request_refused`. Anyone can cause these, so they stay out of the security events table | systemd journal | journald settings |
| Service errors | Unexpected errors, without request bodies | systemd journal (stdout of `tero.service`) | journald settings |
| Caddy | TLS and certificate errors | systemd journal | journald settings |

Events never contain a password, TOTP code, TOTP secret or token. The journal is written by journald, so the `tero` user can't change past entries.

## Matrix

### V1 Encoding and Sanitization

| ID | L | Requirement | Status | How | Test |
|---|---|---|---|---|---|
| 1.1.1 | 2 | Decode input once, before validation | Met | Request bodies are decoded once by `encoding/json` into typed structs | — |
| 1.1.2 | 2 | Encode output as the last step | Met | `encoding/json` on the server, React when rendering | — |
| 1.2.1 | 1 | Context-aware output encoding | Met | No HTML is built on the server from input; React escapes text | — |
| 1.2.2 | 1 | Encode data in built URLs | Met | Setup tokens are base64url; the `otpauth://` URI uses `url.PathEscape` and `url.Values` | `TestURIIsScannable` |
| 1.2.3 | 1 | Encode data in JavaScript and JSON | Met | `encoding/json` | — |
| 1.2.4 | 1 | Parameterized database queries | Met | Every query in `internal/auth` and `internal/store` uses placeholders | — |
| 1.2.5 | 1 | No OS command injection | Met | The dashboard runs no commands. Root CLI commands run fixed programs from system directories with argument lists and no shell (`internal/sysexec`). The agent runs `podman` with argument lists and no shell (`internal/podman`), after checking app names, branches and folders again | `TestHostRefusesCommandsOutsideSystemDirs`, `TestCheckName`, e2e `Dashboard cannot trigger root work` |
| 1.2.6 | 2 | LDAP injection | N/A | No LDAP | — |
| 1.2.7 | 2 | XPath injection | N/A | No XML | — |
| 1.2.8 | 2 | LaTeX injection | N/A | No LaTeX | — |
| 1.2.9 | 2 | Escape regular expressions | N/A | No regular expression is built from input | — |
| 1.3.1 | 1 | Sanitize WYSIWYG HTML | N/A | No rich-text input | — |
| 1.3.2 | 1 | No `eval` | Met | None in Go or the web app; the CSP has no `unsafe-eval` | `TestBuildCaddyConfigSetsStrictCSP` |
| 1.3.3 | 2 | Sanitize input for dangerous contexts | Met | Input reaches only SQL placeholders, Argon2id and TOTP checks, and is length-bounded ([Inputs and limits](#inputs-and-limits)) | — |
| 1.3.4 | 2 | Sanitize user SVG | N/A | No uploads | — |
| 1.3.5 | 2 | Sanitize Markdown, CSS and similar | N/A | No user markup is rendered | — |
| 1.3.6 | 2 | SSRF protection | Met | Repository URLs must be `https://github.com/<owner>/<repo>`; every fetched URL is rebuilt from the owner and name on that fixed host (`internal/source`) | `TestParseRepository_v5_0_0_1_3_6_OnlyGitHub`, `TestLookupRefusesOtherHosts_v5_0_0_1_3_6`, e2e `apps: Not a GitHub URL (v5.0.0-1.3.6)` |
| 1.3.7 | 2 | Template injection | N/A | No server-side templates | — |
| 1.3.8 | 2 | JNDI injection | N/A | Not Java | — |
| 1.3.9 | 2 | Memcache injection | N/A | No memcache | — |
| 1.3.10 | 2 | Format string injection | N/A | No format string comes from input | — |
| 1.3.11 | 2 | SMTP and IMAP injection | N/A | Tero sends no mail | — |
| 1.4.1 | 2 | Memory-safe buffers | Met | Go, with no cgo in the service (pure-Go SQLite) | — |
| 1.4.2 | 2 | Integer overflow | Met | No arithmetic on numbers from input | — |
| 1.4.3 | 2 | Use-after-free | Met | Garbage-collected Go | — |
| 1.5.1 | 1 | XXE | N/A | No XML parsing | — |
| 1.5.2 | 2 | Safe deserialization | Met | JSON into fixed structs, unknown fields refused, 64 KiB cap | — |

### V2 Validation and Business Logic

| ID | L | Requirement | Status | How | Test |
|---|---|---|---|---|---|
| 2.1.1 | 1 | Input rules documented | Met | [Inputs and limits](#inputs-and-limits) | — |
| 2.1.2 | 2 | Rules for combined items documented | N/A | No inputs depend on each other | — |
| 2.1.3 | 2 | Business limits documented | Met | [Brute-force protection](#brute-force-protection), [Inputs and limits](#inputs-and-limits) | — |
| 2.2.1 | 1 | Positive input validation | Met | Codes must be 6 digits, passwords 8 to 1024 characters, tokens must match a stored hash. App names, repository URLs, branches and folders are checked against allow patterns | `TestVerifyRefusesMalformedCodes`, `TestPolicy_v5_0_0_6_2_1_RejectsSevenCharacters` |
| 2.2.2 | 1 | Validation at a trusted layer | Met | All checks run in the Go API; the web app's `minlength` is only a hint | `TestSetupRejectsShortPassword_v5_0_0_6_2_1` |
| 2.2.3 | 2 | Combinations of items are reasonable | N/A | No combined items | — |
| 2.3.1 | 1 | Steps happen in order | Met | Finishing setup needs the pending credentials from its first step; nothing is created before | `TestSetupWithoutTOTPCreatesNoAccount` |
| 2.3.2 | 2 | Business limits enforced | Met | Throttle limits as documented | `TestLogin_v5_0_0_6_3_1_RepeatedFailuresFromOneIP`, `TestLogin_v5_0_0_6_3_1_DistributedGuessingHitsAccountLimit` |
| 2.3.3 | 2 | Operations are transactional | Met | Account creation, link use and the first session commit together; `reset-login` is one transaction | — |
| 2.3.4 | 2 | No double-booking | Met | The admin row has a fixed id, so racing setups can't create two accounts; attempts are counted in a write transaction | `TestSecondAccountCannotBeCreated`, `TestLogin_v5_0_0_6_3_1_ParallelFailuresCannotPassThrottle` |
| 2.4.1 | 2 | Anti-automation | Met | Logins and setup codes are throttled, hashing is bounded, and every other call needs a session or a setup link | `TestRateLimitedLoginHasRetryAfter`, `TestHashingIsBoundedToTwoAtOnce` |

### V3 Web Frontend Security

| ID | L | Requirement | Status | How | Test |
|---|---|---|---|---|---|
| 3.2.1 | 1 | Content not rendered in the wrong context | Met | The API answers only JSON, with `nosniff` and the CSP | `TestAppPagesServeIndexAndAPIStaysJSON` |
| 3.2.2 | 1 | Text rendered safely | Met | React renders text as text; no `dangerouslySetInnerHTML` | — |
| 3.3.1 | 1 | `Secure` and a `__Host-` or `__Secure-` prefix | Met | Both cookies are `__Host-` and `Secure` | `TestCookieAttributes` |
| 3.3.2 | 2 | `SameSite` fits the cookie | Met | `SameSite=Strict` | `TestCookieAttributes` |
| 3.3.3 | 2 | `__Host-` prefix | Met | `__Host-tero_session`, `__Host-tero_device` | `TestCookieAttributes` |
| 3.3.4 | 2 | `HttpOnly` for tokens | Met | Both cookies are `HttpOnly`; tokens never appear in a response body | `TestCookieAttributes` |
| 3.4.1 | 1 | HSTS for a year, including subdomains at L2 | Met | `max-age=31536000; includeSubDomains` | `TestBuildCaddyConfig_v5_0_0_3_4_1_HSTSCoversSubdomainsForAYear` |
| 3.4.2 | 1 | Fixed CORS origin | Met | No `Access-Control-Allow-Origin` is sent | — |
| 3.4.3 | 2 | Content-Security-Policy | Met | `default-src 'self'`, no inline scripts or styles, `object-src 'none'`, `base-uri 'none'` | `TestBuildCaddyConfigSetsStrictCSP`, e2e `web app served under the strict CSP` |
| 3.4.4 | 2 | `X-Content-Type-Options: nosniff` | Met | Set on every dashboard response | — |
| 3.4.5 | 2 | Referrer policy | Met | `Referrer-Policy: no-referrer` | — |
| 3.4.6 | 2 | CSP `frame-ancestors` | Met | `frame-ancestors 'none'` | `TestBuildCaddyConfigSetsStrictCSP` |
| 3.5.1 | 1 | Cross-origin requests validated | Met | Requests with another `Origin` or `Sec-Fetch-Site: cross-site` are refused, the API takes only JSON, and cookies are `SameSite=Strict` | `TestCrossSiteRequestsRefused`, `TestFormPostsRefused` |
| 3.5.2 | 1 | No bypass of CORS preflight | N/A | Tero doesn't rely on preflight; see 3.5.1 | — |
| 3.5.3 | 1 | Unsafe methods for sensitive actions | Met | Every state change is a `POST`; `GET` only reads | — |
| 3.5.4 | 2 | Separate apps on separate hostnames | Met | The dashboard has its own hostname. Slice 2 gives every app its own hostname too | `TestBuildCaddyConfigServesOnlyTheDashboardDomain` |
| 3.5.5 | 2 | `postMessage` origins checked | N/A | No `postMessage` | — |
| 3.7.1 | 2 | Supported client technologies | Met | React and standard browser APIs | — |
| 3.7.2 | 2 | Redirects only to allowed hosts | Met | The only redirect is HTTP to HTTPS on the same host | e2e `Plain HTTP request` |

### V4 API and Web Service

| ID | L | Requirement | Status | How | Test |
|---|---|---|---|---|---|
| 4.1.1 | 1 | Correct `Content-Type` with charset | Met | HTML and text are sent as UTF-8; JSON is `application/json`, which is UTF-8 by definition | — |
| 4.1.2 | 2 | Only user-facing endpoints redirect to HTTPS | Met | Port 80 answers `/api/` with `403` and redirects only pages to HTTPS | e2e `Plain HTTP request` |
| 4.1.3 | 2 | Intermediary headers can't be overridden | Met | No proxy in front; forwarding headers are never read | — |
| 4.2.1 | 2 | No request smuggling | Met | One server parses each request and handles it in the same process | — |
| 4.3.1 | 2 | GraphQL query limits | N/A | No GraphQL | — |
| 4.3.2 | 2 | GraphQL introspection off | N/A | No GraphQL | — |
| 4.4.1 | 1 | WebSockets over TLS | N/A | No WebSockets. Live build logs use Server-Sent Events on the dashboard's HTTPS session | — |
| 4.4.2 | 2 | WebSocket `Origin` checked | N/A | No WebSockets. Live build logs use Server-Sent Events on the dashboard's HTTPS session | — |
| 4.4.3 | 2 | WebSocket session tokens | N/A | No WebSockets. Live build logs use Server-Sent Events on the dashboard's HTTPS session | — |
| 4.4.4 | 2 | WebSocket tokens from the HTTPS session | N/A | No WebSockets. Live build logs use Server-Sent Events on the dashboard's HTTPS session | — |

### V5 File Handling

Tero accepts no file uploads and serves no user-supplied files.

| ID | L | Requirement | Status | How | Test |
|---|---|---|---|---|---|
| 5.1.1 | 2 | Upload rules documented | N/A | No uploads | — |
| 5.2.1 | 1 | Upload size limits | N/A | No uploads | — |
| 5.2.2 | 1 | Upload type checks | N/A | No uploads | — |
| 5.2.3 | 2 | Archive limits | N/A | No uploads | — |
| 5.3.1 | 1 | Uploads not executed | N/A | No uploads | — |
| 5.3.2 | 1 | File paths not from user input | Met | An app's folder is the only path from input. It must be relative, can't leave the repository, and is resolved through symlinks inside the checkout (`source.BuildDirectory`) | `TestBuildDirectoryRefusesSymlinkOutOfCheckout`, `TestCleanRootDirectory` |
| 5.4.1 | 2 | Download file names | N/A | No downloads | — |
| 5.4.2 | 2 | File names encoded | N/A | No downloads | — |
| 5.4.3 | 2 | Antivirus scan | N/A | No user files are served | — |

### V6 Authentication

| ID | L | Requirement | Status | How | Test |
|---|---|---|---|---|---|
| 6.1.1 | 1 | Brute-force controls documented | Met | [Brute-force protection](#brute-force-protection) | — |
| 6.1.2 | 2 | Context-specific words documented | Planned | No list yet (slice 3) | — |
| 6.1.3 | 2 | All login paths documented | Met | [Login paths](#login-paths) | — |
| 6.2.1 | 1 | At least 8 characters | Met | Minimum is 8, as NIST allows for a password that is always paired with TOTP; see [Passwords](#passwords) | `TestPolicy_v5_0_0_6_2_1_RejectsSevenCharacters`, e2e `Password too short (v5.0.0-6.2.1)` |
| 6.2.2 | 1 | Users can change their password | Planned | Only `sudo tero reset-login`, which also resets TOTP (slice 3) | — |
| 6.2.3 | 1 | Password change needs current and new password | Planned | Follows 6.2.2 (slice 3) | — |
| 6.2.4 | 1 | Checked against the top 3,000 passwords | Met | Bundled list | `TestBundledList_v5_0_0_6_2_4_CoversTopThreeThousandPolicyPasswords` |
| 6.2.5 | 1 | No composition rules | Met | Only length and breach checks | `TestPolicy_v5_0_0_6_2_5_HasNoCompositionRules` |
| 6.2.6 | 1 | Password fields masked | Met | `type="password"` on setup and login | — |
| 6.2.7 | 1 | Paste and password managers allowed | Met | `autocomplete` set, paste not blocked | web `labels the password field for password managers and sends the token from the fragment (v5.0.0-6.2.7)` |
| 6.2.8 | 1 | Password checked exactly as typed | Met | No trimming or case-folding | — |
| 6.2.9 | 2 | 64-character passwords allowed | Met | Up to 1024 | `TestPolicy_v5_0_0_6_2_9_AcceptsSixtyFourLowercaseLetters`, e2e `Long password accepted (v5.0.0-6.2.9)` |
| 6.2.10 | 2 | No forced rotation | Met | Passwords never expire | — |
| 6.2.11 | 2 | Context-specific words refused | Planned | Follows 6.1.2 (slice 3) | — |
| 6.2.12 | 2 | Breached passwords refused | Met | Bundled list plus Have I Been Pwned | `TestPolicy_v5_0_0_6_2_12_RejectsPasswordFromBreachAPI`, `TestPwnedPasswords_v5_0_0_6_2_12_SendsOnlyHashPrefix`, e2e `Breached password (v5.0.0-6.2.12)` |
| 6.3.1 | 1 | Brute-force controls in place | Met | [Brute-force protection](#brute-force-protection) | `TestLogin_v5_0_0_6_3_1_*`, e2e `Repeated failures from one IP (v5.0.0-6.3.1)` |
| 6.3.2 | 1 | No default accounts | Met | No account exists until setup | `TestNoLoginBeforeSetup` |
| 6.3.3 | 2 | Multi-factor authentication | Met | Password and TOTP on every login; setup can't finish without TOTP | `TestSetupWithoutTOTPCreatesNoAccount`, `TestWrongCodeDoesNotSayWhichFactor` |
| 6.3.4 | 2 | No undocumented login paths | Met | Two paths, both documented and recorded | `TestEventsRecordedWithoutSecrets`, e2e `Events recorded / No secrets in events (v5.0.0-6.3.4, v5.0.0-6.3.5, v5.0.0-6.3.7)` |
| 6.4.1 | 1 | Activation codes random, short-lived, single use | Met | Setup links: 256 bits, 24 hours, single use | `TestSetupLinkReusedIsRejected`, `TestSetupLinkExpires`, `TestNewSetupLinkReplacesTheOldOne` |
| 6.4.2 | 1 | No hints or security questions | Met | None | — |
| 6.4.3 | 2 | Password reset doesn't bypass MFA | Met | The only reset needs root and makes the admin enrol TOTP again | `TestResetLoginRevokesSessionsAndCredentials` |
| 6.4.4 | 2 | Lost factor needs the same proof as enrolment | Met | Same path as first setup: a link printed on the server | `TestResetLoginRevokesSessionsAndCredentials` |
| 6.5.1 | 2 | TOTP codes usable once | Met | The last accepted time step is stored and only later steps are accepted | `TestLogin_v5_0_0_6_5_1_ReplayedCodeFails`, `TestVerify_v5_0_0_6_5_1_RefusesReplayedCode`, e2e `Replayed code (v5.0.0-6.5.1)` |
| 6.5.2 | 2 | Weak lookup secrets hashed | N/A | No recovery codes | — |
| 6.5.3 | 2 | TOTP seeds from a CSPRNG | Met | 160 bits from `crypto/rand` | — |
| 6.5.4 | 2 | Lookup and out-of-band codes have 20 bits | N/A | Neither exists | — |
| 6.5.5 | 2 | TOTP lifetime of 30 seconds | Deviation | The step is 30 seconds, and the steps before and after are also accepted for clock drift between server and phone (RFC 6238 section 5.2). Each code still works only once | `TestVerifyAcceptsAdjacentStepsForClockDrift`, `TestVerifyRefusesOldCodes` |
| 6.6.1 | 2 | SMS codes only with safeguards | N/A | No SMS | — |
| 6.6.2 | 2 | Out-of-band codes bound to the request | N/A | No out-of-band codes | — |
| 6.6.3 | 2 | Out-of-band codes rate limited | N/A | No out-of-band codes | — |
| 6.8.1 | 2 | No identity spoofing across IdPs | N/A | No identity providers | — |
| 6.8.2 | 2 | Assertion signatures checked | N/A | No identity providers | — |
| 6.8.3 | 2 | SAML assertions used once | N/A | No SAML | — |
| 6.8.4 | 2 | IdP authentication strength checked | N/A | No identity providers | — |

### V7 Session Management

| ID | L | Requirement | Status | How | Test |
|---|---|---|---|---|---|
| 7.1.1 | 2 | Timeouts documented | Met | [Sessions](#sessions) | — |
| 7.1.2 | 2 | Parallel sessions documented | Met | [Sessions](#sessions) | — |
| 7.1.3 | 2 | Federated sessions documented | N/A | No federation | — |
| 7.2.1 | 1 | Tokens verified by the backend | Met | Checked in Go on every protected route | `TestEventsNeedSession` |
| 7.2.2 | 1 | Dynamic session tokens | Met | A new random token per session | — |
| 7.2.3 | 1 | 128-bit tokens from a CSPRNG | Met | 256 bits from `crypto/rand` | — |
| 7.2.4 | 1 | New token on login, old one ended | Met | Every login issues a new token and ends the session the browser sent | `TestLogin_v5_0_0_7_2_4_EndsThePreviousSession` |
| 7.3.1 | 2 | Inactivity timeout | Met | 30 minutes | `TestSessionIdleTimeout` |
| 7.3.2 | 2 | Absolute lifetime | Met | 12 hours | `TestSessionAbsoluteTimeout` |
| 7.4.1 | 1 | Ended sessions can't be used | Met | Logout and expiry delete the session on the server | `TestLogout_v5_0_0_7_4_1_EndsTheSession`, `TestSessionIdleTimeout` |
| 7.4.2 | 1 | Sessions end when the account is disabled | Met | `reset-login` deletes every session | `TestResetLoginRevokesSessionsAndCredentials` |
| 7.4.3 | 2 | Option to end sessions after a factor change | Met | Factors change only through `reset-login`, which ends every session | `TestResetLoginRevokesSessionsAndCredentials` |
| 7.4.4 | 2 | Visible logout | Met | Log out button on the only signed-in page | — |
| 7.4.5 | 2 | Admin can end sessions | Planned | Only `reset-login`, which also wipes the credentials (slice 3) | — |
| 7.5.1 | 2 | Re-authentication before changing factors | N/A | The dashboard can't change a factor yet. Applies once 6.2.2 is met | — |
| 7.5.2 | 2 | View and end active sessions | Planned | No session list (slice 3) | — |
| 7.6.1 | 2 | RP and IdP session lifetimes | N/A | No federation | — |
| 7.6.2 | 2 | Sessions need user action | N/A | No federation | — |

### V8 Authorization

| ID | L | Requirement | Status | How | Test |
|---|---|---|---|---|---|
| 8.1.1 | 1 | Function and data access rules documented | Met | [Authorization](#authorization) | — |
| 8.1.2 | 2 | Field access rules documented | Met | [Authorization](#authorization) | — |
| 8.2.1 | 1 | Function-level access enforced | Met | Session required on every other route | `TestEventsNeedSession` |
| 8.2.2 | 1 | Object-level access enforced | N/A | One admin owns all data | — |
| 8.2.3 | 2 | Field-level access enforced | N/A | One admin sees every field | — |
| 8.3.1 | 1 | Authorization on the server | Met | Go API | `TestEventsNeedSession`, e2e `Dashboard cannot trigger root work` |
| 8.4.1 | 2 | Cross-tenant controls | N/A | Single tenant | — |

### V9 Self-contained Tokens

Tero accepts no self-contained tokens such as JWTs: sessions are random reference tokens.

| ID | L | Requirement | Status | How | Test |
|---|---|---|---|---|---|
| 9.1.1 | 1 | Token signatures checked | N/A | No self-contained tokens | — |
| 9.1.2 | 1 | Algorithm allowlist | N/A | No self-contained tokens | — |
| 9.1.3 | 1 | Trusted key sources | N/A | No self-contained tokens | — |
| 9.2.1 | 1 | Validity period checked | N/A | No self-contained tokens | — |
| 9.2.2 | 2 | Token type checked | N/A | No self-contained tokens | — |
| 9.2.3 | 2 | Audience checked | N/A | No self-contained tokens | — |
| 9.2.4 | 2 | Audience restriction on issued tokens | N/A | No self-contained tokens | — |

### V10 OAuth and OIDC

Tero isn't an OAuth client, resource server, authorization server or OpenID provider for login. The GitHub App connection in slice 4 makes Tero a GitHub App, so the client rows are checked again then.

| ID | L | Requirement | Status | How | Test |
|---|---|---|---|---|---|
| 10.1.1 | 2 | Tokens only where needed | Later | Slice 4: installation tokens | — |
| 10.1.2 | 2 | Values tied to the initiating session | Later | Slice 4: GitHub App manifest and install callbacks | — |
| 10.2.1 | 2 | CSRF protection for the code flow | Later | Slice 4: GitHub App manifest flow | — |
| 10.2.2 | 2 | Mix-up defense | N/A | Only GitHub | — |
| 10.3.1 | 2 | Audience checked | N/A | Not a resource server | — |
| 10.3.2 | 2 | Delegated claims enforced | N/A | Not a resource server | — |
| 10.3.3 | 2 | User identified by stable claims | N/A | Not a resource server | — |
| 10.3.4 | 2 | Authentication strength checked | N/A | Not a resource server | — |
| 10.4.1 | 1 | Exact redirect URIs | N/A | Not an authorization server | — |
| 10.4.2 | 1 | Codes used once | N/A | Not an authorization server | — |
| 10.4.3 | 1 | Short-lived codes | N/A | Not an authorization server | — |
| 10.4.4 | 1 | Only needed grants | N/A | Not an authorization server | — |
| 10.4.5 | 1 | Refresh token replay | N/A | Not an authorization server | — |
| 10.4.6 | 2 | PKCE required | N/A | Not an authorization server | — |
| 10.4.7 | 2 | Dynamic client registration | N/A | Not an authorization server | — |
| 10.4.8 | 2 | Refresh token expiry | N/A | Not an authorization server | — |
| 10.4.9 | 2 | Token revocation UI | N/A | Not an authorization server | — |
| 10.4.10 | 2 | Confidential clients authenticated | N/A | Not an authorization server | — |
| 10.4.11 | 2 | Minimal scopes | N/A | Not an authorization server | — |
| 10.5.1 | 2 | ID token replay | N/A | Not an OIDC client | — |
| 10.5.2 | 2 | Stable user identifier | N/A | Not an OIDC client | — |
| 10.5.3 | 2 | Issuer metadata checked | N/A | Not an OIDC client | — |
| 10.5.4 | 2 | ID token audience | N/A | Not an OIDC client | — |
| 10.5.5 | 2 | Back-channel logout | N/A | Not an OIDC client | — |
| 10.6.1 | 2 | Allowed response modes | N/A | Not an OpenID provider | — |
| 10.6.2 | 2 | Forced logout | N/A | Not an OpenID provider | — |
| 10.7.1 | 2 | User consent | N/A | Not an authorization server | — |
| 10.7.2 | 2 | Clear consent prompts | N/A | Not an authorization server | — |
| 10.7.3 | 2 | Consent review | N/A | Not an authorization server | — |

### V11 Cryptography

| ID | L | Requirement | Status | How | Test |
|---|---|---|---|---|---|
| 11.1.1 | 2 | Key management policy | Later | Lifecycles are in [Cryptography](#cryptography); rotation, recovery and destruction of stored-secret keys arrive with slice 5 (ADR 0001) | — |
| 11.1.2 | 2 | Cryptographic inventory | Met | [Cryptography](#cryptography) | — |
| 11.2.1 | 2 | Validated implementations | Met | Go standard library, `golang.org/x/crypto/argon2`, Caddy | — |
| 11.2.2 | 2 | Crypto agility | Met | Argon2id parameters are stored in each hash; TLS settings come from Caddy | — |
| 11.2.3 | 2 | 128-bit security | Met | 256-bit tokens, 160-bit HMAC key, Caddy's TLS defaults | — |
| 11.3.1 | 1 | No ECB or weak padding | N/A | Tero encrypts nothing itself yet; slice 5 uses AES-256-GCM | — |
| 11.3.2 | 1 | Approved ciphers such as AES-GCM | N/A | As 11.3.1 | — |
| 11.3.3 | 2 | Authenticated encryption | N/A | As 11.3.1 | — |
| 11.4.1 | 1 | Approved hash functions | Met | SHA-256 for everything Tero chooses; SHA-1 only where TOTP and the breach API require it, as explained in [Cryptography](#cryptography) | — |
| 11.4.2 | 2 | Passwords stored with a slow KDF | Met | Argon2id, RFC 9106 parameters | `TestHashIsArgon2idAndVerifies`, e2e `Stored form` |
| 11.4.3 | 2 | Collision-resistant hashes | Met | SHA-256 for token hashes | — |
| 11.4.4 | 2 | Key stretching for password-derived keys | N/A | No keys are derived from passwords | — |
| 11.5.1 | 2 | CSPRNG with 128 bits | Met | `crypto/rand` for tokens (256 bits), TOTP secrets (160) and salts (128) | — |
| 11.6.1 | 2 | Approved key generation | Met | Caddy generates the TLS key | — |

### V12 Secure Communication

| ID | L | Requirement | Status | How | Test |
|---|---|---|---|---|---|
| 12.1.1 | 1 | Only TLS 1.2 and 1.3 | Met | Caddy's defaults | — |
| 12.1.2 | 2 | Recommended cipher suites | Met | Caddy's defaults: AEAD suites with forward secrecy | — |
| 12.1.3 | 2 | mTLS certificates validated | N/A | No mTLS | — |
| 12.2.1 | 1 | TLS for external services | Met | The dashboard is only served over HTTPS | e2e `Plain HTTP request`, e2e `Admin skips the domain (serves HTTPS)` |
| 12.2.2 | 1 | Publicly trusted certificates | Met | Let's Encrypt through ACME. Local e2e uses Pebble; real certificates are tested on cloud servers in slice 1, part 4 | e2e `Admin provides a domain` |
| 12.3.1 | 2 | Encrypted connections in and out | Met | Outbound calls use HTTPS; administration uses SSH | — |
| 12.3.2 | 2 | TLS clients validate certificates | Met | Go's default verification; nothing turns it off | — |
| 12.3.3 | 2 | Internal HTTP services use TLS | Later | The dashboard runs in-process today. Slice 2 adds the proxy to app containers | — |
| 12.3.4 | 2 | Internal certificates trusted | Later | As 12.3.3 | — |

### V13 Configuration

| ID | L | Requirement | Status | How | Test |
|---|---|---|---|---|---|
| 13.1.1 | 2 | Communication needs documented | Met | [Connections](#connections) | — |
| 13.2.1 | 2 | Backend components authenticated | N/A | No networked backend components; SQLite is a local file | — |
| 13.2.2 | 2 | Least privilege for components | Met | The service runs as `tero` with only `CAP_NET_BIND_SERVICE`; the database is mode 0600 | e2e `Service process identity`, `TestOpenCreatesPrivateDatabaseWithSchema` |
| 13.2.3 | 2 | No default credentials | Met | None exist | — |
| 13.2.4 | 2 | Outbound allowlist | Gap | The firewall filters inbound only | — |
| 13.2.5 | 2 | Server allowlist for requests | Gap | As 13.2.4 | — |
| 13.3.1 | 2 | Secrets management solution | Later | Slice 5: a systemd-sealed key and envelope encryption (ADR 0001). Today the TOTP secret and TLS key are files only `tero` can read | — |
| 13.3.2 | 2 | Least privilege for secrets | Met | Only the `tero` user can read the database and Caddy storage | `TestOpenCreatesPrivateDatabaseWithSchema` |
| 13.4.1 | 1 | No source control metadata | Met | One binary with the built web app embedded | — |
| 13.4.2 | 2 | Debug modes off | Met | Caddy's admin API is disabled; no debug endpoints | — |
| 13.4.3 | 2 | No directory listings | Met | Assets are served by exact name; any other path gets the app page | — |
| 13.4.4 | 2 | No `TRACE` | Met | The API answers unknown methods with an error, and the web app allows only `GET` and `HEAD` | — |
| 13.4.5 | 2 | No exposed docs or monitoring | Met | None exist | — |

### V14 Data Protection

| ID | L | Requirement | Status | How | Test |
|---|---|---|---|---|---|
| 14.1.1 | 2 | Sensitive data classified | Met | [Sensitive data](#sensitive-data) | — |
| 14.1.2 | 2 | Protection requirements documented | Met | [Sensitive data](#sensitive-data) | — |
| 14.2.1 | 1 | No sensitive data in URLs | Met | The setup token is in the fragment; everything else is in bodies and cookies | web `labels the password field for password managers and sends the token from the fragment (v5.0.0-6.2.7)` |
| 14.2.2 | 2 | No caching in server components | Met | No caches in front; API responses are `no-store` | `TestAPIResponsesAreNotCached` |
| 14.2.3 | 2 | No data to untrusted parties | Met | The CSP allows only the dashboard's origin; the breach API gets only a 5-character hash prefix | `TestPwnedPasswords_v5_0_0_6_2_12_SendsOnlyHashPrefix` |
| 14.2.4 | 2 | Documented controls implemented | Met | As in [Sensitive data](#sensitive-data) | `TestEventsRecordedWithoutSecrets` |
| 14.3.1 | 1 | Client data cleared when the session ends | Met | Logout sends `Clear-Site-Data: "cache", "storage"` (cookies stay, so the trusted-device cookie survives), and the signed-in page checks the state again once an idle session has ended | `TestLogout_v5_0_0_14_3_1_ClearsSiteDataButKeepsCookies`, web `goes back to the login form once an idle session has ended (v5.0.0-14.3.1)` |
| 14.3.2 | 2 | Anti-caching headers | Met | `Cache-Control: no-store` on the API | `TestAPIResponsesAreNotCached` |
| 14.3.3 | 2 | No sensitive data in browser storage | Met | Only the two `HttpOnly` cookies | — |

### V15 Secure Coding and Architecture

| ID | L | Requirement | Status | How | Test |
|---|---|---|---|---|---|
| 15.1.1 | 1 | Remediation time frames documented | Open | `SECURITY.md`, slice 1 task 3.4 | — |
| 15.1.2 | 2 | SBOM | Open | Syft SBOMs with each release, slice 1 task 3.1 | — |
| 15.1.3 | 2 | Expensive functions documented | Met | [Inputs and limits](#inputs-and-limits) | — |
| 15.2.1 | 1 | Components within time frames | Open | Follows 15.1.1, with `govulncheck` and dependency updates in CI | — |
| 15.2.2 | 2 | Availability defenses | Met | Two hashing slots, a 10-second wait then `503`, `MemoryMax=512M` | `TestHashingIsBoundedToTwoAtOnce` |
| 15.2.3 | 2 | No test code in production | Met | Test-only hooks (the local ACME issuer, the public IP override) compile only with the `e2e` build tag | — |
| 15.3.1 | 1 | Only needed fields returned | Met | Events return time, type, IP and detail; no other stored data leaves the API | — |
| 15.3.2 | 2 | Redirects not followed | Met | The breach-check client refuses redirects; a redirect counts as the API being unavailable | `TestPwnedPasswords_v5_0_0_15_3_2_RefusesRedirects` |
| 15.3.3 | 2 | No mass assignment | Met | Fixed request structs, unknown fields refused | — |
| 15.3.4 | 2 | Real client IP | Met | The TCP peer address | `TestIPv6ClientsAreThrottledPerSlash64` |
| 15.3.5 | 2 | No type juggling | Met | Static Go types, strict JSON decoding | — |
| 15.3.6 | 2 | No prototype pollution | Met | The web app never merges untrusted objects | — |
| 15.3.7 | 2 | No parameter pollution | Met | Input comes only from the JSON body | — |

### V16 Security Logging and Error Handling

| ID | L | Requirement | Status | How | Test |
|---|---|---|---|---|---|
| 16.1.1 | 2 | Logging inventory | Met | [Logging](#logging) | — |
| 16.2.1 | 2 | When, where, who, what | Met | Time, type and source IP; there is one admin | `TestEventsRecordedWithoutSecrets` |
| 16.2.2 | 2 | Synchronized clocks, UTC | Met | Stored as Unix time and returned in UTC; Ubuntu syncs the clock with systemd-timesyncd | — |
| 16.2.3 | 2 | Logs only go to documented places | Met | [Logging](#logging) | — |
| 16.2.4 | 2 | Readable log format | Met | Structured journal fields | — |
| 16.2.5 | 2 | Sensitive data kept out of logs | Met | No passwords, codes, secrets or tokens | `TestEventsRecordedWithoutSecrets` |
| 16.3.1 | 2 | All authentication logged | Met | Every login path records its events | `TestEventsRecordedWithoutSecrets`, `TestRateLimitTriggerIsRecorded` |
| 16.3.2 | 2 | Failed authorization logged | Met | Requests refused for a missing or expired session are written to the journal | `TestRefusedRequests_v5_0_0_16_3_2_MissingSessionIsJournaled` |
| 16.3.3 | 2 | Bypass attempts logged | Met | Rate limiting is recorded as an event; cross-site requests, non-JSON requests and malformed bodies are written to the journal | `TestRefusedRequests_v5_0_0_16_3_3_BypassAttemptsAreJournaled` |
| 16.3.4 | 2 | Unexpected errors logged | Met | Logged to the journal; Caddy logs TLS errors | — |
| 16.4.1 | 2 | No log injection | Met | Structured journal fields; the IP comes from TCP; event types are fixed | — |
| 16.4.2 | 2 | Logs protected | Met | journald owns the journal, so `tero` can't edit past entries | — |
| 16.4.3 | 2 | Logs sent to a separate system | Gap | Logs stay on the server | — |
| 16.5.1 | 2 | Generic error messages | Met | Unknown errors become a generic `500` | — |
| 16.5.2 | 2 | Works when external resources fail | Met | If the breach API is down, the bundled list decides and an event is recorded | `TestPolicyFallsBackToBundledListWhenAPIFails` |
| 16.5.3 | 2 | Fails closed | Met | Every error path refuses, and attempts are charged before credentials are checked | `TestLogin_v5_0_0_6_3_1_LimitDoesNotLeakValidity` |

### V17 WebRTC

Tero has no WebRTC, TURN or media server.

| ID | L | Requirement | Status | How | Test |
|---|---|---|---|---|---|
| 17.1.1 | 2 | TURN relays only public addresses | N/A | No WebRTC | — |
| 17.2.1 | 2 | DTLS key managed | N/A | No WebRTC | — |
| 17.2.2 | 2 | Approved DTLS suites | N/A | No WebRTC | — |
| 17.2.3 | 2 | SRTP authentication | N/A | No WebRTC | — |
| 17.2.4 | 2 | Malformed SRTP handled | N/A | No WebRTC | — |
| 17.3.1 | 2 | Signaling under flood | N/A | No WebRTC | — |
| 17.3.2 | 2 | Malformed signaling handled | N/A | No WebRTC | — |

## Level 3 items Tero adopts

ADR 0001 adopts a few Level 3 requirements because they are cheap.

| ID | Requirement | Status | How | Test |
|---|---|---|---|---|
| 6.3.5 | Tell the user about suspicious logins | Met | Failed logins, rate limits and resets are on the dashboard home and in the journal; v1 has no email | `TestEventsRecordedWithoutSecrets`, e2e `Events recorded / No secrets in events (v5.0.0-6.3.4, v5.0.0-6.3.5, v5.0.0-6.3.7)` |
| 6.3.7 | Tell the user after a credential change | Met | The reset event is shown at the next login | e2e `Recovery visible after the fact` |
| 6.3.3 | A phishing-resistant factor | Later | Passkeys, right after v1 | — |
| 13.3.3 | Keys in an isolated module | Later | Slice 5: the key is sealed with `systemd-creds` | — |
| 13.3.4 | Secret rotation | Later | Slice 5: `sudo tero rotate-key` | — |
