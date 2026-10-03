package store

import (
	"context"
	"errors"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/trading-journal/api/internal/domain/plan"
)

type PlanRepo struct{ pool DBTX }

func (r *PlanRepo) WithTx(tx DBTX) *PlanRepo { return &PlanRepo{pool: tx} }

func (r *PlanRepo) GetByID(ctx context.Context, id uuid.UUID) (*plan.Plan, error) {
	row := r.pool.QueryRow(ctx, planSelect+` WHERE id = $1`, id)
	return scanPlan(row)
}

func (r *PlanRepo) GetByCode(ctx context.Context, code plan.Code) (*plan.Plan, error) {
	row := r.pool.QueryRow(ctx, planSelect+` WHERE code = $1`, code)
	return scanPlan(row)
}

func (r *PlanRepo) ListActive(ctx context.Context) ([]*plan.Plan, error) {
	rows, err := r.pool.Query(ctx, planSelect+` WHERE is_active = TRUE ORDER BY price_cents ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*plan.Plan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

const planSelect = `
SELECT id, code, name, description, price_cents, currency, interval, is_active, metadata, created_at, updated_at
FROM plans`

func scanPlan(row pgx.Row) (*plan.Plan, error) {
	var p plan.Plan
	if err := row.Scan(&p.ID, &p.Code, &p.Name, &p.Description, &p.PriceCents, &p.Currency, &p.Interval, &p.IsActive, &p.Metadata, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if p.Metadata == nil {
		p.Metadata = map[string]any{}
	}
	return &p, nil
}