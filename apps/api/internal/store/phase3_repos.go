package store

import (
	"context"
	"errors"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"
)

// ── Phase 3 — advanced analytics aggregations on `trades` ─────────────
//
// All methods are tenant-scoped (every query starts with `tenant_id = $1`).
// `accountID` is optional (nil → aggregate across the tenant's whole portfolio).

// BucketRow is the common shape for all Phase 3 aggregation endpoints:
// a single bucket (session / EA / weekday / hour) plus its win/loss stats.
type BucketRow struct {
	Bucket       string  `json:"bucket"`
	TradeCount   int64   `json:"trade_count"`
	Wins         int64   `json:"wins"`
	Losses       int64   `json:"losses"`
	Breakevens   int64   `json:"breakevens"`
	NetPL        float64 `json:"net_p_l"`
	GrossProfit  float64 `json:"gross_profit"`
	GrossLoss    float64 `json:"gross_loss"`
	ProfitFactor float64 `json:"profit_factor"`
	WinRate      float64 `json:"win_rate"`
}

// bucketArgs returns the (sql, args) tail common to all 4 aggregations:
//   - base WHERE on tenant_id
//   - optional trading_account_id filter
//   - "AND close_time IS NOT NULL" — only closed trades count toward P/L.
func bucketArgs(tenantID uuid.UUID, accountID *uuid.UUID, baseFrom string) (string, []any) {
	args := []any{tenantID}
	q := baseFrom + ` WHERE tenant_id = $1 AND close_time IS NOT NULL`
	if accountID != nil {
		q += ` AND trading_account_id = $2`
		args = append(args, *accountID)
	}
	return q, args
}

func computeBucketStats(rows []BucketRow) []BucketRow {
	for i := range rows {
		if rows[i].TradeCount > 0 {
			rows[i].WinRate = float64(rows[i].Wins) / float64(rows[i].TradeCount)
		}
		if rows[i].GrossLoss < 0 {
			rows[i].ProfitFactor = rows[i].GrossProfit / (-rows[i].GrossLoss)
		}
	}
	return rows
}

// ── Session breakdown (Asia / London / New York / Sydney, overlap-aware) ─

// SessionBreakdown returns one BucketRow per session. Note sessions are
// NOT mutually exclusive — a London/NY overlap trade counts in both.
//
// UTC windows (from Phase 3 spec §3.2):
//   - Asia     00:00–09:00
//   - London   07:00–16:00
//   - New York 13:00–22:00
//   - Sydney   22:00–07:00 (wraps midnight)
func (r *TradeRepo) SessionBreakdown(ctx context.Context, tenantID uuid.UUID, accountID *uuid.UUID) ([]BucketRow, error) {
	q := `
		WITH buckets AS (
		  SELECT
		    CASE
		      WHEN (open_time AT TIME ZONE 'UTC')::time BETWEEN '00:00:00' AND '09:00:00' THEN 'asia'
		      WHEN (open_time AT TIME ZONE 'UTC')::time BETWEEN '07:00:00' AND '16:00:00' THEN 'london'
		      WHEN (open_time AT TIME ZONE 'UTC')::time BETWEEN '13:00:00' AND '22:00:00' THEN 'newyork'
		      ELSE 'sydney'
		    END AS bucket,
		    net_p_l, gross_profit, gross_loss, outcome
		  FROM trades`
	q, args := bucketArgs(tenantID, accountID, q)
	q += `
		)
		SELECT bucket,
		       COUNT(*) AS n,
		       COUNT(*) FILTER (WHERE outcome='win') AS wins,
		       COUNT(*) FILTER (WHERE outcome='loss') AS losses,
		       COUNT(*) FILTER (WHERE outcome='breakeven') AS bes,
		       COALESCE(SUM(net_p_l),0),
		       COALESCE(SUM(gross_profit),0),
		       COALESCE(SUM(gross_loss),0)
		FROM buckets WHERE bucket IS NOT NULL
		GROUP BY bucket
		ORDER BY bucket`
	return scanBucketRows(ctx, r.pool, q, args...)
}

// ── EA (magic) breakdown ──────────────────────────────────────────────

