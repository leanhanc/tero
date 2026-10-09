package password

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

type fakeBreaches struct {
	breached map[string]bool
	err      error
}

func (f fakeBreaches) IsBreached(_ context.Context, password string) (bool, error) {
	return f.breached[password], f.err
}

func TestPolicy_v5_0_0_6_2_1_RejectsFourteenCharacters(t *testing.T) {
	_, err := Policy{}.Check(context.Background(), strings.Repeat("x", 14))
	if !errors.Is(err, ErrTooShort) {
		t.Fatalf("Check() = %v, want ErrTooShort", err)
	}
	if !strings.Contains(err.Error(), "15") {
		t.Errorf("message %q does not state the 15-character minimum", err)
	}
}

func TestPolicy_v5_0_0_6_2_9_AcceptsSixtyFourLowercaseLetters(t *testing.T) {
	password := "correctlylongbutonlylowercaselettersthatnobodyhaseverusedbeforex"
	if len(password) != 64 {
		t.Fatalf("test password has %d characters", len(password))
	}

	if _, err := (Policy{Breaches: fakeBreaches{}}).Check(context.Background(), password); err != nil {
		t.Fatalf("Check() = %v", err)
	}
}

func TestPolicy_v5_0_0_6_2_5_HasNoCompositionRules(t *testing.T) {
	for _, password := range []string{"onlylowercaseletters", "ONLYUPPERCASELETTERS", "with spaces in it ok", "ñandú ünïcödé pässwörd"} {
		if _, err := (Policy{Breaches: fakeBreaches{}}).Check(context.Background(), password); err != nil {
			t.Errorf("Check(%q) = %v", password, err)
		}
	}
}

func TestPolicy_v5_0_0_6_2_12_RejectsBundledCommonPassword(t *testing.T) {
	_, err := Policy{}.Check(context.Background(), "1QAZ2WSX3EDC4RFV")
	if !errors.Is(err, ErrBreached) {
		t.Fatalf("Check() = %v, want ErrBreached", err)
	}
}

func TestPolicy_v5_0_0_6_2_12_RejectsPasswordFromBreachAPI(t *testing.T) {
	breaches := fakeBreaches{breached: map[string]bool{"leakedbutlongenough": true}}

	_, err := (Policy{Breaches: breaches}).Check(context.Background(), "leakedbutlongenough")
	if !errors.Is(err, ErrBreached) {
		t.Fatalf("Check() = %v, want ErrBreached", err)
	}
}

func TestPolicyFallsBackToBundledListWhenAPIFails(t *testing.T) {
	breaches := fakeBreaches{err: errors.New("unreachable")}

	result, err := (Policy{Breaches: breaches}).Check(context.Background(), "an unusual but fine passphrase")
	if err != nil {
		t.Fatalf("Check() = %v", err)
	}
	if !result.IsOnlineCheckSkipped {
		t.Error("the skipped online check is not reported")
	}
}

func TestPwnedPasswords_v5_0_0_6_2_12_SendsOnlyHashPrefix(t *testing.T) {
	const password = "a password that should stay on the server"
	digest := sha1.Sum([]byte(password))
	hash := strings.ToUpper(hex.EncodeToString(digest[:]))

	var requestedPath, padding string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		padding = r.Header.Get("Add-Padding")
		w.Write([]byte(hash[5:] + ":3\r\nFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF:0\r\n"))
	}))
	defer server.Close()

	checker := PwnedPasswords{BaseURL: server.URL + "/range/", Client: server.Client()}
	isBreached, err := checker.IsBreached(context.Background(), password)
	if err != nil {
		t.Fatal(err)
	}

	if requestedPath != "/range/"+hash[:5] {
		t.Errorf("requested %q, want only the 5-character prefix", requestedPath)
	}
	if padding != "true" {
		t.Errorf("Add-Padding = %q", padding)
	}
	if !isBreached {
		t.Error("matching suffix not reported as breached")
	}
}

func TestPwnedPasswordsIgnoresPaddingEntries(t *testing.T) {
	const password = "padding only matches nothing here"
	digest := sha1.Sum([]byte(password))
	suffix := strings.ToUpper(hex.EncodeToString(digest[:]))[5:]

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(suffix + ":0\r\n"))
	}))
	defer server.Close()

	checker := PwnedPasswords{BaseURL: server.URL + "/", Client: server.Client()}
	if isBreached, _ := checker.IsBreached(context.Background(), password); isBreached {
		t.Error("a padding entry counted as a breach")
	}
}

func TestHashIsArgon2idAndVerifies(t *testing.T) {
	hash, err := Hash(context.Background(), "the admin's long passphrase")
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(hash, "$argon2id$v=19$m=65536,t=3,p=4$") {
		t.Errorf("hash = %q", hash)
	}
	if strings.Contains(hash, "passphrase") {
		t.Error("hash contains the plaintext")
	}

	isMatch, err := Verify(context.Background(), "the admin's long passphrase", hash)
	if err != nil || !isMatch {
		t.Errorf("Verify(correct) = %v, %v", isMatch, err)
	}
	isMatch, _ = Verify(context.Background(), "a different passphrase", hash)
	if isMatch {
		t.Error("Verify(wrong) = true")
	}
}

func TestHashingIsBoundedToTwoAtOnce(t *testing.T) {
	releaseFirst, _ := acquireSlot(context.Background())
	releaseSecond, _ := acquireSlot(context.Background())
	defer releaseFirst()
	defer releaseSecond()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Verify(ctx, "anything at all here", "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0c2FsdA$a2V5"); err == nil {
		t.Fatal("a third hash ran while two slots were taken")
	}
}

// ASVS asks for at least the 3,000 most common passwords that meet the
// policy; shorter entries would be dead weight, since length refuses them.
func TestBundledListCoversTopThreeThousandPolicyPasswords(t *testing.T) {
	if len(commonPasswords) < 3000 {
		t.Errorf("the bundled list has %d passwords, want at least 3000", len(commonPasswords))
	}
	for password := range commonPasswords {
		if utf8.RuneCountInString(password) < MinLength {
			t.Errorf("%q is shorter than the minimum length", password)
		}
	}
}

func TestBundledListIsCheckedEvenWhenAPIAnswers(t *testing.T) {
	_, err := (Policy{Breaches: fakeBreaches{}}).Check(context.Background(), "1qaz2wsx3edc4rfv")
	if !errors.Is(err, ErrBreached) {
		t.Fatalf("Check() with a reachable API = %v, want ErrBreached", err)
	}
}
