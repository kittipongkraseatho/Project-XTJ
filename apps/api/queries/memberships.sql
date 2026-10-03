-- name: CreateMembership :one
INSERT INTO memberships (id, tenant_id, user_id, role, status)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, tenant_id, user_id, role, status, created_at, updated_at;

-- name: GetMembershipByUserAndTenant :one
SELECT id, tenant_id, user_id, role, status, created_at, updated_at
FROM memberships
WHERE user_id = $1 AND tenant_id = $2;

-- name: GetMembershipsByUser :many
SELECT id, tenant_id, user_id, role, status, created_at, updated_at
FROM memberships
WHERE user_id = $1;

-- name: GetMembershipsByTenant :many
SELECT id, tenant_id, user_id, role, status, created_at, updated_at
FROM memberships
WHERE tenant_id = $1;

-- name: UpdateMembershipRole :exec
UPDATE memberships SET role = $3, updated_at = NOW() WHERE tenant_id = $1 AND user_id = $2;