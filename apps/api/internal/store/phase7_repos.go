package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/trading-journal/api/internal/domain/webhook"
)

// ── WebhookSubscriptionRepo ───────────────────────────────────────────

// WebhookSubscriptionRepo manages webhook_subscriptions rows.
type WebhookSubscriptionRepo struct {
	pool DBTX
}

func (r *WebhookSubscriptionRepo) WithTx(tx DBTX) *WebhookSubscriptionRepo {
	return &WebhookSubscriptionRepo{pool: tx}
}

// Create inserts a new subscription row.
func (r *WebhookSubscriptionRepo) Create(ctx context.Context, s *webhook.Subscription) error {
	eventTypes := make([]string, len(s.EventTypes))
	for i, e := range s.EventTypes {
		eventTypes[i] = string(e)
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO webhook_subscriptions
			(id, url, event_types, secret, description, is_active, tenant_id, created_by_user_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		s.ID, s.URL, eventTypes, s.Secret, s.Description, s.IsActive,
		s.TenantID, s.CreatedByUserID, s.CreatedAt, s.UpdatedAt,
	)
	return err
}

// GetByIDByTenant fetches one row scoped by tenant.
func (r *WebhookSubscriptionRepo) GetByIDByTenant(ctx context.Context, tenantID, id uuid.UUID) (*webhook.Subscription, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, url, event_types, secret, description, is_active, tenant_id,
		       created_by_user_id, created_at, updated_at, revoked_at
		FROM webhook_subscriptions
		WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	return scanWebhookSubscription(row)
}

// ListByTenant returns all subscriptions for the tenant (active + revoked).
func (r *WebhookSubscriptionRepo) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*webhook.Subscription, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, url, event_types, secret, description, is_active, tenant_id,
		       created_by_user_id, created_at, updated_at, revoked_at
		FROM webhook_subscriptions
		WHERE tenant_id = $1
		ORDER BY created_at ASC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*webhook.Subscription
	for rows.Next() {
		s, err := scanWebhookSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListActiveByEvent returns all active subscriptions for the tenant
// whose EventTypes contains the given event. Used by the Emit path.
func (r *WebhookSubscriptionRepo) ListActiveByEvent(ctx context.Context, tenantID uuid.UUID, event webhook.EventType) ([]*webhook.Subscription, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, url, event_types, secret, description, is_active, tenant_id,
		       created_by_user_id, created_at, updated_at, revoked_at
		FROM webhook_subscriptions
		WHERE tenant_id = $1 AND is_active = TRUE AND revoked_at IS NULL
		  AND $2 = ANY(event_types)`, tenantID, string(event))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*webhook.Subscription
	for rows.Next() {
		s, err := scanWebhookSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Update fields in-place. Only mutable fields (URL, EventTypes, IsActive).
func (r *WebhookSubscriptionRepo) Update(ctx context.Context, s *webhook.Subscription) error {
	eventTypes := make([]string, len(s.EventTypes))
	for i, e := range s.EventTypes {
		eventTypes[i] = string(e)
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE webhook_subscriptions
		SET url = $1, event_types = $2, description = $4, updated_at = $5
		WHERE id = $3 AND tenant_id = $6`,
		s.URL, eventTypes, s.ID, s.Description, s.UpdatedAt, s.TenantID,
	)
	return err
}

// Revoke sets is_active=false and revoked_at=now().
func (r *WebhookSubscriptionRepo) Revoke(ctx context.Context, tenantID, id uuid.UUID, when time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE webhook_subscriptions
		SET is_active = FALSE, revoked_at = $1, updated_at = $1
		WHERE id = $2 AND tenant_id = $3`, when, id, tenantID)
	return err
}

// ── WebhookDeliveryRepo ───────────────────────────────────────────────

// WebhookDeliveryRepo manages webhook_deliveries rows.
type WebhookDeliveryRepo struct {
	pool DBTX
}

func (r *WebhookDeliveryRepo) WithTx(tx DBTX) *WebhookDeliveryRepo {
	return &WebhookDeliveryRepo{pool: tx}
}

// Create inserts a new pending delivery row.
func (r *WebhookDeliveryRepo) Create(ctx context.Context, d *webhook.Delivery) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO webhook_deliveries
			(id, tenant_id, subscription_id, event_type, event_id, payload,
			 attempt, max_attempts, status, next_attempt_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		d.ID, d.TenantID, d.SubscriptionID, string(d.EventType), d.EventID,
		d.Payload, d.Attempt, d.MaxAttempts, d.Status, d.NextAttemptAt, d.CreatedAt,
	)
	return err
}

// GetByID returns one delivery (no tenant scoping — workers may look up
// by id during delivery).
func (r *WebhookDeliveryRepo) GetByID(ctx context.Context, id uuid.UUID) (*webhook.Delivery, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, subscription_id, event_type, event_id, payload,
		       attempt, max_attempts, status, response_code, response_body,
		       next_attempt_at, last_attempted_at, completed_at, created_at
		FROM webhook_deliveries WHERE id = $1`, id)
	return scanWebhookDelivery(row)
}

