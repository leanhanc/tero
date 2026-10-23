package auth

import (
	"context"
	"errors"

	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/totp"
)

// Enrolment is what the setup page shows so the admin can add Tero to their
// authenticator app.
type Enrolment struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

// SetupLink is the URL for a setup token root got from the key service. The
// token is in the URL fragment, so browsers never send it in requests, logs
// or Referer headers; the dashboard reads it from there.
func SetupLink(domain, token string) string {
	return "https://" + domain + "/setup#" + token
}

// BeginSetup checks the chosen password against the length, blocklist and
// breach rules, then has the key service hash it and create a TOTP secret to
// enrol. Nothing is created until FinishSetup confirms a code from that
// secret. The key service only accepts this for a link root issued while no
// admin exists.
func (s *Service) BeginSetup(ctx context.Context, ip, token, newPassword string) (Enrolment, error) {
	if err := s.checkThrottle(ctx, []throttleKey{ipKey(ip)}); err != nil {
		return Enrolment{}, err
	}

	result, err := s.passwords.Check(ctx, newPassword)
	if err != nil {
		return Enrolment{}, err
	}
	if result.IsOnlineCheckSkipped {
		s.record(ctx, keys.EventBreachCheckSkipped, ip)
	}

	enrolment, err := s.keys.BeginSetup(ctx, token, newPassword)
	if err != nil {
		return Enrolment{}, fromKeys(err)
	}

	return Enrolment{Secret: enrolment.Secret, URI: totp.URI(enrolment.Secret, "Tero", "admin@"+s.domain)}, nil
}

// FinishSetup has the key service confirm a TOTP code from the enrolled
// secret and create the admin, then logs the admin in, trusting this
// browser. A wrong code is charged to the throttle like a failed login, so
// the six-digit code can't be guessed at speed.
func (s *Service) FinishSetup(ctx context.Context, ip, token, code string) (LoginResult, error) {
	throttles := []throttleKey{ipKey(ip), accountKey}
	if err := s.checkThrottle(ctx, throttles); err != nil {
		return LoginResult{}, err
	}

	charged, err := s.chargeAttempt(ctx, throttles)
	if err != nil {
		return LoginResult{}, err
	}

	if err := fromKeys(s.keys.FinishSetup(ctx, ip, token, code)); err != nil {
		if errors.Is(err, ErrInvalidCode) {
			s.recordLockout(ctx, ip, charged)
			return LoginResult{}, err
		}
		// An invalid link or a key service that is down says nothing about
		// the code, so its failure is taken back.
		if refundErr := s.refund(ctx, charged); refundErr != nil {
			return LoginResult{}, refundErr
		}
		return LoginResult{}, err
	}

	result, err := s.startSession(ctx, "", false)
	if err != nil {
		return LoginResult{}, err
	}

	return result, s.forgive(ctx, charged)
}
