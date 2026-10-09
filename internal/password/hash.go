package password

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// argon2Params are RFC 9106's second recommended option. They are stored in
// every hash, so raising them later keeps old hashes verifiable.
type argon2Params struct {
	memoryKiB uint32
	time      uint32
	threads   uint8
}

var currentParams = argon2Params{memoryKiB: 64 * 1024, time: 3, threads: 4}

const (
	saltLength = 16
	keyLength  = 32
)

var errMalformedHash = errors.New("malformed password hash")

// ErrBusy means every hashing slot stayed taken while the caller waited.
var ErrBusy = errors.New("The server is busy. Try again in a few seconds")

// hashingSlots bounds how many hashes run at once. Each one takes 64 MiB, so
// without a bound a flood of login requests could exhaust the server's
// memory.
var hashingSlots = make(chan struct{}, 2)

// slotWait is how long a request waits for a hashing slot.
const slotWait = 10 * time.Second

func acquireSlot(ctx context.Context) (func(), error) {
	timer := time.NewTimer(slotWait)
	defer timer.Stop()

	select {
	case hashingSlots <- struct{}{}:
		return func() { <-hashingSlots }, nil
	case <-timer.C:
		return nil, ErrBusy
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Hash returns an Argon2id hash of password in the PHC string format, for
// example "$argon2id$v=19$m=65536,t=3,p=4$<salt>$<key>".
func Hash(ctx context.Context, password string) (string, error) {
	release, err := acquireSlot(ctx)
	if err != nil {
		return "", err
	}
	defer release()

	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	key := argon2.IDKey([]byte(password), salt, currentParams.time, currentParams.memoryKiB, currentParams.threads, keyLength)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, currentParams.memoryKiB, currentParams.time, currentParams.threads,
		encode(salt), encode(key)), nil
}

// Verify reports whether password matches hash, comparing in constant time.
func Verify(ctx context.Context, password, hash string) (bool, error) {
	params, salt, wantKey, err := parseHash(hash)
	if err != nil {
		return false, err
	}

	release, err := acquireSlot(ctx)
	if err != nil {
		return false, err
	}
	defer release()

	key := argon2.IDKey([]byte(password), salt, params.time, params.memoryKiB, params.threads, uint32(len(wantKey)))
	isMatch := subtle.ConstantTimeCompare(key, wantKey) == 1

	return isMatch, nil
}

func parseHash(hash string) (argon2Params, []byte, []byte, error) {
	parts := strings.Split(hash, "$")
	isArgon2id := len(parts) == 6 && parts[1] == "argon2id"
	if !isArgon2id {
		return argon2Params{}, nil, nil, errMalformedHash
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return argon2Params{}, nil, nil, errMalformedHash
	}

	var params argon2Params
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &params.memoryKiB, &params.time, &params.threads); err != nil {
		return argon2Params{}, nil, nil, errMalformedHash
	}

	salt, saltErr := decode(parts[4])
	key, keyErr := decode(parts[5])
	if saltErr != nil || keyErr != nil {
		return argon2Params{}, nil, nil, errMalformedHash
	}

	return params, salt, key, nil
}

func encode(data []byte) string {
	return base64.RawStdEncoding.EncodeToString(data)
}

func decode(text string) ([]byte, error) {
	return base64.RawStdEncoding.DecodeString(text)
}
