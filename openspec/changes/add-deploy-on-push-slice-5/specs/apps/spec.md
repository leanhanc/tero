## MODIFIED Requirements

### Requirement: Create a web app from a public GitHub repository
The admin SHALL be able to create a web app from the dashboard by choosing a repository the GitHub App is installed on, private ones included, or by giving a public GitHub repository URL, plus a branch, a display name, an app name for its address and optionally a root directory inside the repository. Creating the app SHALL queue its first deploy with trigger `create`. An app SHALL store the repository's GitHub id when the App covers it and match the repository by that id.

#### Scenario: Create a web app
- **WHEN** the admin creates an app from `https://github.com/<owner>/<repo>` with an existing branch and a free name
- **THEN** the app is listed in the dashboard and its first deploy is queued

#### Scenario: Create from a private repository
- **WHEN** GitHub is connected and the admin picks a private repository from the list on the new-app form
- **THEN** the branch field is filled with its default branch, and the app is created and deploys

#### Scenario: Default branch suggested
- **WHEN** the admin enters a repository URL or picks a repository in the new-app form
- **THEN** the branch field is filled with the repository's default branch, the display name with the repository name, and the address with an app name made from the display name

#### Scenario: Address follows the display name
- **WHEN** the admin types a display name such as `Tienda Café` and hasn't edited the address
- **THEN** the address field shows `tienda-cafe` and the form previews the app's full address

#### Scenario: Not a GitHub URL
- **WHEN** the admin enters a URL whose host is not `github.com`, or that is not of the form `https://github.com/<owner>/<repo>`
- **THEN** the app is not created and the form says only GitHub repository URLs are accepted
- **AND** the server makes no outbound request to the given host (ASVS v5.0.0-1.3.6)

#### Scenario: Private repository by URL without the App
- **WHEN** the admin enters the URL of a private repository the App isn't installed on, or GitHub isn't connected
- **THEN** the app is not created and the form says to connect GitHub or add the repository to the App's installation

#### Scenario: Repository or branch not found
- **WHEN** the repository does not exist or has no such branch
- **THEN** the app is not created and the form says which one was not found

#### Scenario: Repository renamed
- **WHEN** a repository the App covers is renamed on GitHub after the app was created
- **THEN** pushes to it still deploy the app, and the app page shows the new name after the next push
