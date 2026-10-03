package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

// PKCE helpers — Authorization Code + PKCE (RFC 7636).

// NewPKCE returns a fresh verifier (43-128 chars, base64url no pad)
// and its S256 challenge. We always use S256 (never "plain").
func NewPKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32) // 32 bytes → 43-char base64url string (RFC 7636 §4.1 min 43 chars)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

// NewStateToken returns a 32-byte random base64url string for CSRF protection
// on the OAuth state parameter.
func NewStateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}