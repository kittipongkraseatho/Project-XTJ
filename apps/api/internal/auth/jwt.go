package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gofrs/uuid"
)

// Claims is the JWT payload for access tokens (spec §7.2).
//
// sub  = user_id (uuid)
// tid  = tenant_id (uuid)
// sid  = refresh session id (uuid)
// rol  = membership role (owner|admin|member|viewer)
// iat  = issued at
// exp  = expires at
// iss  = issuer
// aud  = audience
type Claims struct {
	TenantID string `json:"tid"`
	SessionID string `json:"sid"`
	Role     string `json:"rol"`
	jwt.RegisteredClaims
}

// JWTKeyID is the kid claim value for Phase 0 (single key).
const JWTKeyID = "v1"

// JWTService signs and verifies EdDSA/Ed25519 access tokens.
type JWTService struct {
	priv     ed25519.PrivateKey
	pub      ed25519.PublicKey
	issuer   string
	audience string
	ttl      time.Duration
}

// NewJWTService loads (or generates) the EdDSA keypair from path.
//
// If the file does not exist, a fresh keypair is generated, persisted to
// path (dir 0700, file 0600), and loaded. Subsequent boots reuse the file.
func NewJWTService(path, issuer, audience string, ttl time.Duration) (*JWTService, error) {
	if path == "" {
		return nil, errors.New("jwt key path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("mkdir key dir: %w", err)
	}

	priv, err := loadOrGenerateEd25519(path)
	if err != nil {
		return nil, err
	}

	return &JWTService{
		priv:     priv,
		pub:      priv.Public().(ed25519.PublicKey),
		issuer:   issuer,
		audience: audience,
		ttl:      ttl,
	}, nil
}

func loadOrGenerateEd25519(path string) (ed25519.PrivateKey, error) {
	if data, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, errors.New("jwt key pem: invalid PEM block")
		}
		// PKCS#8 encoded
		key, perr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if perr != nil {
			return nil, fmt.Errorf("parse pkcs8: %w", perr)
		}
		priv, ok := key.(ed25519.PrivateKey)
		if !ok {
			return nil, errors.New("key is not ed25519")
		}
		return priv, nil
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519: %w", err)
	}
	_ = pub

	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("marshal pkcs8: %w", err)
	}
	pemBlock := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	if err := os.WriteFile(path, pem.EncodeToMemory(pemBlock), 0o600); err != nil {
		return nil, fmt.Errorf("write key: %w", err)
	}
	return priv, nil
}

// SignInput is the payload for a new access token.
type SignInput struct {
	UserID    uuid.UUID
	TenantID  uuid.UUID
	SessionID uuid.UUID
	Role      string
}

// Sign creates a new EdDSA JWT for the given input.
func (s *JWTService) Sign(in SignInput) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(s.ttl)
	c := Claims{
		TenantID:  in.TenantID.String(),
		SessionID: in.SessionID.String(),
		Role:      in.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   in.UserID.String(),
			Issuer:    s.issuer,
			Audience:  jwt.ClaimStrings{s.audience},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, c)
	tok.Header["kid"] = JWTKeyID
	signed, err := tok.SignedString(s.priv)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign jwt: %w", err)
	}
	return signed, exp, nil
}

// Verify parses and validates a token, returning the claims.
// Returns ErrInvalidToken on any verification failure.
var ErrInvalidToken = errors.New("invalid token")

func (s *JWTService) Verify(raw string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(raw, &Claims{}, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodEdDSA.Alg() {
			return nil, fmt.Errorf("unexpected alg: %v", t.Method.Alg())
		}
		if kid, ok := t.Header["kid"].(string); !ok || kid != JWTKeyID {
			return nil, fmt.Errorf("unexpected kid: %v", t.Header["kid"])
		}
		return s.pub, nil
	}, jwt.WithIssuer(s.issuer), jwt.WithAudience(s.audience), jwt.WithExpirationRequired())

	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}
	c, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, ErrInvalidToken
	}
	return c, nil
}