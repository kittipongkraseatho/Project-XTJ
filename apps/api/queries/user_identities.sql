-- name: CreateUserIdentity :one
INSERT INTO user_identities (id, user_id, tenant_id, provider, provider_sub, email, email_verified, display_name, avatar_url, raw_profile)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, user_id, tenant_id, provider, provider_sub, email, email_verified, display_name, avatar_url, raw_profile, created_at, updated_at, last_used_at;

-- name: GetUserIdentityByProvider :one
SELECT id, user_id, tenant_id, provider, provider_sub, email, email_verified, display_name, avatar_url, raw_profile, created_at, updated_at, last_used_at
FROM user_identities
WHERE provider = $1 AND provider_sub = $2;

-- name: GetUserIdentitiesByUser :many
SELECT id, user_id, tenant_id, provider, provider_sub, email, email_verified, display_name, avatar_url, raw_profile, created_at, updated_at, last_used_at
FROM user_identities
WHERE user_id = $1;

-- name: TouchUserIdentity :exec
UPDATE user_identities SET last_used_at = NOW(), updated_at = NOW() WHERE id = $1;