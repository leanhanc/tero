// Package totp implements RFC 6238 time-based one-time passwords with the
// settings every authenticator app supports: SHA-1, 6 digits, 30 seconds.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	period = 30 * time.Second
	digits = 6
	// skewSteps accepts the codes just before and after the current one, for
	// clock drift between the server and the phone.
	skewSteps = 1
)

var secretEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewSecret returns a random 160-bit secret, base32-encoded as authenticator
// apps expect.
func NewSecret() (string, error) {
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return "", err
	}

	return secretEncoding.EncodeToString(secret), nil
}

// URI is the otpauth:// link an authenticator app scans as a QR code.
func URI(secret, issuer, account string) string {
	label := url.PathEscape(issuer + ":" + account)
	query := url.Values{"secret": {secret}, "issuer": {issuer}}

	return "otpauth://totp/" + label + "?" + query.Encode()
}

// Step is the RFC 6238 time step for t.
func Step(t time.Time) int64 {
	return t.Unix() / int64(period/time.Second)
}

// Verify checks code against secret at time now and returns the time step it
// matched. Only steps after lastStep are accepted, so a code that was already
// used, or an older one, is refused (OWASP ASVS v5.0.0-6.5.1).
func Verify(secret, code string, now time.Time, lastStep int64) (int64, bool) {
	key, err := secretEncoding.DecodeString(strings.ToUpper(secret))
	isWellFormedCode := len(code) == digits && strings.Trim(code, "0123456789") == ""
	if err != nil || !isWellFormedCode {
		return 0, false
	}

	currentStep := Step(now)
	for step := currentStep - skewSteps; step <= currentStep+skewSteps; step++ {
		isUnused := step > lastStep
		isMatch := subtle.ConstantTimeCompare([]byte(codeAt(key, step)), []byte(code)) == 1
		if isUnused && isMatch {
			return step, true
		}
	}

	return 0, false
}

// Code returns the code for secret at time t. Tests use it to act as the
// admin's authenticator.
func Code(secret string, t time.Time) (string, error) {
	key, err := secretEncoding.DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", err
	}

	return codeAt(key, Step(t)), nil
}

// codeAt is RFC 4226 HOTP for counter step.
func codeAt(key []byte, step int64) string {
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))

	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff

	return fmt.Sprintf("%0*d", digits, truncated%1_000_000)
}
