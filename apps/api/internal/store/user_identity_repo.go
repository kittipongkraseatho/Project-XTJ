package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"
)

type IdentityProvider string

const (
	ProviderGoogle IdentityProvider = "google"
	ProviderLine   IdentityProvider = "line"
	ProviderGitHub IdentityProvider = "github"
)

// UserIdentity is the per-provider linked identity record.
type UserIdentity struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	TenantID      uuid.UUID
	Provider      IdentityProvider
	ProviderSub   string
	Email         *string
	EmailVerified bool
	DisplayName   *string
	AvatarURL     *string
	RawProfile    map[string]any
	CreatedAt     time.Time
	UpdatedAt     time.Time
	LastUsedAt    time.Time
}

type UserIdentityRepo struct{ pool DBTX }

func (r *UserIdentityRepo) WithTx(tx DBTX) *UserIdentityRepo { return &UserIdentityRepo{pool: tx} }

func (r *UserIdentityRepo) Create(ctx context.Context, id *UserIdentity) error {
	raw, _ := json.Marshal(id.RawProfile)
	if raw == nil {
		raw = []byte("{}")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO user_identities (id, user_id, tenant_id, provider, provider_sub, email, email_verified, display_name, avatar_url, raw_profile)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING created_at, updated_at, last_used_at`,
		id.ID, id.UserID, id.TenantID, id.Provider, id.ProviderSub,
		id.Email, id.EmailVerified, id.DisplayName, id.AvatarURL, raw)
	return row.Scan(&id.CreatedAt, &id.UpdatedAt, &id.LastUsedAt)
}

func (r *UserIdentityRepo) GetByProviderSub(ctx context.Context, provider IdentityProvider, sub string) (*UserIdentity, error) {
	row := r.pool.QueryRow(ctx, identitySelect+` WHERE provider = $1 AND provider_sub = $2`, provider, sub)
	return scanIdentity(row)
}

func (r *UserIdentityRepo) Touch(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE user_identities SET last_used_at = NOW(), updated_at = NOW() WHERE id = $1`, id)
	return err
}

const identitySelect = `
SELECT id, user_id, tenant_id, provider, provider_sub, email, email_verified,
       display_name, avatar_url, raw_profile, created_at, updated_at, last_used_at
FROM user_identities`

func scanIdentity(row pgx.Row) (*UserIdentity, error) {
	var id UserIdentity
	var raw []byte
	if err := row.Scan(
		&id.ID, &id.UserID, &id.TenantID, &id.Provider, &id.ProviderSub,
		&id.Email, &id.EmailVerified, &id.DisplayName, &id.AvatarURL, &raw,
		&id.CreatedAt, &id.UpdatedAt, &id.LastUsedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &id.RawProfile)
	}
	if id.RawProfile == nil {
		id.RawProfile = map[string]any{}
	}
	return &id, nil
}