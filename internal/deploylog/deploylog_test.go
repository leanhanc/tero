package deploylog

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// build-pipeline: Log rendered as text (the ANSI half; the dashboard renders
// it as text)
func TestWriterStripsANSICodes(t *testing.T) {
	dir := t.TempDir()
	writer, err := Create(dir, 7)
	if err != nil {
		t.Fatal(err)
	}

	writer.Write([]byte("\x1b[1;32m#3 DONE\x1b[0m 0.4s\n\x1b]0;title\x07<b>npm</b>\n"))
	writer.Close()

	got, _ := os.ReadFile(Path(dir, 7))
	if string(got) != "#3 DONE 0.4s\n<b>npm</b>\n" {
		t.Fatalf("log = %q", got)
	}
}

func TestWriterCapsTheLog(t *testing.T) {
	dir := t.TempDir()
	writer, err := Create(dir, 8)
	if err != nil {
		t.Fatal(err)
	}

	chunk := bytes.Repeat([]byte("x"), 1<<20)
	for range 12 {
		if n, err := writer.Write(chunk); n != len(chunk) || err != nil {
			t.Fatalf("Write = %d, %v", n, err)
		}
	}
	writer.Close()

	got, _ := os.ReadFile(Path(dir, 8))
	if len(got) != MaxSize+len(truncatedNote) || !strings.HasSuffix(string(got), truncatedNote) {
		t.Fatalf("log is %d bytes, want %d ending with the note", len(got), MaxSize+len(truncatedNote))
	}
}