// EABreakdown returns one BucketRow per MT5 magic. magic=0 → 'manual' label.
// Other magics → 'magic-N' label.
func (r *TradeRepo) EABreakdown(ctx context.Context, tenantID uuid.UUID, accountID *uuid.UUID) ([]BucketRow, error) {
	q := `
		SELECT COALESCE(magic::text, '0') AS bucket,
		       COUNT(*) AS n,
		       COUNT(*) FILTER (WHERE outcome='win'),
		       COUNT(*) FILTER (WHERE outcome='loss'),
		       COUNT(*) FILTER (WHERE outcome='breakeven'),
		       COALESCE(SUM(net_p_l),0),
		       COALESCE(SUM(gross_profit),0),
		       COALESCE(SUM(gross_loss),0)
		FROM trades`
	q, args := bucketArgs(tenantID, accountID, q)
	q += `
		GROUP BY magic
		ORDER BY SUM(net_p_l) DESC`
	return scanBucketRows(ctx, r.pool, q, args...)
}

// ── Day-of-week (0=Sunday … 6=Saturday, Postgres EXTRACT) ─────────────

func (r *TradeRepo) DayOfWeekBreakdown(ctx context.Context, tenantID uuid.UUID, accountID *uuid.UUID) ([]BucketRow, error) {
	q := `
		SELECT EXTRACT(DOW FROM open_time AT TIME ZONE 'UTC')::int AS bucket,
		       COUNT(*) AS n,
		       COUNT(*) FILTER (WHERE outcome='win'),
		       COUNT(*) FILTER (WHERE outcome='loss'),
		       COUNT(*) FILTER (WHERE outcome='breakeven'),
		       COALESCE(SUM(net_p_l),0),
		       COALESCE(SUM(gross_profit),0),
		       COALESCE(SUM(gross_loss),0)
		FROM trades`
	q, args := bucketArgs(tenantID, accountID, q)
	q += `
		GROUP BY bucket
		ORDER BY bucket`
	return scanBucketRows(ctx, r.pool, q, args...)
}

// ── Hour-of-day (0..23) ───────────────────────────────────────────────

func (r *TradeRepo) HourOfDayBreakdown(ctx context.Context, tenantID uuid.UUID, accountID *uuid.UUID) ([]BucketRow, error) {
	q := `
		SELECT EXTRACT(HOUR FROM open_time AT TIME ZONE 'UTC')::int AS bucket,
		       COUNT(*) AS n,
		       COUNT(*) FILTER (WHERE outcome='win'),
		       COUNT(*) FILTER (WHERE outcome='loss'),
		       COUNT(*) FILTER (WHERE outcome='breakeven'),
		       COALESCE(SUM(net_p_l),0),
		       COALESCE(SUM(gross_profit),0),
		       COALESCE(SUM(gross_loss),0)
		FROM trades`
	q, args := bucketArgs(tenantID, accountID, q)
	q += `
		GROUP BY bucket
		ORDER BY bucket`
	return scanBucketRows(ctx, r.pool, q, args...)
}

// ── shared scan helper ───────────────────────────────────────────────

func scanBucketRows(ctx context.Context, db DBTX, q string, args ...any) ([]BucketRow, error) {
	rows, err := db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]BucketRow, 0, 4)
	for rows.Next() {
		var b BucketRow
		if err := rows.Scan(&b.Bucket, &b.TradeCount, &b.Wins, &b.Losses, &b.Breakevens,
			&b.NetPL, &b.GrossProfit, &b.GrossLoss); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return computeBucketStats(out), rows.Err()
}

// ── Phase 3 §2.2 — plan-limit enforcement ──────────────────────────────

// CountTradingAccountsByTenant is used by the CreateAccount handler to enforce
// the plan's max-trading-accounts limit.
func (r *TradingAccountRepo) CountByTenant(ctx context.Context, tenantID uuid.UUID) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM trading_accounts WHERE tenant_id=$1`, tenantID).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

// CountBrokerConnectionsByTenant mirrors the above for broker_connections.
func (r *BrokerConnectionRepo) CountByTenant(ctx context.Context, tenantID uuid.UUID) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM broker_connections WHERE tenant_id=$1`, tenantID).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return n, err
}