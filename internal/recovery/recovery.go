// Package recovery is the server's recovery key and the encryption of its
// backups. The recovery key is an age identity (post-quantum hybrid,
// ML-KEM-768 with X25519) that `sudo tero init` prints once and never stores.
// The server keeps only its recipient, the public half, so it can encrypt
// backups that only whoever holds the printed key can open: not the server
// itself, and not someone who copies its disk.
package recovery

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"

	"filippo.io/age"

	"github.com/leanhanc/tero/internal/hostfs"
)

// RecipientPath holds the recovery key's recipient, readable only by root.
// Nothing running as Tero's users can replace it, so backups can't be
// redirected to someone else's key, or read it: anyone with the recipient
// can make a file that opens with the recovery key.
const RecipientPath = "/etc/tero/recovery-recipient"

// ErrWrongKey means a recovery key doesn't open a backup: it is another
// server's key, or an older key of this server.
var ErrWrongKey = errors.New("this recovery key doesn't open the backup. Use the key printed when the backup's server was set up, or by its latest `sudo tero new-recovery-key`")

// ErrInvalidKey means the text isn't a recovery key at all.
var ErrInvalidKey = errors.New("that isn't a recovery key: it starts with AGE-SECRET-KEY-PQ-1")

// maxPlaintext bounds what Open returns, so a crafted file can't make root
// or the key service fill memory.
const maxPlaintext = 64 << 20

// Key is a new recovery key: Secret is printed once, Recipient is stored.
type Key struct {
	Secret    string
	Recipient string
}

// Generate makes a new recovery key.
func Generate() (Key, error) {
	identity, err := age.GenerateHybridIdentity()
	if err != nil {
		return Key{}, err
	}

	return Key{Secret: identity.String(), Recipient: identity.Recipient().String()}, nil
}

// Seal encrypts plaintext so that only the recovery key behind recipient
// opens it. A changed byte stops the result opening, but it doesn't prove who
// sealed it: anyone with the recipient can seal a file of their own.
func Seal(recipient string, plaintext []byte) ([]byte, error) {
	parsed, err := age.ParseHybridRecipient(strings.TrimSpace(recipient))
	if err != nil {
		return nil, fmt.Errorf("read the recovery key's recipient: %w", err)
	}

	var sealed bytes.Buffer
	writer, err := age.Encrypt(&sealed, parsed)
	if err != nil {
		return nil, err
	}
	if _, err := writer.Write(plaintext); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	return sealed.Bytes(), nil
}

// Open decrypts what Seal encrypted with the recovery key's secret.
func Open(secret string, sealed []byte) ([]byte, error) {
	identity, err := ParseSecret(secret)
	if err != nil {
		return nil, err
	}

	reader, err := age.Decrypt(bytes.NewReader(sealed), identity)
	var noMatch *age.NoIdentityMatchError
	if errors.As(err, &noMatch) {
		return nil, ErrWrongKey
	}
	if err != nil {
		return nil, fmt.Errorf("open the backup: %w", err)
	}

	plaintext, err := io.ReadAll(io.LimitReader(reader, maxPlaintext+1))
	if err != nil {
		return nil, fmt.Errorf("open the backup: %w", err)
	}
	if len(plaintext) > maxPlaintext {
		return nil, errors.New("the backup is larger than any Tero makes")
	}

	return plaintext, nil
}

// ParseSecret reads a recovery key as the admin typed or pasted it.
func ParseSecret(secret string) (*age.HybridIdentity, error) {
	identity, err := age.ParseHybridIdentity(strings.TrimSpace(secret))
	if err != nil {
		return nil, ErrInvalidKey
	}

	return identity, nil
}

// Install makes a new recovery key and stores its recipient, replacing any
// earlier one. Backups made after it open only with the new key.
func Install(files hostfs.FS) (Key, error) {
	key, err := Generate()
	if err != nil {
		return Key{}, err
	}
	if _, err := files.WriteFile(RecipientPath, []byte(key.Recipient+"\n"), 0o600); err != nil {
		return Key{}, fmt.Errorf("save the recovery key's public half: %w", err)
	}

	return key, nil
}

// LoadRecipient reads the stored recipient.
func LoadRecipient(files hostfs.FS) (string, error) {
	recipient, err := files.ReadFile(RecipientPath)
	if errors.Is(err, fs.ErrNotExist) {
		return "", errors.New("this server has no recovery key. Make one with `sudo tero new-recovery-key`")
	}
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(recipient)), nil
}

// Announcement is what root commands print with a new recovery key.
func Announcement(secret string) string {
	return "Your recovery key, shown only this once. Save it in your password manager now:\n\n" +
		"  " + secret + "\n\n" +
		"It is the only way to open backups made with `sudo tero backup`, which move your apps and their variables to another server. Tero doesn't keep it.\n"
}
