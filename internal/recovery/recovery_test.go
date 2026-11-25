package recovery

import (
	"errors"
	"testing"
)

func TestSealedOnlyOpensWithItsKey(t *testing.T) {
	key, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	other, err := Generate()
	if err != nil {
		t.Fatal(err)
	}

	sealed, err := Seal(key.Recipient, []byte("apps"))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := Open("  "+key.Secret+"\n", sealed)
	if err != nil || string(opened) != "apps" {
		t.Fatalf("Open = %q, %v", opened, err)
	}

	if _, err := Open(other.Secret, sealed); !errors.Is(err, ErrWrongKey) {
		t.Errorf("another key: %v, want ErrWrongKey", err)
	}
	if _, err := Open("not a key", sealed); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("garbage key: %v, want ErrInvalidKey", err)
	}

	tampered := append([]byte{}, sealed...)
	tampered[len(tampered)-1] ^= 1
	if _, err := Open(key.Secret, tampered); err == nil {
		t.Error("a changed backup opened")
	}
}
