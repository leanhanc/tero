package appentry

import (
	"encoding/json"
	"testing"
)

// app-variables: Multi-line value
func TestEncodeKeepsValuesByteForByte(t *testing.T) {
	want := map[string]string{"PEM": "-----BEGIN KEY-----\nabc=\n-----END KEY-----\n", "QUOTED": `a"b'c\d`, "EMPTY": ""}

	encoded, err := Encode(want)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	for name, value := range want {
		if got[name] != value {
			t.Errorf("%s = %q, want %q", name, got[name], value)
		}
	}
}
