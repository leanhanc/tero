# domains Specification

## Purpose
What later slices add to domains: custom domains with automatic HTTPS (slice 8). The default nip.io address and the HTTP redirect are in the slice 2 change.

## Requirements

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
