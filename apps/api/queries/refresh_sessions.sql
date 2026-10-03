-- name: CreateRefreshSession :one
INSERT INTO refresh_sessions (id, user_id, tenant_id, token_hash, status, user_agent, ip, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, user_id, tenant_id, token_hash, status, user_agent, ip, created_at, last_used_at, expires_at, revoked_at, replaced_by_session_id;

-- name: GetRefreshSessionByHash :one
SELECT id, user_id, tenant_id, token_hash, status, user_agent, ip, created_at, last_used_at, expires_at, revoked_at, replaced_by_session_id
FROM refresh_sessions
WHERE token_hash = $1;

-- name: ListActiveRefreshSessionsByUser :many
SELECT id, user_id, tenant_id, token_hash, status, user_agent, ip, created_at, last_used_at, expires_at, revoked_at, replaced_by_session_id
FROM refresh_sessions
WHERE user_id = $1 AND status = 'active' AND expires_at > NOW();

-- name: RevokeRefreshSession :exec
UPDATE refresh_sessions SET status = 'revoked', revoked_at = NOW() WHERE id = $1;

-- name: RevokeAllActiveRefreshSessionsForUser :exec
UPDATE refresh_sessions SET status = 'revoked', revoked_at = NOW()
WHERE user_id = $1 AND status = 'active';

-- name: RotateRefreshSession :exec
-- Mark old as revoked and link to new session id
UPDATE refresh_sessions
SET status = 'revoked', revoked_at = NOW(), replaced_by_session_id = $2
WHERE id = $1;

-- name: TouchRefreshSession :exec
UPDATE refresh_sessions SET last_used_at = NOW() WHERE id = $1;

-- name: DeleteExpiredRefreshSessions :exec
DELETE FROM refresh_sessions WHERE expires_at < NOW() - INTERVAL '7 days';