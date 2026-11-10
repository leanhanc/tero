# After a suspected breach

Run `sudo tero assume-breach` on the server when you think someone got into the dashboard, the agent or an app. It asks you to type `yes`, or skips the question with `--yes`.

## What it does

- Stops the dashboard, the agent, every app and every other process running as Tero's users.
- Moves aside units, cron jobs and Podman settings those users could have planted, and lists them so you can check them. They are kept in `/var/lib/tero-breach`, readable by root only. Their files in `/tmp`, `/var/tmp` and `/dev/shm` are deleted.
- Ends every dashboard session, forgets every trusted browser, and resets the password, the authenticator and the passkeys. The last line it prints is a new setup link.
- Deletes the dashboard's and the apps' certificates and keys, so new ones are issued.
- Replaces the master key. Your app variables are kept, and listed so you can rotate them.
- Deletes the GitHub connection. Delete the App on GitHub too, then connect again from Settings.
- **Deletes every app's image and container, and the build cache.** Someone running as the agent could have changed any of them, so nothing built before the breach runs again.

## Your apps stay down until you redeploy them

Once the reset is done, each app's last deploy shows as failed, and its address answers with no app behind it. To bring an app back:

1. Open the setup link and set up your login again.
2. Connect GitHub again, if your apps deploy from repositories the GitHub App covers.
3. Rotate each listed variable at its provider and replace it in the dashboard.
4. Redeploy each app. Tero builds it again from its repository.

## If it stops halfway

If a step fails, the dashboard, the agent and the apps stay stopped, even after a reboot, until `sudo tero assume-breach` runs to the end. Fix what the error names and run it again. If only starting Tero again fails at the very end, the reset is done and the setup link is printed: start Tero with `sudo systemctl start tero` once the problem is fixed.

## Outside the server

The command prints what is left for you to do elsewhere: install the latest Tero if the breach may have come from a bug in it, change the old dashboard password wherever else you used it, remove the old authenticator entry and passkeys, and check the SSH keys of every user on the server.
