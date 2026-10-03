package webhook

import (
	"crypto/rand"
	"encoding/hex"
)

// randomSecret returns a hex-encoded random byte string of the given length.
// Used for the per-subscription HMAC secret.
func randomSecret(bytesN int) (string, error) {
	if bytesN <= 0 {
		bytesN = 32
	}
	b := make([]byte, bytesN)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "trj_whsec_" + hex.EncodeToString(b), nil
}