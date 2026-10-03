package store

import (
	"context"
	"errors"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/trading-journal/api/internal/domain/api_key"
	"github.com/trading-journal/api/internal/tenant"
)

// ── APIKeyRepo ───────────────────────────────────────────────────────

type APIKeyRepo struct{ pool DBTX }

func (r *APIKeyRepo) WithTx(tx DBTX) *APIKeyRepo { return &APIKeyRepo{pool: tx} }

func (r *APIKeyRepo) Create(ctx context.Context, k *api_key.APIKey) error {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO api_keys (
			id, tenant_id, name, prefix, secret_hash, scopes,
			last_used_at, expires_at, is_active, created_by_user_id,
			created_at, revoked_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING created_at`,
		k.ID, k.TenantID, k.Name, k.Prefix, k.SecretHash, k.Scopes,
		k.LastUsedAt, k.ExpiresAt, k.IsActive, k.CreatedByUserID,
		k.CreatedAt, k.RevokedAt)
	return row.Scan(&k.CreatedAt)
}

func (r *APIKeyRepo) GetByPrefix(ctx context.Context, prefix string) (*api_key.APIKey, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, prefix, secret_hash, scopes,
		       last_used_at, expires_at, is_active, created_by_user_id,
		       created_at, revoked_at
		FROM api_keys WHERE prefix = $1`, prefix)
	return scanAPIKey(row)
}

func (r *APIKeyRepo) GetByIDByTenant(ctx context.Context, id uuid.UUID) (*api_key.APIKey, error) {
	tenantID, err := tenant.TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, name, prefix, secret_hash, scopes,
		       last_used_at, expires_at, is_active, created_by_user_id,
		       created_at, revoked_at
		FROM api_keys WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	return scanAPIKey(row)
}

func (r *APIKeyRepo) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*api_key.APIKey, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, name, prefix, secret_hash, scopes,
		       last_used_at, expires_at, is_active, created_by_user_id,
		       created_at, revoked_at
		FROM api_keys WHERE tenant_id = $1 ORDER BY created_at DESC`,
		tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*api_key.APIKey
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// MarkUsed bumps last_used_at (best-effort; never returned as error to caller).
func (r *APIKeyRepo) MarkUsed(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE api_keys SET last_used_at = $2 WHERE id = $1`, id, at)
	return err
}

func (r *APIKeyRepo) Revoke(ctx context.Context, id uuid.UUID, at time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE api_keys SET is_active = FALSE, revoked_at = $2 WHERE id = $1`,
		id, at)
	return err
}

// Update modifies mutable fields (name, scopes, expires_at).
func (r *APIKeyRepo) Update(ctx context.Context, k *api_key.APIKey) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE api_keys
		SET name = $2, scopes = $3, expires_at = $4
		WHERE id = $1`, k.ID, k.Name, k.Scopes, k.ExpiresAt)
	return err
}

func scanAPIKey(row rowScanner) (*api_key.APIKey, error) {
	var k api_key.APIKey
	if err := row.Scan(
		&k.ID, &k.TenantID, &k.Name, &k.Prefix, &k.SecretHash, &k.Scopes,
		&k.LastUsedAt, &k.ExpiresAt, &k.IsActive, &k.CreatedByUserID,
		&k.CreatedAt, &k.RevokedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if k.Scopes == nil {
		k.Scopes = []string{}
	}
	return &k, nil
}