# domains Specification

## Purpose
Put each web app on a domain with HTTPS handled automatically by the Caddy proxy built into the Tero binary. Every web app gets a working default address; the admin can add custom domains.

## Requirements

### Requirement: Default app address
Every web app SHALL get a default HTTPS address under the server's sslip.io name, so it is reachable before any DNS is set up.

#### Scenario: New web app reachable without DNS
- **WHEN** a web app's first deploy goes live and no custom domain is set
- **THEN** it answers over HTTPS on `<app>.<public-ip>.sslip.io` with a valid certificate

### Requirement: Custom domains with automatic HTTPS
The admin SHALL be able to add custom domains to a web app. Tero SHALL obtain and renew certificates for them automatically.

#### Scenario: Add a domain with DNS pointed
- **WHEN** the admin adds `app.example.com` to a web app and the domain's DNS points at the server
- **THEN** the app answers over HTTPS on that domain with a publicly trusted certificate

#### Scenario: DNS not pointed yet
- **WHEN** the admin adds a domain whose DNS does not point at the server
- **THEN** the dashboard shows the domain as pending with the record the admin needs to create, and the app's other addresses keep working

#### Scenario: Remove a domain
- **WHEN** the admin removes a domain from an app
- **THEN** requests to that domain no longer reach the app

#### Scenario: Domain already in use
- **WHEN** the admin adds a domain already attached to another app or to the dashboard
- **THEN** Tero rejects it and says which app uses it

### Requirement: HTTP redirects to HTTPS
Plain HTTP requests to any app domain SHALL redirect to HTTPS.

#### Scenario: HTTP request to an app
- **WHEN** a client requests an app domain over HTTP
- **THEN** it receives a redirect to the same URL over HTTPS
