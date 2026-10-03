-- name: InsertAuditLog :exec
INSERT INTO audit_logs (occurred_at, actor_user_id, actor_tenant_id, action, resource_type, resource_id, ip, user_agent, request_id, metadata)
VALUES (COALESCE($1, NOW()), $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: ListAuditLogsByTenant :many
SELECT id, occurred_at, actor_user_id, actor_tenant_id, action, resource_type, resource_id, ip, user_agent, request_id, metadata
FROM audit_logs
WHERE actor_tenant_id = $1
ORDER BY occurred_at DESC
LIMIT $2 OFFSET $3;

-- name: CountAuditLogsByTenant :one
SELECT COUNT(*) FROM audit_logs WHERE actor_tenant_id = $1;