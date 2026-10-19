## ADDED Requirements

### Requirement: Create a web app from a public GitHub repository
The admin SHALL be able to create a web app from the dashboard by giving a public GitHub repository URL, a branch, a name and optionally a root directory inside the repository. Creating the app SHALL start its first deploy.

#### Scenario: Create a web app
- **WHEN** the admin creates an app from `https://github.com/<owner>/<repo>` with an existing branch and a free name
- **THEN** the app is listed in the dashboard and its first deploy is queued

#### Scenario: Default branch suggested
- **WHEN** the admin enters a repository URL in the new-app form
- **THEN** the branch field is filled with the repository's default branch and the name field with the repository name

#### Scenario: Not a GitHub URL
- **WHEN** the admin enters a URL whose host is not `github.com`, or that is not of the form `https://github.com/<owner>/<repo>`
- **THEN** the app is not created and the form says only public GitHub repository URLs are accepted
- **AND** the server makes no outbound request to the given host (ASVS v5.0.0-1.3.6)

#### Scenario: Repository or branch not found
- **WHEN** the repository does not exist, is private, or has no such branch
- **THEN** the app is not created and the form says which one was not found

### Requirement: Root directory
An app's root directory SHALL default to the repository root. When set, Tero SHALL build only that directory, and SHALL refuse a path that is absolute, leaves the repository, or does not exist on the branch.

#### Scenario: App in a subfolder
- **WHEN** the admin creates an app with root directory `e2e/fixtures/bun` from `https://github.com/leanhanc/tero`
- **THEN** Railpack plans from that folder and the app answers on its address

#### Scenario: Path outside the repository
- **WHEN** the admin enters `../`, an absolute path, or a folder that doesn't exist on the branch
- **THEN** the app is not created and the form says the folder wasn't found in the repository

### Requirement: App names
An app name SHALL be 1 to 30 characters of lowercase letters, digits and hyphens, start with a letter, not end with a hyphen, and be unique on the server. It SHALL NOT change after creation.

#### Scenario: Invalid name
- **WHEN** the admin enters a name with uppercase letters, dots, or more than 30 characters
- **THEN** the app is not created and the form says what a valid name looks like

#### Scenario: Name taken
- **WHEN** the admin enters the name of an existing app
- **THEN** the app is not created and the form says the name is in use

### Requirement: Web apps listen on PORT
A web app SHALL receive HTTP traffic from the built-in proxy on the port given to it in the `PORT` environment variable.

#### Scenario: PORT set
- **WHEN** a web app's container starts
- **THEN** its process sees `PORT` set, and HTTPS requests to the app's address reach that port

### Requirement: App list and app page
The dashboard SHALL list every app with its name, address and the state of its latest deploy, and each app SHALL have a page with its repository, branch, address, live commit, recent deploys and a Redeploy button.

#### Scenario: App list
- **WHEN** the admin opens the dashboard home with two apps created
- **THEN** both are listed with name, address and latest deploy state

#### Scenario: App page
- **WHEN** the admin opens an app
- **THEN** the page shows its repository, branch, root directory, address as a link, the live commit, and its deploys newest first

### Requirement: Apps survive restarts
Live apps SHALL keep running, or be started again, across a crash of the app, a restart of Tero, and a reboot of the server.

#### Scenario: App process exits
- **WHEN** a live app's process exits
- **THEN** Tero starts its container again and the app answers again

#### Scenario: Reboot
- **WHEN** the server reboots
- **THEN** every live app answers on its address again without manual steps
