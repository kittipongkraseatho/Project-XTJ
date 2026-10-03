package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/trading-journal/api/internal/domain/payment"
	"github.com/trading-journal/api/internal/domain/report_schedule"
	"github.com/trading-journal/api/internal/tenant"
)

// ── PaymentRepo ─────────────────────────────────────────────────────

type PaymentRepo struct{ pool DBTX }

func (r *PaymentRepo) WithTx(tx DBTX) *PaymentRepo { return &PaymentRepo{pool: tx} }

func (r *PaymentRepo) Create(ctx context.Context, p *payment.Payment) error {
	meta, _ := json.Marshal(p.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO payments (
			id, tenant_id, subscription_id, provider, external_invoice_id,
			amount_cents, currency, status, paid_at, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (provider, external_invoice_id) DO NOTHING
		RETURNING created_at`,
		p.ID, p.TenantID, p.SubscriptionID, p.Provider, p.ExternalInvoiceID,
		p.AmountCents, p.Currency, p.Status, p.PaidAt, meta)
	err := row.Scan(&p.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// ON CONFLICT — already inserted (webhook retry). Load existing.
		var existing payment.Payment
		err = r.pool.QueryRow(ctx, `
			SELECT id, tenant_id, subscription_id, provider, external_invoice_id,
			       amount_cents, currency, status, paid_at, metadata, created_at
			FROM payments WHERE provider=$1 AND external_invoice_id=$2`,
			p.Provider, p.ExternalInvoiceID).Scan(
			&existing.ID, &existing.TenantID, &existing.SubscriptionID, &existing.Provider,
			&existing.ExternalInvoiceID, &existing.AmountCents, &existing.Currency,
			&existing.Status, &existing.PaidAt, &meta, &existing.CreatedAt)
		if err != nil {
			return err
		}
		_ = json.Unmarshal(meta, &existing.Metadata)
		*p = existing
	}
	return err
}

func (r *PaymentRepo) ListByTenant(ctx context.Context, tenantID uuid.UUID, limit int) ([]*payment.Payment, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, subscription_id, provider, external_invoice_id,
		       amount_cents, currency, status, paid_at, metadata, created_at
		FROM payments WHERE tenant_id=$1 ORDER BY created_at DESC LIMIT $2`,
		tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var payments []*payment.Payment
	for rows.Next() {
		var p payment.Payment
		var metaRaw []byte
		if err := rows.Scan(&p.ID, &p.TenantID, &p.SubscriptionID, &p.Provider,
			&p.ExternalInvoiceID, &p.AmountCents, &p.Currency, &p.Status, &p.PaidAt,
			&metaRaw, &p.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(metaRaw, &p.Metadata)
		if p.Metadata == nil {
			p.Metadata = map[string]any{}
		}
		payments = append(payments, &p)
	}
	return payments, rows.Err()
}

// ── ReportScheduleRepo ──────────────────────────────────────────────

type ReportScheduleRepo struct{ pool DBTX }

func (r *ReportScheduleRepo) WithTx(tx DBTX) *ReportScheduleRepo {
	return &ReportScheduleRepo{pool: tx}
}

func (r *ReportScheduleRepo) Create(ctx context.Context, s *report_schedule.ReportSchedule) error {
	meta, _ := json.Marshal(s.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO report_schedules (
			id, tenant_id, trading_account_id, frequency, hour_utc,
			weekday, day_of_month, last_sent_at, next_send_at,
			recipients, is_active, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING created_at, updated_at`,
		s.ID, s.TenantID, s.TradingAccountID, s.Frequency, s.HourUTC,
		s.Weekday, s.DayOfMonth, s.LastSentAt, s.NextSendAt,
		s.Recipients, s.IsActive, meta)
	return row.Scan(&s.CreatedAt, &s.UpdatedAt)
}

func (r *ReportScheduleRepo) UpdateAfterSend(ctx context.Context, id uuid.UUID, sentAt, nextSendAt time.Time) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE report_schedules
		SET last_sent_at = $2, next_send_at = $3, updated_at = now()
		WHERE id = $1`, id, sentAt, nextSendAt)
	return err
}

func (r *ReportScheduleRepo) SetActive(ctx context.Context, id uuid.UUID, active bool) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE report_schedules SET is_active=$2, updated_at=now() WHERE id=$1`,
		id, active)
	return err
}

func (r *ReportScheduleRepo) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM report_schedules WHERE id=$1`, id)
	return err
}

func (r *ReportScheduleRepo) GetByID(ctx context.Context, id uuid.UUID) (*report_schedule.ReportSchedule, error) {
	return scanSchedule(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, trading_account_id, frequency, hour_utc,
		       weekday, day_of_month, last_sent_at, next_send_at,
		       recipients, is_active, metadata, created_at, updated_at
		FROM report_schedules WHERE id=$1`, id))
}

func (r *ReportScheduleRepo) GetByIDByTenant(ctx context.Context, id uuid.UUID) (*report_schedule.ReportSchedule, error) {
	tenantID, err := tenant.TenantIDFromContext(ctx)
	if err != nil {
		return nil, err
	}
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, trading_account_id, frequency, hour_utc,
		       weekday, day_of_month, last_sent_at, next_send_at,
		       recipients, is_active, metadata, created_at, updated_at
		FROM report_schedules WHERE id=$1 AND tenant_id=$2`, id, tenantID)
	return scanSchedule(row)
}

func (r *ReportScheduleRepo) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*report_schedule.ReportSchedule, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, trading_account_id, frequency, hour_utc,
		       weekday, day_of_month, last_sent_at, next_send_at,
		       recipients, is_active, metadata, created_at, updated_at
		FROM report_schedules WHERE tenant_id=$1 ORDER BY next_send_at ASC`,
		tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*report_schedule.ReportSchedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *ReportScheduleRepo) ListDue(ctx context.Context, now time.Time, limit int) ([]*report_schedule.ReportSchedule, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, trading_account_id, frequency, hour_utc,
		       weekday, day_of_month, last_sent_at, next_send_at,
		       recipients, is_active, metadata, created_at, updated_at
		FROM report_schedules
		WHERE is_active = TRUE AND next_send_at <= $1
		ORDER BY next_send_at
		LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*report_schedule.ReportSchedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func scanSchedule(row rowScanner) (*report_schedule.ReportSchedule, error) {
	var s report_schedule.ReportSchedule
	var metaRaw []byte
	if err := row.Scan(
		&s.ID, &s.TenantID, &s.TradingAccountID, &s.Frequency, &s.HourUTC,
		&s.Weekday, &s.DayOfMonth, &s.LastSentAt, &s.NextSendAt,
		&s.Recipients, &s.IsActive, &metaRaw, &s.CreatedAt, &s.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal(metaRaw, &s.Metadata)
	if s.Metadata == nil {
		s.Metadata = map[string]any{}
	}
	if s.Recipients == nil {
		s.Recipients = []string{}
	}
	return &s, nil
}