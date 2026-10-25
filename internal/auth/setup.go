package auth

import (
	"context"

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

// BeginSetup checks the chosen password against the password policy, then
// has the key service hash it and create a TOTP secret to enrol. Nothing is
// created until FinishSetup confirms a code from that secret. The key
// service only accepts this for a link root issued while no admin exists.
func (s *Service) BeginSetup(ctx context.Context, ip, token, newPassword string) (Enrolment, error) {
	if err := s.checkNewPassword(ctx, ip, newPassword); err != nil {
		return Enrolment{}, err
	}

	enrolment, err := s.keys.BeginSetup(ctx, ip, token, newPassword)
	if err != nil {
		return Enrolment{}, fromKeys(err)
	}

	return Enrolment{Secret: enrolment.Secret, URI: totp.URI(enrolment.Secret, "Tero", "admin@"+s.domain)}, nil
}

// FinishSetup has the key service confirm a TOTP code from the enrolled
// secret, create the admin and log the admin in, trusting this browser.
func (s *Service) FinishSetup(ctx context.Context, browser Browser, token, code string) (LoginResult, error) {
	result, err := s.keys.FinishSetup(ctx, browser, token, code)
	return result, fromKeys(err)
}
