package api_key

import (
	"time"

	"github.com/gofrs/uuid"
)

// Scope is the per-key permission grant. Phase 5 is read-only.
type Scope string

const (
	ScopeReadAccounts  Scope = "read:accounts"
	ScopeReadTrades    Scope = "read:trades"
	ScopeReadAnalytics Scope = "read:analytics"
	ScopeReadPayments  Scope = "read:payments"
)

// AllScopes lists every valid Scope (used by the FE selector).
var AllScopes = []Scope{
	ScopeReadAccounts,
	ScopeReadTrades,
	ScopeReadAnalytics,
	ScopeReadPayments,
}

// IsValid reports whether s is one of AllScopes.
func (s Scope) IsValid() bool {
	for _, v := range AllScopes {
		if v == s {
			return true
		}
	}
	return false
}

// APIKey is one row of api_keys.
//
// SecretHash is the bcrypt hash of the plaintext secret; the plaintext
// itself is never persisted (it's shown once at creation and discarded).
type APIKey struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	Name             string
	Prefix           string // e.g. "trj_pk_abcd1234" — the lookup key
	SecretHash       string // bcrypt(secret, cost=10)
	Scopes           []string
	LastUsedAt       *time.Time
	ExpiresAt        *time.Time
	IsActive         bool
	CreatedByUserID  uuid.UUID
	CreatedAt        time.Time
	RevokedAt        *time.Time
}

// IsValidForRequest reports whether the key can serve the current request:
// active, not revoked, not expired.
func (k *APIKey) IsValidForRequest(now time.Time) bool {
	if !k.IsActive {
		return false
	}
	if k.RevokedAt != nil && !k.RevokedAt.After(now) {
		return false
	}
	if k.ExpiresAt != nil && !k.ExpiresAt.After(now) {
		return false
	}
	return true
}

// HasScope reports whether k's scopes include the requested one.
func (k *APIKey) HasScope(s Scope) bool {
	for _, sc := range k.Scopes {
		if Scope(sc) == s {
			return true
		}
	}
	return false
}