package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// rawTokenBytes is 32 bytes of crypto/rand → 43-char base64-url string.
// Used for both magic-link and session cookie tokens.
const rawTokenBytes = 32

// GenerateToken returns (raw, sha256(raw)). The raw form goes in the email
// link or cookie; only the hash is persisted, so a DB leak does not let an
// attacker mint sessions or replay magic links.
func GenerateToken() (raw string, hash []byte, err error) {
	b := make([]byte, rawTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(raw))
	return raw, h[:], nil
}

// HashToken is the verify-side counterpart: take the raw token off the wire
// and produce the same digest we stored at issue time.
func HashToken(raw string) []byte {
	h := sha256.Sum256([]byte(raw))
	return h[:]
}