// PickDue returns up to `limit` deliveries that are pending and due.
// Used by the WebhookWorker poll loop.
func (r *WebhookDeliveryRepo) PickDue(ctx context.Context, limit int) ([]*webhook.Delivery, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, subscription_id, event_type, event_id, payload,
		       attempt, max_attempts, status, response_code, response_body,
		       next_attempt_at, last_attempted_at, completed_at, created_at
		FROM webhook_deliveries
		WHERE status = 'pending' AND next_attempt_at <= now()
		ORDER BY next_attempt_at ASC
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*webhook.Delivery
	for rows.Next() {
		d, err := scanWebhookDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// MarkSuccess finalizes a successful attempt.
func (r *WebhookDeliveryRepo) MarkSuccess(ctx context.Context, id uuid.UUID, code int, body string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE webhook_deliveries
		SET status = 'success', response_code = $1, response_body = $2,
		    last_attempted_at = now(), completed_at = now()
		WHERE id = $3`, code, body, id)
	return err
}

// MarkRetryableFailed advances attempt count + schedules the next retry.
func (r *WebhookDeliveryRepo) MarkRetryableFailed(ctx context.Context, id uuid.UUID, attempt int, nextAttemptAt time.Time, code *int, body *string, errorMsg string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE webhook_deliveries
		SET status = 'pending', attempt = $1, next_attempt_at = $2,
		    response_code = $3, response_body = $4, error = $5, last_attempted_at = now()
		WHERE id = $6`, attempt, nextAttemptAt, code, body, errorMsg, id)
	return err
}

// MarkExhausted marks the row as exhausted (no more retries).
func (r *WebhookDeliveryRepo) MarkExhausted(ctx context.Context, id uuid.UUID, code *int, body *string, errorMsg string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE webhook_deliveries
		SET status = 'exhausted', response_code = $1, response_body = $2,
		    error = $3, last_attempted_at = now(), completed_at = now()
		WHERE id = $4`, code, body, errorMsg, id)
	return err
}

// MarkNonRetryableFailed marks the row as a permanent client error.
func (r *WebhookDeliveryRepo) MarkNonRetryableFailed(ctx context.Context, id uuid.UUID, code int, body string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE webhook_deliveries
		SET status = 'failed', response_code = $1, response_body = $2,
		    last_attempted_at = now(), completed_at = now()
		WHERE id = $3`, code, body, id)
	return err
}

// ListBySubscription returns recent deliveries for a subscription (drill-down).
func (r *WebhookDeliveryRepo) ListBySubscription(ctx context.Context, subscriptionID uuid.UUID, limit int) ([]*webhook.Delivery, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, subscription_id, event_type, event_id, payload,
		       attempt, max_attempts, status, response_code, response_body,
		       next_attempt_at, last_attempted_at, completed_at, created_at
		FROM webhook_deliveries
		WHERE subscription_id = $1
		ORDER BY created_at DESC
		LIMIT $2`, subscriptionID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*webhook.Delivery
	for rows.Next() {
		d, err := scanWebhookDelivery(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ── row scanners ──────────────────────────────────────────────────────

type scanner interface {
	Scan(dest ...any) error
}

func scanWebhookSubscription(s scanner) (*webhook.Subscription, error) {
	var sub webhook.Subscription
	var eventTypes []string
	var description string
	var revokedAt *time.Time
	if err := s.Scan(
		&sub.ID, &sub.URL, &eventTypes, &sub.Secret, &description, &sub.IsActive,
		&sub.TenantID, &sub.CreatedByUserID, &sub.CreatedAt, &sub.UpdatedAt, &revokedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	sub.Description = description
	sub.EventTypes = make([]webhook.EventType, len(eventTypes))
	for i, e := range eventTypes {
		sub.EventTypes[i] = webhook.EventType(e)
	}
	sub.RevokedAt = revokedAt
	return &sub, nil
}

func scanWebhookDelivery(s scanner) (*webhook.Delivery, error) {
	var d webhook.Delivery
	var eventType string
	var payloadRaw []byte
	var code *int
	var body *string
	var lastAttempt *time.Time
	var completedAt *time.Time
	if err := s.Scan(
		&d.ID, &d.TenantID, &d.SubscriptionID, &eventType, &d.EventID, &payloadRaw,
		&d.Attempt, &d.MaxAttempts, &d.Status, &code, &body,
		&d.NextAttemptAt, &lastAttempt, &completedAt, &d.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	d.EventType = webhook.EventType(eventType)
	d.Payload = payloadRaw
	d.ResponseCode = code
	d.ResponseBody = body
	d.LastAttemptedAt = lastAttempt
	d.CompletedAt = completedAt
	// Best-effort parse for the FE; ignore errors (payload is JSON but
	// the repo treats it as []byte).
	_ = json.Unmarshal(payloadRaw, new(any))
	return &d, nil
}

