// Package apikey implements the public API key issuance + verification flow.
//
// Phase 5 ships:
//
//   - Generate: mint a new key for a tenant; returns plaintext ONCE
//   - List:     tenant-scoped list of keys (prefix only, never secret)
//   - Revoke:   soft-delete (is_active=false, revoked_at=now)
//   - Update:   rename, expand scopes
//   - Authenticate: given the bearer string, return the APIKey or an error
//
// The bcrypt cost factor is 10 (matches Phase 0's auth bcrypt usage).
package apikey

import (
	"context"
	"errors"

	"github.com/gofrs/uuid"
	"github.com/trading-journal/api/internal/domain/api_key"
)

// Service is the contract for the API key management + verification.
type Service interface {
	// Generate mints a new key for a tenant. Returns the plaintext once —
	// the caller (HTTP handler) is responsible for showing it to the user
	// and discarding. The plaintext is NEVER recoverable afterward.
	Generate(ctx context.Context, in GenerateInput) (*GenerateResult, error)

	// List returns all keys for the tenant. Secrets are NOT included.
	List(ctx context.Context, tenantID uuid.UUID) ([]*api_key.APIKey, error)

	// Get returns one api_key by id (tenant-scoped via ctx).
	Get(ctx context.Context, id uuid.UUID) (*api_key.APIKey, error)

	// Revoke soft-deletes a key. After Revoke returns, Authenticate refuses
	// the key with ErrRevoked.
	Revoke(ctx context.Context, id uuid.UUID) error

	// Update modifies mutable fields (name, scopes, expires_at).
	Update(ctx context.Context, k *api_key.APIKey) error

	// Authenticate parses a bearer string ("trj_pk_<prefix>.<secret>") and
	// returns the matching APIKey if it's valid for the current time.
	// Updates last_used_at asynchronously on hit.
	Authenticate(ctx context.Context, bearer string) (*api_key.APIKey, error)
}

// GenerateInput is the input to Generate.
type GenerateInput struct {
	TenantID        uuid.UUID
	CreatedByUserID uuid.UUID
	Name            string
	Scopes          []api_key.Scope
	ExpiresAt       *string // RFC3339; nil = never
}

// GenerateResult is what Generate returns. Plaintext is shown ONCE.
type GenerateResult struct {
	Key       *api_key.APIKey `json:"key"`
	Plaintext string          `json:"plaintext"` // trj_pk_<prefix>.<secret>
}

// ── Errors ─────────────────────────────────────────────────────────────

var (
	ErrInvalidBearer = errors.New("apikey: invalid bearer format")
	ErrUnknownKey    = errors.New("apikey: unknown key prefix")
	ErrBadSecret     = errors.New("apikey: bad secret")
	ErrRevoked       = errors.New("apikey: key revoked")
	ErrExpired       = errors.New("apikey: key expired")
	ErrInactive      = errors.New("apikey: key inactive")
)