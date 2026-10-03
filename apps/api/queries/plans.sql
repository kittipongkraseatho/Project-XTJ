-- name: GetPlanByID :one
SELECT id, code, name, description, price_cents, currency, interval, is_active, metadata, created_at, updated_at
FROM plans
WHERE id = $1;

-- name: GetPlanByCode :one
SELECT id, code, name, description, price_cents, currency, interval, is_active, metadata, created_at, updated_at
FROM plans
WHERE code = $1;

-- name: ListActivePlans :many
SELECT id, code, name, description, price_cents, currency, interval, is_active, metadata, created_at, updated_at
FROM plans
WHERE is_active = TRUE
ORDER BY price_cents ASC;