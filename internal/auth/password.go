package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// The cost of one hash, which is the first choice of the OWASP password cheat sheet for argon2id. The memory of one hash is
// about 19 MiB, and a copy has 128 MiB, so only a few hashes run at once.
const (
	argonMemoryKiB = 19 * 1024
	argonTime      = 2
	argonThreads   = 1
	argonKeyLen    = 32
	argonSaltLen   = 16
)

// ErrBusy is returned when every slot for a hash is taken until the context ends
var ErrBusy = errors.New("too many passwords are being checked")

// Hasher hashes and checks passwords with argon2id and lets only a few run at once
type Hasher struct {
	slots chan struct{}
	dummy string
}

// NewHasher returns a hasher that runs at most concurrency hashes at the same time
func NewHasher(concurrency int) *Hasher {
	// the dummy hash is made once at start, so a failure shows at once and every later call spends the same time
	return &Hasher{slots: make(chan struct{}, max(concurrency, 1)), dummy: encode("not-a-real-password")}
}

func (h *Hasher) acquire(ctx context.Context) error {
	select {
	case h.slots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ErrBusy
	}
}

func (h *Hasher) release() { <-h.slots }

// Hash returns the argon2id hash of a password in the usual encoded form with its salt and cost
func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.release()

	return encode(password), nil
}

// encode hashes a password with a new salt. The random reader of Go does not return an error, it stops the program instead.
func encode(password string) string {
	salt := make([]byte, argonSaltLen)
	_, _ = rand.Read(salt)

	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

// Verify reports whether the password matches the encoded hash. A malformed hash never matches.
func (h *Hasher) Verify(ctx context.Context, password, encoded string) (bool, error) {
	var (
		version        int
		memory, timeC  uint32
		threads        uint8
		saltB64, keyB6 string
	)

	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, nil
	}

	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, nil
	}

	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeC, &threads); err != nil {
		return false, nil
	}

	// a hash that asks for more than this service ever made is refused, so a forged row cannot make a request use all memory
	if memory > 4*argonMemoryKiB || timeC > 10 || threads == 0 || threads > 4 {
		return false, nil
	}

	saltB64, keyB6 = parts[4], parts[5]

	salt, err := base64.RawStdEncoding.DecodeString(saltB64)
	if err != nil {
		return false, nil
	}

	want, err := base64.RawStdEncoding.DecodeString(keyB6)
	if err != nil || len(want) == 0 {
		return false, nil
	}

	if err := h.acquire(ctx); err != nil {
		return false, err
	}
	defer h.release()

	got := argon2.IDKey([]byte(password), salt, timeC, memory, threads, uint32(len(want))) //nolint:gosec // the key length came from base64 of a stored hash

	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// VerifyDummy spends the time of a real check, so a sign in with an unknown email takes as long as one with a wrong password
func (h *Hasher) VerifyDummy(ctx context.Context) error {
	_, err := h.Verify(ctx, "another-password", h.dummy)

	return err
}
