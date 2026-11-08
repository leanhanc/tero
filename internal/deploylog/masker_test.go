package deploylog

import (
	"bytes"
	"strings"
	"testing"
)

// build-pipeline: Build prints a value, App prints a value and exits
func TestMaskerHidesValues(t *testing.T) {
	var out bytes.Buffer
	masker := NewMasker(&out)
	masker.Add([]string{"s3cret-value", "s3cret", ""})

	writes := []string{"start ", "s3cr", "et-val", "ue middle s3cret\n", "line s3"}
	for _, write := range writes {
		if n, err := masker.Write([]byte(write)); err != nil || n != len(write) {
			t.Fatalf("write %q: %d, %v", write, n, err)
		}
	}
	if err := masker.Close(); err != nil {
		t.Fatal(err)
	}

	if got, want := out.String(), "start [hidden] middle [hidden]\nline s3"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestMaskerPassesTextThroughWithNoValues(t *testing.T) {
	var out bytes.Buffer
	masker := NewMasker(&out)

	if _, err := masker.Write([]byte("plain")); err != nil {
		t.Fatal(err)
	}
	if out.String() != "plain" {
		t.Errorf("held back %q", out.String())
	}
}

func TestMaskerDoesntHoldOrdinaryText(t *testing.T) {
	var out bytes.Buffer
	masker := NewMasker(&out)
	masker.Add([]string{"s3cret"})

	if _, err := masker.Write([]byte("building step 4\n")); err != nil {
		t.Fatal(err)
	}
	if out.String() != "building step 4\n" {
		t.Errorf("wrote %q", out.String())
	}
}

func TestMaskerMasksMessagesAndMultiLineValues(t *testing.T) {
	var out bytes.Buffer
	masker := NewMasker(&out)
	pem := "-----BEGIN KEY-----\nabc\n-----END KEY-----"
	masker.Add([]string{pem})

	if got := masker.Mask("failed: " + pem); strings.Contains(got, "abc") {
		t.Errorf("message = %q", got)
	}
	if _, err := masker.Write([]byte(pem + "\n")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "abc") {
		t.Errorf("log = %q", out.String())
	}
}

func TestMaskerMasksAShorterValueHeldAtClose(t *testing.T) {
	var out bytes.Buffer
	masker := NewMasker(&out)
	masker.Add([]string{"secret-short", "secret-short-and-longer"})

	if _, err := masker.Write([]byte("boom: secret-short")); err != nil {
		t.Fatal(err)
	}
	if err := masker.Close(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "boom: [hidden]" {
		t.Errorf("got %q", out.String())
	}
}
