package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/trading-journal/api/internal/domain/tenant"
)

type TenantRepo struct{ pool DBTX }

func (r *TenantRepo) WithTx(tx DBTX) *TenantRepo { return &TenantRepo{pool: tx} }

func (r *TenantRepo) Create(ctx context.Context, t *tenant.Tenant) error {
	meta, _ := json.Marshal(t.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO tenants (id, name, slug, status, metadata)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at, updated_at`,
		t.ID, t.Name, t.Slug, t.Status, meta)
	return row.Scan(&t.CreatedAt, &t.UpdatedAt)
}

func (r *TenantRepo) GetByID(ctx context.Context, id uuid.UUID) (*tenant.Tenant, error) {
	row := r.pool.QueryRow(ctx, tenantSelect+` WHERE id = $1 AND deleted_at IS NULL`, id)
	return scanTenant(row)
}

func (r *TenantRepo) GetBySlug(ctx context.Context, slug string) (*tenant.Tenant, error) {
	row := r.pool.QueryRow(ctx, tenantSelect+` WHERE slug = $1 AND deleted_at IS NULL`, slug)
	return scanTenant(row)
}

const tenantSelect = `
SELECT id, name, slug, status, metadata, created_at, updated_at, deleted_at
FROM tenants`

func scanTenant(row pgx.Row) (*tenant.Tenant, error) {
	var t tenant.Tenant
	var metaRaw []byte
	if err := row.Scan(&t.ID, &t.Name, &t.Slug, &t.Status, &metaRaw, &t.CreatedAt, &t.UpdatedAt, &t.DeletedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(metaRaw) > 0 {
		_ = json.Unmarshal(metaRaw, &t.Metadata)
	}
	if t.Metadata == nil {
		t.Metadata = map[string]any{}
	}
	return &t, nil
}