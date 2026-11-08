## ADDED Requirements

### Requirement: Deploys that restart the live image
A deploy with trigger `variables` that needs no build, queued only by the admin's redeploy of pending changes, SHALL run the live release's image through the same start check and switch as any deploy. Removing the old release SHALL keep an image another deploy uses. A queued deploy that needs a build SHALL keep needing one whatever trigger updates it.

#### Scenario: Old release removed, image kept
- **WHEN** a restart deploy goes live
- **THEN** the old container is gone and the image remains, and the app restarts from it after a reboot

#### Scenario: Failed restart keeps the image
- **WHEN** a restart deploy fails its start check
- **THEN** the image remains and the previous release keeps serving

#### Scenario: Variable change while a push is queued
- **WHEN** the admin redeploys runtime-only changes while the app has a queued deploy from a push
- **THEN** one deploy runs, and it builds the branch head

#### Scenario: Variable change while building
- **WHEN** the admin redeploys runtime-only changes while the app's deploy is building
- **THEN** a deploy is queued behind it, and restarts the newer image with the new values

### Requirement: Apps with variables after a reboot
After a reboot, the agent SHALL get an app's values from the key service before starting its container. While the key service can't answer or can't open a value, that app SHALL stay stopped, its page SHALL say why, and the agent SHALL keep trying. Apps with no variables SHALL start regardless. A running app SHALL never be stopped by a key service problem.

#### Scenario: Reboot
- **WHEN** the server reboots
- **THEN** apps with variables come back with their values

#### Scenario: Key service down at boot
- **WHEN** the server reboots and `tero-keys` can't start
- **THEN** apps with no variables serve, apps with variables stay stopped and their pages say why, and they start once `tero-keys` answers

#### Scenario: Key service stops
- **WHEN** `tero-keys` stops while apps are running
- **THEN** every app keeps answering

#### Scenario: App exits
- **WHEN** an app with variables exits with `tero-keys` stopped
- **THEN** the agent starts it again with the same values
