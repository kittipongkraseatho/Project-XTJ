-- name: CreateTenant :one
INSERT INTO tenants (id, name, slug, status, metadata)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, name, slug, status, metadata, created_at, updated_at, deleted_at;

-- name: GetTenantByID :one
SELECT id, name, slug, status, metadata, created_at, updated_at, deleted_at
FROM tenants
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetTenantBySlug :one
SELECT id, name, slug, status, metadata, created_at, updated_at, deleted_at
FROM tenants
WHERE slug = $1 AND deleted_at IS NULL;

-- name: UpdateTenantStatus :exec
UPDATE tenants SET status = $2, updated_at = NOW() WHERE id = $1;

-- name: SoftDeleteTenant :exec
UPDATE tenants SET deleted_at = NOW() WHERE id = $1;