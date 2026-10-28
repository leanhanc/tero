// Package deploylog stores each deploy's build log as a file the agent
// writes and the service streams to the dashboard. Logs are plain text: ANSI
// escape codes are removed and each log is capped in size.
package deploylog

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"syscall"
)

// Dir holds one log file per deploy, in the directory the service and the
// agent share. Init creates it with group tero-shared, so the agent writes
// each log and the service can read it.
const Dir = "/var/lib/tero-shared/logs/deploys"

// logFileMode lets the group, which holds the service, read a log.
const logFileMode = 0o640

// MaxSize caps a log so a noisy build can't fill the disk.
const MaxSize = 10 << 20

const truncatedNote = "\n[Tero: the log reached 10 MB, so the rest of this deploy's output isn't kept.]\n"

// ansiEscape matches CSI and OSC sequences, which is what build tools use
// for colour and progress bars.
var ansiEscape = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b[@-Z\\-_]`)

// Path is the log file of a deploy.
func Path(dir string, deployID int64) string {
	return filepath.Join(dir, fmt.Sprintf("%d.log", deployID))
}

// Writer appends plain text to a deploy's log. It is safe for concurrent
// use, since BuildKit's progress and Tero's own lines write at once.
type Writer struct {
	mutex       sync.Mutex
	file        *os.File
	written     int64
	isTruncated bool
}

// Create starts a deploy's log, replacing any earlier file of that name.
func Create(dir string, deployID int64) (*Writer, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}

	// The log is created afresh, never through a link someone left in its
	// place.
	path := Path(dir, deployID)
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, logFileMode)
	if err != nil {
		return nil, err
	}
	// The agent's umask would otherwise leave the service unable to read it.
	if err := file.Chmod(logFileMode); err != nil {
		file.Close()
		return nil, err
	}

	return &Writer{file: file}, nil
}

// Write always reports len(p) written, so a full log never fails the
// build that is writing to it.
func (w *Writer) Write(p []byte) (int, error) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	if w.isTruncated {
		return len(p), nil
	}

	plain := ansiEscape.ReplaceAll(p, nil)
	room := MaxSize - w.written
	if int64(len(plain)) > room {
		plain = plain[:room]
		w.isTruncated = true
	}

	written, err := w.file.Write(plain)
	w.written += int64(written)
	if err != nil {
		return len(p), nil
	}
	if w.isTruncated {
		_, _ = io.WriteString(w.file, truncatedNote)
	}

	return len(p), nil
}

// Close finishes the log.
func (w *Writer) Close() error {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	return w.file.Close()
}
