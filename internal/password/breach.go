package password

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// pwnedPasswordsAPI is Have I Been Pwned's k-anonymity range API.
const pwnedPasswordsAPI = "https://api.pwnedpasswords.com/range/"

// PwnedPasswords checks passwords against Have I Been Pwned. Only the first 5
// hex characters of the password's SHA-1 hash leave the server, and padding
// hides how many matches the response holds.
type PwnedPasswords struct {
	BaseURL string
	Client  *http.Client
}

// NewPwnedPasswords returns a checker for the public API.
func NewPwnedPasswords() PwnedPasswords {
	return PwnedPasswords{BaseURL: pwnedPasswordsAPI, Client: &http.Client{Timeout: 5 * time.Second}}
}

// IsBreached reports whether password appears in the breach corpus.
func (p PwnedPasswords) IsBreached(ctx context.Context, password string) (bool, error) {
	digest := sha1.Sum([]byte(password))
	hash := strings.ToUpper(hex.EncodeToString(digest[:]))
	prefix, suffix := hash[:5], hash[5:]

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, p.BaseURL+prefix, nil)
	if err != nil {
		return false, err
	}
	request.Header.Set("Add-Padding", "true")
	request.Header.Set("User-Agent", "tero")

	response, err := p.Client.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("breach API answered %s", response.Status)
	}

	return containsSuffix(response, suffix)
}

// containsSuffix scans "SUFFIX:COUNT" lines. Padding lines have a count of 0.
func containsSuffix(response *http.Response, suffix string) (bool, error) {
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		lineSuffix, count, _ := strings.Cut(strings.TrimSpace(scanner.Text()), ":")
		isRealMatch := strings.EqualFold(lineSuffix, suffix) && count != "0"
		if isRealMatch {
			return true, nil
		}
	}

	return false, scanner.Err()
}
