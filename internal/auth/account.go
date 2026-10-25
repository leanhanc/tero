package auth

import (
	"context"
	"encoding/json"

	"github.com/leanhanc/tero/internal/keys"
)

// SessionInfo is one entry of the session list.
type SessionInfo = keys.SessionInfo

// PasskeyInfo is one entry of the passkey list.
type PasskeyInfo = keys.PasskeyInfo

// Every call here passes the caller's session token, so the key service only
// acts for a live session, and for a confirmed one where it must.

// Confirm checks the password and a code, so the session may end sessions
// and manage passkeys for the next few minutes.
func (s *Service) Confirm(ctx context.Context, session string, browser Browser, givenPassword, code string) error {
	return fromKeys(s.keys.Confirm(ctx, session, browser, givenPassword, code))
}

// ChangePassword checks the new password against the password policy, then
// has the key service check the current password and code and store it.
func (s *Service) ChangePassword(ctx context.Context, session string, browser Browser, currentPassword, code, newPassword string, isEndingOthers bool) error {
	if err := s.checkNewPassword(ctx, browser.IP, newPassword); err != nil {
		return err
	}

	return fromKeys(s.keys.ChangePassword(ctx, session, browser, currentPassword, code, newPassword, isEndingOthers))
}

func (s *Service) Sessions(ctx context.Context, session string) ([]SessionInfo, error) {
	sessions, err := s.keys.ListSessions(ctx, session)
	return sessions, fromKeys(err)
}

// EndSessions ends the session with id, or every other one when isAllOthers
// is set.
func (s *Service) EndSessions(ctx context.Context, session, ip, id string, isAllOthers bool) error {
	_, err := s.keys.RevokeSession(ctx, session, ip, id, isAllOthers)
	return fromKeys(err)
}

func (s *Service) Passkeys(ctx context.Context, session string) ([]PasskeyInfo, error) {
	passkeys, err := s.keys.ListPasskeys(ctx, session)
	return passkeys, fromKeys(err)
}

func (s *Service) BeginPasskeyRegistration(ctx context.Context, session string) (PasskeyOptions, error) {
	options, err := s.keys.BeginPasskeyRegistration(ctx, session)
	return options, fromKeys(err)
}

func (s *Service) FinishPasskeyRegistration(ctx context.Context, session, ip, challengeID, name string, answer json.RawMessage, isEndingOthers bool) (PasskeyInfo, error) {
	info, err := s.keys.FinishPasskeyRegistration(ctx, session, ip, challengeID, name, answer, isEndingOthers)
	return info, fromKeys(err)
}

func (s *Service) RenamePasskey(ctx context.Context, session, id, name string) error {
	return fromKeys(s.keys.RenamePasskey(ctx, session, id, name))
}

func (s *Service) RemovePasskey(ctx context.Context, session, ip, id string, isEndingOthers bool) error {
	return fromKeys(s.keys.RemovePasskey(ctx, session, ip, id, isEndingOthers))
}
