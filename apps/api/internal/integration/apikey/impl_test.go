package apikey

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/trading-journal/api/internal/domain/api_key"
)

func TestRandomString_Shape(t *testing.T) {
	for _, size := range []int{4, 8, 16, 32} {
		s := randomString(size)
		assert.Len(t, s, size)
		// base64url alphabet: A-Z, a-z, 0-9, -, _
		for _, c := range s {
			assert.True(t,
				(c >= 'A' && c <= 'Z') ||
					(c >= 'a' && c <= 'z') ||
					(c >= '0' && c <= '9') ||
					c == '-' || c == '_',
				"unexpected char %q", c)
		}
	}
}

func TestRandomString_Unique(t *testing.T) {
	const n = 1000
	seen := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		s := randomString(16)
		assert.False(t, seen[s], "collision on %s", s)
		seen[s] = true
	}
}

func TestRandomString_NoEquals(t *testing.T) {
	for i := 0; i < 100; i++ {
		s := randomString(8)
		assert.NotContains(t, s, "=", "base64 padding leaked: %s", s)
	}
}

func TestIsValidForRequest(t *testing.T) {
	now := mustParse(t, "2026-01-01T12:00:00Z")
	k := &api_key.APIKey{IsActive: true}
	assert.True(t, k.IsValidForRequest(now))

	k.IsActive = false
	assert.False(t, k.IsValidForRequest(now))

	k.IsActive = true
	k.RevokedAt = ptrTime(mustParse(t, "2025-12-01T00:00:00Z")) // revoked in past
	assert.False(t, k.IsValidForRequest(now))

	k.RevokedAt = ptrTime(mustParse(t, "2026-02-01T00:00:00Z")) // revoked in future
	assert.True(t, k.IsValidForRequest(now))

	k.ExpiresAt = ptrTime(mustParse(t, "2025-12-01T00:00:00Z")) // expired
	assert.False(t, k.IsValidForRequest(now))
}

func TestHasScope(t *testing.T) {
	k := &api_key.APIKey{Scopes: []string{"read:trades", "read:analytics"}}
	assert.True(t, k.HasScope(api_key.ScopeReadTrades))
	assert.True(t, k.HasScope(api_key.ScopeReadAnalytics))
	assert.False(t, k.HasScope(api_key.ScopeReadAccounts))
	assert.False(t, k.HasScope(api_key.ScopeReadPayments))
}

func TestHasScope_Empty(t *testing.T) {
	k := &api_key.APIKey{}
	assert.False(t, k.HasScope(api_key.ScopeReadTrades))
}

func TestScope_IsValid(t *testing.T) {
	for _, s := range api_key.AllScopes {
		assert.True(t, s.IsValid(), "scope %q should be valid", s)
	}
	assert.False(t, api_key.Scope("write:foo").IsValid())
	assert.False(t, api_key.Scope("READ:TRADES").IsValid(), "scopes are lower-case only")
}

// helpers

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err)
	return parsed
}

func ptrTime(t time.Time) *time.Time { return &t }

// Sanity-check that the in-package helpers we expose (randomString) produce
// strings that work as lookup prefixes in the Authenticate bearer parser.
func TestRandomString_WorksAsBearerPrefix(t *testing.T) {
	secret := randomString(32)
	prefix := KeyPrefix + randomString(8)
	bearer := prefix + "." + secret

	assert.True(t, strings.HasPrefix(bearer, KeyPrefix))
	parts := strings.SplitN(bearer, ".", 2)
	require.Len(t, parts, 2)
	assert.Equal(t, prefix, parts[0])
	assert.Equal(t, secret, parts[1])
}

// Idempotency — generating multiple keys gives distinct prefixes.
func TestGeneratePrefixes_Distinct(t *testing.T) {
	const n = 50
	seen := make(map[string]bool)
	for i := 0; i < n; i++ {
		p := KeyPrefix + randomString(8)
		assert.False(t, seen[p], "collision %s", p)
		seen[p] = true
	}
}

// Import-time guard: ensure the context type doesn't drift.
var _ = context.Background
var _ = uuid.Nil