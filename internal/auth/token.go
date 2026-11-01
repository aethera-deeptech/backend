package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// newToken returns a random token for a cookie or a link, and the SHA-256 hash that the database stores. The token has 256 bits,
// so a fast hash is enough, a password hash would add cost and no safety.
func newToken() (raw string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, fmt.Errorf("make a token %w", err)
	}

	raw = base64.RawURLEncoding.EncodeToString(b)

	return raw, hashToken(raw), nil
}

func hashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))

	return sum[:]
}
