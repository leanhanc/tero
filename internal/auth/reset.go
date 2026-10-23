package auth

import "context"

// ResetLogin is the dashboard's part of `sudo tero reset-login` and `sudo
// tero assume-breach`: it ends every session and forgets trusted devices and
// login throttles. The root command resets the admin's credentials in the
// key service itself. Throttles are cleared because whoever runs it has
// proven root access to the server.
func (s *Service) ResetLogin(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, statement := range []string{
		"DELETE FROM sessions",
		"DELETE FROM trusted_devices",
		"DELETE FROM login_throttles",
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}

	return tx.Commit()
}
