// Package password holds the dashboard password rules (OWASP ASVS 5.0.0
// V6.2) and stores passwords as Argon2id hashes (RFC 9106).
package password

import (
	"bufio"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	// MinLength is the shortest password accepted (v5.0.0-6.2.1).
	MinLength = 15
	// maxLength is far above the 64 characters ASVS requires accepting
	// (v5.0.0-6.2.9); it only bounds the work done per request.
	maxLength = 1024
)

var (
	ErrTooShort = fmt.Errorf("Use at least %d characters", MinLength)
	ErrTooLong  = fmt.Errorf("Use at most %d characters", maxLength)
	ErrBreached = errors.New("This password is known from data breaches. Choose a different one")
)

// commonPasswords lists well-known passwords, one per line, that are at least
// MinLength characters long; shorter ones are already refused by length.
//
//go:embed common-passwords.txt
var commonPasswordsFile string

var commonPasswords = parseCommonPasswords(commonPasswordsFile)

// BreachChecker reports whether a password appears in a breach corpus.
type BreachChecker interface {
	IsBreached(ctx context.Context, password string) (bool, error)
}

// Policy checks new passwords. There are no composition rules
// (v5.0.0-6.2.5): only length and known-breach checks.
type Policy struct {
	Breaches BreachChecker
}

// Result says whether the online breach check could run. When it couldn't,
// the bundled list alone decided.
type Result struct {
	IsOnlineCheckSkipped bool
}

// Check returns ErrTooShort, ErrTooLong or ErrBreached when password can't be
// used.
func (p Policy) Check(ctx context.Context, password string) (Result, error) {
	length := utf8.RuneCountInString(password)
	if length < MinLength {
		return Result{}, ErrTooShort
	}
	if length > maxLength {
		return Result{}, ErrTooLong
	}

	if commonPasswords[strings.ToLower(password)] {
		return Result{}, ErrBreached
	}

	if p.Breaches == nil {
		return Result{IsOnlineCheckSkipped: true}, nil
	}

	isBreached, err := p.Breaches.IsBreached(ctx, password)
	if err != nil {
		return Result{IsOnlineCheckSkipped: true}, nil
	}
	if isBreached {
		return Result{}, ErrBreached
	}

	return Result{}, nil
}

func parseCommonPasswords(contents string) map[string]bool {
	passwords := map[string]bool{}
	scanner := bufio.NewScanner(strings.NewReader(contents))
	for scanner.Scan() {
		line := scanner.Text()
		isComment := strings.HasPrefix(line, "#")
		if line == "" || isComment {
			continue
		}
		passwords[strings.ToLower(line)] = true
	}

	return passwords
}
