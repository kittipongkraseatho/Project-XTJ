package store

import (
	"context"
	"errors"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/trading-journal/api/internal/domain/membership"
)

type MembershipRepo struct{ pool DBTX }

func (r *MembershipRepo) WithTx(tx DBTX) *MembershipRepo { return &MembershipRepo{pool: tx} }

func (r *MembershipRepo) Create(ctx context.Context, m *membership.Membership) error {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO memberships (id, tenant_id, user_id, role, status)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at, updated_at`,
		m.ID, m.TenantID, m.UserID, m.Role, m.Status)
	return row.Scan(&m.CreatedAt, &m.UpdatedAt)
}

func (r *MembershipRepo) GetByUserAndTenant(ctx context.Context, userID, tenantID uuid.UUID) (*membership.Membership, error) {
	row := r.pool.QueryRow(ctx, membershipSelect+` WHERE user_id = $1 AND tenant_id = $2`, userID, tenantID)
	return scanMembership(row)
}

func (r *MembershipRepo) ListByUser(ctx context.Context, userID uuid.UUID) ([]*membership.Membership, error) {
	rows, err := r.pool.Query(ctx, membershipSelect+` WHERE user_id = $1 ORDER BY created_at ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*membership.Membership
	for rows.Next() {
		m, err := scanMembership(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

const membershipSelect = `
SELECT id, tenant_id, user_id, role, status, created_at, updated_at
FROM memberships`

// row can be a pgx.Row (single) or pgx.Rows (multi); both satisfy pgx.Row.
func scanMembership(row pgx.Row) (*membership.Membership, error) {
	var m membership.Membership
	if err := row.Scan(&m.ID, &m.TenantID, &m.UserID, &m.Role, &m.Status, &m.CreatedAt, &m.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &m, nil
}