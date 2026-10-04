package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/trading-journal/api/internal/domain/payment"
	"github.com/trading-journal/api/internal/domain/subscription"
)

// ── Phase 7 — Stripe webhook state-machine repo methods ─────────────

// WebhookEventRepo tracks Stripe event ids we've applied (idempotency log).
type WebhookEventRepo struct{ pool DBTX }

func (r *WebhookEventRepo) WithTx(tx DBTX) *WebhookEventRepo {
	return &WebhookEventRepo{pool: tx}
}

// Insert appends a new event row. Returns true if the row was inserted,
// false if it already existed (UNIQUE on stripe_event_id).
func (r *WebhookEventRepo) Insert(ctx context.Context, stripeEventID, eventType string) (bool, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO webhook_events (id, stripe_event_id, event_type)
		VALUES ($1, $2, $3)
		ON CONFLICT (stripe_event_id) DO NOTHING
		RETURNING id`,
		uuid.Must(uuid.NewV7()), stripeEventID, eventType,
	)
	var id uuid.UUID
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil // already processed
		}
		return false, err
	}
	return true, nil
}

// ── Subscription upsert by external id ───────────────────────────────

// UpsertByExternalID creates or updates a subscription row keyed on
// `external_subscription_id`. Used by the Stripe webhook handler to
// apply `checkout.session.completed` and `customer.subscription.updated`.
//
// If a row with this external id exists, we update the mutable fields.
// Otherwise we insert a new row with the given id.
func (r *SubscriptionRepo) UpsertByExternalID(ctx context.Context, s *subscription.Subscription) error {
	meta, _ := marshalMeta(s.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	// ON CONFLICT (external_subscription_id) DO UPDATE keeps the local id stable
	// across regenerations.
	_, err := r.pool.Exec(ctx, `
		INSERT INTO subscriptions
			(id, tenant_id, plan_id, status, started_at,
			 current_period_start, current_period_end,
			 cancel_at, canceled_at,
			 external_provider, external_subscription_id, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (external_subscription_id) DO UPDATE
		SET plan_id = EXCLUDED.plan_id,
		    status = EXCLUDED.status,
		    current_period_start = EXCLUDED.current_period_start,
		    current_period_end = EXCLUDED.current_period_end,
		    cancel_at = EXCLUDED.cancel_at,
		    canceled_at = EXCLUDED.canceled_at,
		    metadata = EXCLUDED.metadata,
		    updated_at = now()`,
		s.ID, s.TenantID, s.PlanID, s.Status, s.StartedAt,
		s.CurrentPeriodStart, s.CurrentPeriodEnd,
		s.CancelAt, s.CanceledAt,
		s.ExternalProvider, s.ExternalSubscriptionID, meta,
	)
	return err
}

// GetByExternalID looks up a subscription by its Stripe id.
// Returns ErrNotFound if no row matches.
func (r *SubscriptionRepo) GetByExternalID(ctx context.Context, externalID string) (*subscription.Subscription, error) {
	row := r.pool.QueryRow(ctx, subscriptionSelect+`
		WHERE external_subscription_id = $1
		LIMIT 1`, externalID)
	return scanSubscription(row)
}

// MarkStripeIDs updates external_provider + external_subscription_id + plan_id
// for an existing row (used when the local row was created without Stripe
// identifiers — e.g. a manual plan upgrade).
func (r *SubscriptionRepo) MarkStripeIDs(ctx context.Context, tenantID uuid.UUID, externalSubID, planCode string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE subscriptions
		SET external_provider = 'stripe',
		    external_subscription_id = $2,
		    plan_id = $3,
		    updated_at = now()
		WHERE tenant_id = $1 AND external_subscription_id IS NULL`,
		tenantID, externalSubID, planCode)
	return err
}

// ── Payment upsert by external invoice id ─────────────────────────────

// UpsertByExternalInvoice creates or updates a payment row keyed on
// (provider, external_invoice_id). Used by `invoice.paid` and
// `invoice.payment_failed`.
func (r *PaymentRepo) UpsertByExternalInvoice(ctx context.Context, p *payment.Payment) error {
	meta, _ := marshalMeta(p.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	var paidAt *time.Time
	if p.PaidAt != nil {
		t := p.PaidAt.UTC()
		paidAt = &t
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO payments
			(id, tenant_id, subscription_id, provider, external_invoice_id,
			 amount_cents, currency, status, paid_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (provider, external_invoice_id) DO UPDATE
		SET status = EXCLUDED.status,
		    paid_at = EXCLUDED.paid_at,
		    amount_cents = EXCLUDED.amount_cents,
		    metadata = EXCLUDED.metadata`,
		p.ID, p.TenantID, p.SubscriptionID, p.Provider, p.ExternalInvoiceID,
		p.AmountCents, p.Currency, p.Status, paidAt, meta,
	)
	return err
}

// ── Local helpers ───────────────────────────────────────────────────

// marshalMeta is a tiny alias to keep the import block tight in callers.
func marshalMeta(v any) ([]byte, error) {
	if v == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(v)
}
