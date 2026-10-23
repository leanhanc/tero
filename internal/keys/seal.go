package keys

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"strconv"
)

// keySize is the master key's length: AES-256.
const keySize = 32

// errNoKey means the master key version a value names isn't loaded.
var errNoKey = errors.New("the master key that encrypted this value isn't loaded")

// keyring holds every loaded master key version. Values are always sealed
// with the newest one; older ones only open what they sealed.
type keyring map[int][]byte

func (k keyring) current() (int, bool) {
	newest := 0
	for version := range k {
		newest = max(newest, version)
	}

	return newest, newest > 0
}

// sealed is a value encrypted under one master key version.
type sealed struct {
	version    int
	ciphertext []byte
}

// seal encrypts plaintext with AES-256-GCM under the newest key. The purpose
// and key version are the additional data, so a ciphertext copied to another
// row or relabelled with another version doesn't open (OWASP ASVS
// v5.0.0-11.3.2, v5.0.0-13.3.1).
func (k keyring) seal(purpose string, plaintext []byte) (sealed, error) {
	version, hasKey := k.current()
	if !hasKey {
		return sealed{}, errNoKey
	}

	aead, err := newAEAD(k[version])
	if err != nil {
		return sealed{}, err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return sealed{}, err
	}

	ciphertext := aead.Seal(nonce, nonce, plaintext, additionalData(purpose, version))
	return sealed{version: version, ciphertext: ciphertext}, nil
}

func (k keyring) open(purpose string, value sealed) ([]byte, error) {
	key, isLoaded := k[value.version]
	if !isLoaded {
		return nil, errNoKey
	}

	aead, err := newAEAD(key)
	if err != nil {
		return nil, err
	}

	nonceSize := aead.NonceSize()
	if len(value.ciphertext) < nonceSize {
		return nil, errors.New("the stored value is too short")
	}

	nonce, ciphertext := value.ciphertext[:nonceSize], value.ciphertext[nonceSize:]
	return aead.Open(nil, nonce, ciphertext, additionalData(purpose, value.version))
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != keySize {
		return nil, fmt.Errorf("a master key must be %d bytes, not %d", keySize, len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	return cipher.NewGCM(block)
}

func additionalData(purpose string, version int) []byte {
	return []byte("tero-keys\x00" + purpose + "\x00" + strconv.Itoa(version))
}
