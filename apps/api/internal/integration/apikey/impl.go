package apikey

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofrs/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/trading-journal/api/internal/domain/api_key"
	"github.com/trading-journal/api/internal/store"
)

// ServiceImpl is the production implementation. Backed by store + bcrypt.
type ServiceImpl struct {
	store *store.Store
}

// NewServiceImpl builds the production impl.
func NewServiceImpl(st *store.Store) *ServiceImpl {
	return &ServiceImpl{store: st}
}

// Prefix is the literal "trj_pk_" + 8 random base64url chars (no padding).
const (
	KeyPrefix       = "trj_pk_"
	LookupChars     = 8
	SecretBytes     = 24 // → 32 base64url chars
	bcryptCost      = 10
)

// Generate mints a new key.
func (s *ServiceImpl) Generate(_ context.Context, in GenerateInput) (*GenerateResult, error) {
	if in.Name == "" {
		return nil, errors.New("apikey: name required")
	}
	if len(in.Scopes) == 0 {
		return nil, errors.New("apikey: at least one scope required")
	}
	scopeStrs := make([]string, 0, len(in.Scopes))
	for _, s := range in.Scopes {
		if !s.IsValid() {
			return nil, fmt.Errorf("apikey: invalid scope %q", string(s))
		}
		scopeStrs = append(scopeStrs, string(s))
	}

	prefix := KeyPrefix + randomString(LookupChars)
	secret := randomString(SecretBytes)
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("apikey: bcrypt: %w", err)
	}

	id, _ := uuid.NewV7()
	var expiresAt *time.Time
	if in.ExpiresAt != nil && *in.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *in.ExpiresAt)
		if err != nil {
			return nil, fmt.Errorf("apikey: invalid expires_at: %w", err)
		}
		expiresAt = &t
	}
	now := time.Now().UTC()
	k := &api_key.APIKey{
		ID:              id,
		TenantID:        in.TenantID,
		Name:            in.Name,
		Prefix:          prefix,
		SecretHash:      string(hash),
		Scopes:          scopeStrs,
		ExpiresAt:       expiresAt,
		IsActive:        true,
		CreatedByUserID: in.CreatedByUserID,
		CreatedAt:       now,
	}
	if err := s.store.APIKeys.Create(context.Background(), k); err != nil {
		return nil, fmt.Errorf("apikey: create: %w", err)
	}
	return &GenerateResult{
		Key:       k,
		Plaintext: prefix + "." + secret,
	}, nil
}

func (s *ServiceImpl) List(ctx context.Context, tenantID uuid.UUID) ([]*api_key.APIKey, error) {
	return s.store.APIKeys.ListByTenant(ctx, tenantID)
}

func (s *ServiceImpl) Get(ctx context.Context, id uuid.UUID) (*api_key.APIKey, error) {
	return s.store.APIKeys.GetByIDByTenant(ctx, id)
}

func (s *ServiceImpl) Revoke(ctx context.Context, id uuid.UUID) error {
	// ownership check first
	if _, err := s.store.APIKeys.GetByIDByTenant(ctx, id); err != nil {
		return err
	}
	return s.store.APIKeys.Revoke(context.Background(), id, time.Now())
}

func (s *ServiceImpl) Update(ctx context.Context, k *api_key.APIKey) error {
	if _, err := s.store.APIKeys.GetByIDByTenant(ctx, k.ID); err != nil {
		return err
	}
	return s.store.APIKeys.Update(context.Background(), k)
}

// Authenticate parses "trj_pk_<prefix>.<secret>" and verifies the bcrypt
// hash. Returns the APIKey on success or a typed error.
func (s *ServiceImpl) Authenticate(ctx context.Context, bearer string) (*api_key.APIKey, error) {
	// Strip "Bearer " prefix (HTTP middleware does this too, but be defensive).
	bearer = strings.TrimPrefix(strings.TrimSpace(bearer), "Bearer ")
	bearer = strings.TrimSpace(bearer)

	parts := strings.SplitN(bearer, ".", 2)
	if len(parts) != 2 {
		return nil, ErrInvalidBearer
	}
	prefix, secret := parts[0], parts[1]
	if !strings.HasPrefix(prefix, KeyPrefix) {
		return nil, ErrInvalidBearer
	}
	if secret == "" {
		return nil, ErrInvalidBearer
	}

	k, err := s.store.APIKeys.GetByPrefix(ctx, prefix)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrUnknownKey
		}
		return nil, err
	}
	now := time.Now()
	if !k.IsValidForRequest(now) {
		if k.RevokedAt != nil {
			return nil, ErrRevoked
		}
		if k.ExpiresAt != nil && !k.ExpiresAt.After(now) {
			return nil, ErrExpired
		}
		return nil, ErrInactive
	}
	if err := bcrypt.CompareHashAndPassword([]byte(k.SecretHash), []byte(secret)); err != nil {
		return nil, ErrBadSecret
	}

	// Async last_used_at bump. Fire-and-forget so the caller's response
	// isn't slowed by an extra UPDATE.
	go func(id uuid.UUID, at time.Time) {
		_ = s.store.APIKeys.MarkUsed(context.Background(), id, at)
	}(k.ID, now)

	return k, nil
}

// randomString returns n random base64url chars (no padding) suitable for
// URL-safe use as a key prefix or secret. We feed enough random bytes to
// produce at least n chars, then trim. base64 expands 3 bytes → 4 chars
// (no padding), so to get n chars we need ceil(n*3/4) input bytes.
func randomString(n int) string {
	if n <= 0 {
		return ""
	}
	inBytes := (n*3 + 3) / 4 // ceiling
	b := make([]byte, inBytes)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("apikey: crypto/rand failed: %v", err))
	}
	enc := base64.RawURLEncoding.EncodeToString(b)
	if len(enc) <= n {
		return enc
	}
	return enc[:n]
}