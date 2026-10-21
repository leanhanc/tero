## ADDED Requirements

### Requirement: Default app address
Every web app SHALL get the HTTPS address `<app>.<public-ip>.nip.io`, using the public IP recorded at init, so it is reachable without any DNS setup and whatever dashboard domain was chosen.

#### Scenario: New web app reachable without DNS
- **WHEN** a web app's first deploy goes live
- **THEN** it answers over HTTPS on `<app>.<public-ip>.nip.io` with a certificate from the server's ACME issuer

#### Scenario: Dashboard on a custom domain
- **WHEN** the dashboard was set up on a custom domain and a web app goes live
- **THEN** the app still answers on `<app>.<public-ip>.nip.io`

#### Scenario: No route before live
- **WHEN** an app has no live deploy
- **THEN** requests to its address get a Tero page saying the app isn't deployed yet, not the dashboard

### Requirement: HTTP redirects to HTTPS
Plain HTTP requests to any app address SHALL redirect to HTTPS.

#### Scenario: HTTP request to an app
- **WHEN** a client requests an app address over HTTP
- **THEN** it receives a redirect to the same URL over HTTPS

### Requirement: App traffic kept apart from the dashboard
Requests to an app address SHALL reach only that app, and the dashboard and its API SHALL answer only on the dashboard domain.

#### Scenario: Dashboard API on an app host
- **WHEN** a client requests `/api/` paths on an app's address
- **THEN** the request goes to the app, and no dashboard handler or session cookie is involved
