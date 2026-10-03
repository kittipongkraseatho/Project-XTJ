// Package auth provides password hashing (argon2id), JWT (EdDSA) signing,
// refresh-token generation/rotation, and the bearer-auth middleware.
//
// All functions are constant-time where relevant. All secrets returned to callers
// are opaque encoded strings — never raw bytes.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters per spec §7.1.
// time=1, memory=64MB, threads=2 — OWASP recommends time≥1, memory≥46MB,
// parallelism≥1; we add memory for future-proofing.
const (
	argonTime    uint32 = 1
	argonMemory  uint32 = 64 * 1024 // 64 MB
	argonThreads uint8  = 2
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

// ErrInvalidPassword is returned when a verification fails.
var ErrInvalidPassword = errors.New("password does not match")

// HashPassword returns an encoded argon2id string of the form:
//
//	$argon2id$v=19$m=65536,t=1,p=2$<salt-b64>$<hash-b64>
func HashPassword(plain string) (string, error) {
	if plain == "" {
		return "", errors.New("password is empty")
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("salt: %w", err)
	}
	hash := argon2.IDKey([]byte(plain), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	enc := fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	)
	return enc, nil
}

// VerifyPassword compares plain to the encoded hash using constant-time compare.
// Returns nil on match, ErrInvalidPassword otherwise (or parse error).
func VerifyPassword(plain, encoded string) error {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return fmt.Errorf("unsupported hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return fmt.Errorf("parse version: %w", err)
	}
	var memory uint32
	var time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return fmt.Errorf("parse params: %w", err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return fmt.Errorf("decode salt: %w", err)
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return fmt.Errorf("decode hash: %w", err)
	}

	got := argon2.IDKey([]byte(plain), salt, time, memory, threads, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrInvalidPassword
	}
	return nil
}

// IsStrongEnough enforces a minimum policy (8+ chars, at least one letter + one digit).
// Returns nil on pass, error otherwise with the missing rule.
func IsStrongEnough(p string) error {
	if len(p) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	hasLetter, hasDigit := false, false
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			hasLetter = true
		case r >= '0' && r <= '9':
			hasDigit = true
		}
	}
	if !hasLetter {
		return fmt.Errorf("password must contain at least one letter")
	}
	if !hasDigit {
		return fmt.Errorf("password must contain at least one digit")
	}
	return nil
}