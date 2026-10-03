package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// RefreshTokenLength = 32 random bytes → 43-char base64url string (no padding).
const RefreshTokenLength = 32

// NewRefreshToken returns a (token, hash) pair.
// `token` is the opaque value to send to the client (NOT to be stored).
// `hash` is what we persist in refresh_sessions.token_hash.
func NewRefreshToken() (token string, hash string, err error) {
	b := make([]byte, RefreshTokenLength)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	hash = HashRefreshToken(token)
	return token, hash, nil
}

// HashRefreshToken returns the lowercase hex sha256 of the token.
// We persist this hex string in DB; on lookup we hash the presented token
// the same way and compare.
func HashRefreshToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}