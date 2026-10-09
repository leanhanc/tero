package auth

import "context"

// ResetLogin is `sudo tero reset-login`: it deletes the admin's password and
// TOTP secret, ends every session, forgets trusted devices and login
// throttles, records the reset
// and returns a new setup link. Server settings are not touched. Throttles are
// cleared because whoever runs it has proven root access to the server.
func (s *Service) ResetLogin(ctx context.Context) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	for _, statement := range []string{
		"DELETE FROM admin",
		"DELETE FROM sessions",
		"DELETE FROM trusted_devices",
		"DELETE FROM login_throttles",
		"DELETE FROM setup_links",
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}

	if err := s.record(ctx, Event{At: s.now(), Type: EventLoginReset}); err != nil {
		return "", err
	}

	return s.IssueSetupLink(ctx, "")
}
