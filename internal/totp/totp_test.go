package totp

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

// rfc6238Secret is the SHA-1 test key from RFC 6238 appendix B.
var rfc6238Secret = base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))

func TestCodeMatchesRFC6238Vectors(t *testing.T) {
	vectors := map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"}
	for unix, want := range vectors {
		got, err := Code(rfc6238Secret, time.Unix(unix, 0))
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Errorf("Code at %d = %s, want %s", unix, got, want)
		}
	}
}

func TestVerifyAcceptsAdjacentStepsForClockDrift(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	earlier, _ := Code(rfc6238Secret, now.Add(-30*time.Second))

	if _, isValid := Verify(rfc6238Secret, earlier, now, 0); !isValid {
		t.Error("the previous code is refused")
	}
}

func TestVerifyRefusesOldCodes(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	old, _ := Code(rfc6238Secret, now.Add(-2*time.Minute))

	if _, isValid := Verify(rfc6238Secret, old, now, 0); isValid {
		t.Error("a 2-minute-old code is accepted")
	}
}

func TestVerify_v5_0_0_6_5_1_RefusesReplayedCode(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	code, _ := Code(rfc6238Secret, now)

	usedStep, isValid := Verify(rfc6238Secret, code, now, 0)
	if !isValid {
		t.Fatal("first use refused")
	}

	if _, isValid := Verify(rfc6238Secret, code, now.Add(5*time.Second), usedStep); isValid {
		t.Error("the same code is accepted twice")
	}
}

func TestVerifyRefusesMalformedCodes(t *testing.T) {
	for _, code := range []string{"", "12345", "1234567", "12a456", " 123456"} {
		if _, isValid := Verify(rfc6238Secret, code, time.Now(), 0); isValid {
			t.Errorf("Verify(%q) = true", code)
		}
	}
}

func TestURIIsScannable(t *testing.T) {
	uri := URI("ABC", "Tero", "admin@dash.example.com")
	for _, want := range []string{"otpauth://totp/Tero:admin@dash.example.com?", "secret=ABC", "issuer=Tero"} {
		if !strings.Contains(uri, want) {
			t.Errorf("URI %q missing %q", uri, want)
		}
	}
}
