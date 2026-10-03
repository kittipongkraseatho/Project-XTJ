package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/trading-journal/api/internal/domain/subscription"
)

type SubscriptionRepo struct{ pool DBTX }

func (r *SubscriptionRepo) WithTx(tx DBTX) *SubscriptionRepo { return &SubscriptionRepo{pool: tx} }

func (r *SubscriptionRepo) Create(ctx context.Context, s *subscription.Subscription) error {
	meta, _ := json.Marshal(s.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO subscriptions (id, tenant_id, plan_id, status, started_at, current_period_start, current_period_end, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at, updated_at`,
		s.ID, s.TenantID, s.PlanID, s.Status, s.StartedAt, s.CurrentPeriodStart, s.CurrentPeriodEnd, meta)
	return row.Scan(&s.CreatedAt, &s.UpdatedAt)
}

func (r *SubscriptionRepo) GetActiveByTenant(ctx context.Context, tenantID uuid.UUID) (*subscription.Subscription, error) {
	row := r.pool.QueryRow(ctx, subscriptionSelect+`
		WHERE tenant_id = $1 AND status IN ('active','trialing')
		LIMIT 1`, tenantID)
	return scanSubscription(row)
}

const subscriptionSelect = `
SELECT id, tenant_id, plan_id, status, started_at, current_period_start, current_period_end,
       cancel_at, canceled_at, external_provider, external_subscription_id, metadata,
       created_at, updated_at
FROM subscriptions`

func scanSubscription(row pgx.Row) (*subscription.Subscription, error) {
	var s subscription.Subscription
	if err := row.Scan(
		&s.ID, &s.TenantID, &s.PlanID, &s.Status, &s.StartedAt, &s.CurrentPeriodStart,
		&s.CurrentPeriodEnd, &s.CancelAt, &s.CanceledAt, &s.ExternalProvider,
		&s.ExternalSubscriptionID, &s.Metadata, &s.CreatedAt, &s.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if s.Metadata == nil {
		s.Metadata = map[string]any{}
	}
	return &s, nil
}