-- name: CreateUser :one
INSERT INTO users (id, email, password_hash, display_name, avatar_cloudinary_public_id, status, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, email, password_hash, display_name, avatar_cloudinary_public_id, status, metadata, created_at, updated_at, deleted_at;

-- name: GetUserByID :one
SELECT id, email, password_hash, display_name, avatar_cloudinary_public_id, status, metadata, created_at, updated_at, deleted_at
FROM users
WHERE id = $1 AND deleted_at IS NULL;

-- name: GetUserByEmail :one
SELECT id, email, password_hash, display_name, avatar_cloudinary_public_id, status, metadata, created_at, updated_at, deleted_at
FROM users
WHERE email = $1 AND deleted_at IS NULL;

-- name: UpdateUserDisplayName :exec
UPDATE users SET display_name = $2, updated_at = NOW() WHERE id = $1;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = $2, updated_at = NOW() WHERE id = $1;

-- name: UpdateUserAvatar :exec
UPDATE users SET avatar_cloudinary_public_id = $2, updated_at = NOW() WHERE id = $1;

-- name: SoftDeleteUser :exec
UPDATE users SET deleted_at = NOW() WHERE id = $1;