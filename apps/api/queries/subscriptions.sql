-- name: CreateSubscription :one
INSERT INTO subscriptions (id, tenant_id, plan_id, status, started_at, current_period_start, current_period_end, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING id, tenant_id, plan_id, status, started_at, current_period_start, current_period_end, cancel_at, canceled_at, external_provider, external_subscription_id, metadata, created_at, updated_at;

-- name: GetActiveSubscriptionByTenant :one
SELECT id, tenant_id, plan_id, status, started_at, current_period_start, current_period_end, cancel_at, canceled_at, external_provider, external_subscription_id, metadata, created_at, updated_at
FROM subscriptions
WHERE tenant_id = $1 AND status IN ('active', 'trialing')
LIMIT 1;

-- name: GetSubscriptionByID :one
SELECT id, tenant_id, plan_id, status, started_at, current_period_start, current_period_end, cancel_at, canceled_at, external_provider, external_subscription_id, metadata, created_at, updated_at
FROM subscriptions
WHERE id = $1;

-- name: UpdateSubscriptionStatus :exec
UPDATE subscriptions SET status = $2, updated_at = NOW() WHERE id = $1;