package deploylog

import (
	"bytes"
	"cmp"
	"io"
	"slices"
	"strings"
	"sync"
)

// hiddenValue replaces a value in a log.
const hiddenValue = "[hidden]"

// Masker hides known values in what passes through it, also when a value
// spans two writes. It catches exact matches only, not encoded or split
// values, so it is a safety net and not a boundary. It is safe for concurrent
// use.
type Masker struct {
	mutex  sync.Mutex
	out    io.Writer
	values [][]byte
	held   []byte
}

// NewMasker masks what is written to out. Values are added as they are
// released.
func NewMasker(out io.Writer) *Masker {
	return &Masker{out: out}
}

// Add starts hiding values, longest first so one value inside another doesn't
// leave part of the longer one showing.
func (m *Masker) Add(values []string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, value := range values {
		if value != "" {
			m.values = append(m.values, []byte(value))
		}
	}
	slices.SortStableFunc(m.values, func(a, b []byte) int { return cmp.Compare(len(b), len(a)) })
}

// Write always reports len(p) written. Text that might be the start of a value
// is held back until the next write or Close.
func (m *Masker) Write(p []byte) (int, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	ready, held := m.scan(append(m.held, p...))
	m.held = bytes.Clone(held)

	if len(ready) > 0 {
		if _, err := m.out.Write(ready); err != nil {
			return 0, err
		}
	}

	return len(p), nil
}

// Close writes what is held back. It doesn't close the writer underneath.
func (m *Masker) Close() error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	held := m.held
	m.held = nil
	if len(held) == 0 {
		return nil
	}

	// Nothing more is coming, so a shorter value that could have grown into a
	// longer one is a whole value now.
	_, err := m.out.Write(m.replaceValues(held))
	return err
}

// Mask hides values in a message that doesn't go through the log, such as a
// deploy's failure reason.
func (m *Masker) Mask(message string) string {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	for _, value := range m.values {
		message = strings.ReplaceAll(message, string(value), hiddenValue)
	}

	return message
}

// scan walks text once, left to right, and returns it with values replaced and
// what to hold back: the rest of text from the first place that could still
// grow into a value. Checked before a match, so a value that starts another,
// longer one doesn't let the rest of the longer one out.
func (m *Masker) scan(text []byte) (ready, held []byte) {
	ready = make([]byte, 0, len(text))
	for position := 0; position < len(text); {
		rest := text[position:]
		if m.couldGrowIntoValue(rest) {
			return ready, rest
		}

		matched := m.valueAtStart(rest)
		if matched > 0 {
			ready = append(ready, hiddenValue...)
			position += matched
			continue
		}

		ready = append(ready, text[position])
		position++
	}

	return ready, nil
}

func (m *Masker) couldGrowIntoValue(rest []byte) bool {
	for _, value := range m.values {
		if len(value) > len(rest) && bytes.HasPrefix(value, rest) {
			return true
		}
	}

	return false
}

// valueAtStart is the length of the longest value text starts with, or 0.
// Values are sorted longest first.
func (m *Masker) valueAtStart(text []byte) int {
	for _, value := range m.values {
		if bytes.HasPrefix(text, value) {
			return len(value)
		}
	}

	return 0
}

// replaceValues replaces every value in text, longest first.
func (m *Masker) replaceValues(text []byte) []byte {
	for _, value := range m.values {
		text = bytes.ReplaceAll(text, value, []byte(hiddenValue))
	}

	return text
}
