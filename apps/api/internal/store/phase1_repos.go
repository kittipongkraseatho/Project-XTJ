package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/trading-journal/api/internal/domain/daily_metric"
	"github.com/trading-journal/api/internal/domain/deal"
	"github.com/trading-journal/api/internal/domain/import_job"
	"github.com/trading-journal/api/internal/domain/imported_file"
	"github.com/trading-journal/api/internal/domain/journal_entry"
	"github.com/trading-journal/api/internal/domain/position"
	"github.com/trading-journal/api/internal/domain/tag"
	"github.com/trading-journal/api/internal/domain/trade"
	"github.com/trading-journal/api/internal/domain/trade_image"
	"github.com/trading-journal/api/internal/domain/trading_account"
)

// ── TradingAccountRepo ────────────────────────────────────────────────

type TradingAccountRepo struct{ pool DBTX }

func (r *TradingAccountRepo) WithTx(tx DBTX) *TradingAccountRepo {
	return &TradingAccountRepo{pool: tx}
}

func (r *TradingAccountRepo) Create(ctx context.Context, a *trading_account.TradingAccount) error {
	meta, _ := json.Marshal(a.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO trading_accounts (id, user_id, tenant_id, broker, broker_account_id, display_name, currency, leverage, kind, metadata, is_active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING created_at, updated_at`,
		a.ID, a.UserID, a.TenantID, a.Broker, a.BrokerAccountID,
		a.DisplayName, a.Currency, a.Leverage, a.Kind, meta, a.IsActive,
	)
	return row.Scan(&a.CreatedAt, &a.UpdatedAt)
}

func (r *TradingAccountRepo) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*trading_account.TradingAccount, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, user_id, tenant_id, broker, broker_account_id, display_name, currency, leverage, kind, metadata, is_active, created_at, updated_at
		FROM trading_accounts
		WHERE tenant_id = $1
		ORDER BY created_at ASC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*trading_account.TradingAccount
	for rows.Next() {
		var a trading_account.TradingAccount
		var metaRaw []byte
		if err := rows.Scan(&a.ID, &a.UserID, &a.TenantID, &a.Broker, &a.BrokerAccountID,
			&a.DisplayName, &a.Currency, &a.Leverage, &a.Kind, &metaRaw, &a.IsActive,
			&a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		if len(metaRaw) > 0 {
			_ = json.Unmarshal(metaRaw, &a.Metadata)
		}
		if a.Metadata == nil {
			a.Metadata = map[string]any{}
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

func (r *TradingAccountRepo) GetByID(ctx context.Context, id uuid.UUID) (*trading_account.TradingAccount, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, user_id, tenant_id, broker, broker_account_id, display_name, currency, leverage, kind, metadata, is_active, created_at, updated_at
		FROM trading_accounts WHERE id = $1`, id)
	var a trading_account.TradingAccount
	var metaRaw []byte
	if err := row.Scan(&a.ID, &a.UserID, &a.TenantID, &a.Broker, &a.BrokerAccountID,
		&a.DisplayName, &a.Currency, &a.Leverage, &a.Kind, &metaRaw, &a.IsActive,
		&a.CreatedAt, &a.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(metaRaw) > 0 {
		_ = json.Unmarshal(metaRaw, &a.Metadata)
	}
	if a.Metadata == nil {
		a.Metadata = map[string]any{}
	}
	return &a, nil
}

// GetByIDByTenant is the tenant-scoped variant — returns ErrNotFound if the
// row exists but belongs to another tenant (or doesn't exist).
//
// Phase 5: used by the /v1/public/accounts/:id endpoint to guarantee
// tenant isolation even if a malicious caller crafts a UUID.
func (r *TradingAccountRepo) GetByIDByTenant(ctx context.Context, tenantID, id uuid.UUID) (*trading_account.TradingAccount, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, user_id, tenant_id, broker, broker_account_id, display_name, currency, leverage, kind, metadata, is_active, created_at, updated_at
		FROM trading_accounts WHERE id = $1 AND tenant_id = $2`, id, tenantID)
	var a trading_account.TradingAccount
	var metaRaw []byte
	if err := row.Scan(&a.ID, &a.UserID, &a.TenantID, &a.Broker, &a.BrokerAccountID,
		&a.DisplayName, &a.Currency, &a.Leverage, &a.Kind, &metaRaw, &a.IsActive,
		&a.CreatedAt, &a.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(metaRaw) > 0 {
		_ = json.Unmarshal(metaRaw, &a.Metadata)
	}
	if a.Metadata == nil {
		a.Metadata = map[string]any{}
	}
	return &a, nil
}

// ── ImportedFileRepo ──────────────────────────────────────────────────

type ImportedFileRepo struct{ pool DBTX }

func (r *ImportedFileRepo) WithTx(tx DBTX) *ImportedFileRepo {
	return &ImportedFileRepo{pool: tx}
}

// Create returns ErrNotFound if content_hash is duplicate for the tenant (unique violation).
func (r *ImportedFileRepo) Create(ctx context.Context, f *imported_file.ImportedFile) error {
	meta, _ := json.Marshal(f.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO imported_files (id, tenant_id, trading_account_id, user_id, format, original_filename, byte_size, content_hash, storage_path, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING created_at`,
		f.ID, f.TenantID, f.TradingAccountID, f.UserID, f.Format,
		f.OriginalFilename, f.ByteSize, f.ContentHash, f.StoragePath, meta)
	return row.Scan(&f.CreatedAt)
}

func (r *ImportedFileRepo) GetByContentHash(ctx context.Context, tenantID uuid.UUID, hash string) (*imported_file.ImportedFile, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, trading_account_id, user_id, format, original_filename, byte_size, content_hash, storage_path, metadata, created_at
		FROM imported_files
		WHERE tenant_id = $1 AND content_hash = $2`, tenantID, hash)
	var f imported_file.ImportedFile
	var metaRaw []byte
	if err := row.Scan(&f.ID, &f.TenantID, &f.TradingAccountID, &f.UserID, &f.Format,
		&f.OriginalFilename, &f.ByteSize, &f.ContentHash, &f.StoragePath, &metaRaw, &f.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(metaRaw) > 0 {
		_ = json.Unmarshal(metaRaw, &f.Metadata)
	}
	if f.Metadata == nil {
		f.Metadata = map[string]any{}
	}
	return &f, nil
}

// ── ImportJobRepo ─────────────────────────────────────────────────────

type ImportJobRepo struct{ pool DBTX }

func (r *ImportJobRepo) WithTx(tx DBTX) *ImportJobRepo { return &ImportJobRepo{pool: tx} }

func (r *ImportJobRepo) Create(ctx context.Context, j *import_job.ImportJob) error {
	errorsRaw, _ := json.Marshal(j.Errors)
	if errorsRaw == nil {
		errorsRaw = []byte("[]")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO import_jobs (id, tenant_id, imported_file_id, status, started_at, finished_at, parsed_rows, inserted_rows, skipped_rows, errors, worker_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING created_at, updated_at`,
		j.ID, j.TenantID, j.ImportedFileID, j.Status,
		j.StartedAt, j.FinishedAt, j.ParsedRows, j.InsertedRows, j.SkippedRows,
		errorsRaw, j.WorkerID)
	return row.Scan(&j.CreatedAt, &j.UpdatedAt)
}

func (r *ImportJobRepo) MarkRunning(ctx context.Context, id uuid.UUID) error {
	now := time.Now()
	_, err := r.pool.Exec(ctx, `UPDATE import_jobs SET status='running', started_at=$2, updated_at=NOW() WHERE id=$1`, id, now)
	return err
}

func (r *ImportJobRepo) MarkCompleted(ctx context.Context, id uuid.UUID, parsed, inserted, skipped int, errorsList []map[string]any) error {
	errsRaw, _ := json.Marshal(errorsList)
	if errsRaw == nil {
		errsRaw = []byte("[]")
	}
	now := time.Now()
	_, err := r.pool.Exec(ctx, `
		UPDATE import_jobs SET status='completed', finished_at=$2, parsed_rows=$3, inserted_rows=$4, skipped_rows=$5, errors=$6, updated_at=NOW()
		WHERE id=$1`, id, now, parsed, inserted, skipped, errsRaw)
	return err
}

func (r *ImportJobRepo) MarkFailed(ctx context.Context, id uuid.UUID, msg string) error {
	now := time.Now()
	errsRaw, _ := json.Marshal([]map[string]any{{"message": msg}})
	_, err := r.pool.Exec(ctx, `
		UPDATE import_jobs SET status='failed', finished_at=$2, errors=$3, updated_at=NOW()
		WHERE id=$1`, id, now, errsRaw)
	return err
}

func (r *ImportJobRepo) GetByID(ctx context.Context, id uuid.UUID) (*import_job.ImportJob, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, imported_file_id, status, started_at, finished_at, parsed_rows, inserted_rows, skipped_rows, errors, worker_id, created_at, updated_at
		FROM import_jobs WHERE id = $1`, id)
	var j import_job.ImportJob
	var errsRaw []byte
	if err := row.Scan(&j.ID, &j.TenantID, &j.ImportedFileID, &j.Status,
		&j.StartedAt, &j.FinishedAt, &j.ParsedRows, &j.InsertedRows, &j.SkippedRows,
		&errsRaw, &j.WorkerID, &j.CreatedAt, &j.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal(errsRaw, &j.Errors)
	if j.Errors == nil {
		j.Errors = []map[string]any{}
	}
	return &j, nil
}

func (r *ImportJobRepo) ListByTenant(ctx context.Context, tenantID uuid.UUID, limit int) ([]*import_job.ImportJob, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, imported_file_id, status, started_at, finished_at, parsed_rows, inserted_rows, skipped_rows, errors, worker_id, created_at, updated_at
		FROM import_jobs
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2`, tenantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*import_job.ImportJob
	for rows.Next() {
		var j import_job.ImportJob
		var errsRaw []byte
		if err := rows.Scan(&j.ID, &j.TenantID, &j.ImportedFileID, &j.Status,
			&j.StartedAt, &j.FinishedAt, &j.ParsedRows, &j.InsertedRows, &j.SkippedRows,
			&errsRaw, &j.WorkerID, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(errsRaw, &j.Errors)
		if j.Errors == nil {
			j.Errors = []map[string]any{}
		}
		out = append(out, &j)
	}
	return out, rows.Err()
}

// ── DealRepo ──────────────────────────────────────────────────────────

type DealRepo struct{ pool DBTX }

func (r *DealRepo) WithTx(tx DBTX) *DealRepo { return &DealRepo{pool: tx} }

// Insert inserts a deal with ON CONFLICT DO NOTHING (for dedup).
// Returns (inserted, error). inserted=false means it was a duplicate.
func (r *DealRepo) Insert(ctx context.Context, d *deal.Deal) (bool, error) {
	meta, _ := json.Marshal(d.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO deals (id, tenant_id, trading_account_id, import_job_id, provider_deal_id, symbol, deal_type, entry_type, volume, price, commission, swap, profit, magic, comment, executed_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (tenant_id, trading_account_id, provider_deal_id) DO NOTHING`,
		d.ID, d.TenantID, d.TradingAccountID, d.ImportJobID, d.ProviderDealID,
		d.Symbol, d.Type, d.EntryType, d.Volume, d.Price,
		d.Commission, d.Swap, d.Profit, d.Magic, d.Comment, d.ExecutedAt, meta)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *DealRepo) CountByAccount(ctx context.Context, accountID uuid.UUID) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM deals WHERE trading_account_id = $1`, accountID).Scan(&n)
	return n, err
}

// ── PositionRepo ─────────────────────────────────────────────────────

type PositionRepo struct{ pool DBTX }

func (r *PositionRepo) WithTx(tx DBTX) *PositionRepo { return &PositionRepo{pool: tx} }

func (r *PositionRepo) Insert(ctx context.Context, p *position.Position) error {
	meta, _ := json.Marshal(p.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO positions (id, tenant_id, trading_account_id, provider_position_id, direction, symbol, volume, open_price, close_price, open_time, close_time, state, realized_p_l, commission, swap, magic, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING created_at, updated_at`,
		p.ID, p.TenantID, p.TradingAccountID, p.ProviderPositionID,
		p.Direction, p.Symbol, p.Volume, p.OpenPrice, p.ClosePrice,
		p.OpenTime, p.CloseTime, p.State, p.RealizedPL, p.Commission, p.Swap, p.Magic, meta)
	return row.Scan(&p.CreatedAt, &p.UpdatedAt)
}

// ── TradeRepo ─────────────────────────────────────────────────────────

type TradeRepo struct{ pool DBTX }

func (r *TradeRepo) WithTx(tx DBTX) *TradeRepo { return &TradeRepo{pool: tx} }

func (r *TradeRepo) Insert(ctx context.Context, t *trade.Trade) error {
	meta, _ := json.Marshal(t.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO trades (id, tenant_id, user_id, trading_account_id, position_id, symbol, direction, volume, open_price, close_price, open_time, close_time, holding_seconds, gross_profit, gross_loss, net_p_l, commission, swap, magic, outcome, is_partial, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)
		RETURNING created_at, updated_at`,
		t.ID, t.TenantID, t.UserID, t.TradingAccountID, t.PositionID,
		t.Symbol, t.Direction, t.Volume, t.OpenPrice, t.ClosePrice,
		t.OpenTime, t.CloseTime, t.HoldingSeconds,
		t.GrossProfit, t.GrossLoss, t.NetPL, t.Commission, t.Swap, t.Magic,
		t.Outcome, t.IsPartial, meta)
	return row.Scan(&t.CreatedAt, &t.UpdatedAt)
}

// ListByTenant returns trades for a tenant, paginated, optionally filtered by account.
func (r *TradeRepo) ListByTenant(ctx context.Context, tenantID uuid.UUID, accountID *uuid.UUID, limit, offset int) ([]*trade.Trade, error) {
	q := `SELECT id, tenant_id, user_id, trading_account_id, position_id, symbol, direction, volume, open_price, close_price, open_time, close_time, holding_seconds, gross_profit, gross_loss, net_p_l, commission, swap, magic, outcome, is_partial, metadata, created_at, updated_at FROM trades WHERE tenant_id = $1`
	args := []any{tenantID}
	if accountID != nil {
		q += ` AND trading_account_id = $2`
		args = append(args, *accountID)
		q += fmtSprintfOrder(limit, offset, len(args))
	} else {
		q += fmtSprintfOrder(limit, offset, len(args))
	}
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*trade.Trade
	for rows.Next() {
		t, err := scanTrade(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (r *TradeRepo) GetByID(ctx context.Context, id uuid.UUID) (*trade.Trade, error) {
	row := r.pool.QueryRow(ctx, tradeSelect+` WHERE id = $1`, id)
	return scanTrade(row)
}

func (r *TradeRepo) CountByTenant(ctx context.Context, tenantID uuid.UUID) (int64, error) {
	var n int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM trades WHERE tenant_id = $1`, tenantID).Scan(&n)
	return n, err
}

func (r *TradeRepo) AggregateOverview(ctx context.Context, tenantID uuid.UUID, accountID *uuid.UUID) (map[string]any, error) {
	q := `
		SELECT
		  COUNT(*) AS trade_count,
		  COALESCE(SUM(net_p_l),0) AS net_p_l,
		  COALESCE(SUM(gross_profit),0) AS gross_profit,
		  COALESCE(SUM(gross_loss),0) AS gross_loss,
		  COUNT(*) FILTER (WHERE outcome='win') AS wins,
		  COUNT(*) FILTER (WHERE outcome='loss') AS losses
		FROM trades
		WHERE tenant_id = $1 AND close_time IS NOT NULL`
	args := []any{tenantID}
	if accountID != nil {
		q += ` AND trading_account_id = $2`
		args = append(args, *accountID)
	}
	var tradeCount, wins, losses int64
	var netPL, grossProfit, grossLoss float64
	if err := r.pool.QueryRow(ctx, q, args...).Scan(&tradeCount, &netPL, &grossProfit, &grossLoss, &wins, &losses); err != nil {
		return nil, err
	}
	winRate := 0.0
	if tradeCount > 0 {
		winRate = float64(wins) / float64(tradeCount)
	}
	profitFactor := 0.0
	if grossLoss < 0 {
		profitFactor = grossProfit / (-grossLoss)
	}
	avgWin, avgLoss := 0.0, 0.0
	if wins > 0 {
		avgWin = grossProfit / float64(wins)
	}
	if losses > 0 {
		avgLoss = grossLoss / float64(losses)
	}
	expectancy := (winRate * avgWin) + ((1 - winRate) * avgLoss)
	return map[string]any{
		"trade_count":   tradeCount,
		"net_p_l":       netPL,
		"gross_profit":  grossProfit,
		"gross_loss":    grossLoss,
		"wins":          wins,
		"losses":        losses,
		"win_rate":      winRate,
		"profit_factor": profitFactor,
		"avg_win":       avgWin,
		"avg_loss":      avgLoss,
		"expectancy":    expectancy,
	}, nil
}

const tradeSelect = `
SELECT id, tenant_id, user_id, trading_account_id, position_id, symbol, direction, volume, open_price, close_price, open_time, close_time, holding_seconds, gross_profit, gross_loss, net_p_l, commission, swap, magic, outcome, is_partial, metadata, created_at, updated_at
FROM trades`

func scanTrade(row pgx.Row) (*trade.Trade, error) {
	var t trade.Trade
	var metaRaw []byte
	if err := row.Scan(
		&t.ID, &t.TenantID, &t.UserID, &t.TradingAccountID, &t.PositionID,
		&t.Symbol, &t.Direction, &t.Volume, &t.OpenPrice, &t.ClosePrice,
		&t.OpenTime, &t.CloseTime, &t.HoldingSeconds,
		&t.GrossProfit, &t.GrossLoss, &t.NetPL, &t.Commission, &t.Swap, &t.Magic,
		&t.Outcome, &t.IsPartial, &metaRaw, &t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(metaRaw) > 0 {
		_ = json.Unmarshal(metaRaw, &t.Metadata)
	}
	if t.Metadata == nil {
		t.Metadata = map[string]any{}
	}
	return &t, nil
}

// EquityCurve returns per-trade equity series for charting.
func (r *TradeRepo) EquityCurve(ctx context.Context, tenantID uuid.UUID, accountID *uuid.UUID, limit int) ([]map[string]any, error) {
	q := `
		SELECT
		  to_char(close_time AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"') AS closed_at,
		  net_p_l,
		  SUM(net_p_l) OVER (ORDER BY close_time ASC ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) AS equity
		FROM trades
		WHERE tenant_id = $1 AND close_time IS NOT NULL`
	args := []any{tenantID}
	if accountID != nil {
		q += ` AND trading_account_id = $2`
		args = append(args, *accountID)
	}
	q += ` ORDER BY close_time ASC LIMIT $` + intToStr(len(args)+1)
	args = append(args, limit)
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var closedAt string
		var net, equity float64
		if err := rows.Scan(&closedAt, &net, &equity); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"closed_at": closedAt,
			"net_p_l":   net,
			"equity":    equity,
		})
	}
	return out, rows.Err()
}

func intToStr(n int) string {
	const digits = "0123456789"
	if n == 0 {
		return "1"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{digits[n%10]}, b...)
		n /= 10
	}
	return string(b)
}

// SymbolBreakdown returns per-symbol aggregated KPIs.
func (r *TradeRepo) SymbolBreakdown(ctx context.Context, tenantID uuid.UUID, accountID *uuid.UUID) ([]map[string]any, error) {
	q := `
		SELECT
		  symbol,
		  COUNT(*) AS trade_count,
		  COALESCE(SUM(net_p_l), 0) AS net_p_l,
		  COALESCE(SUM(gross_profit), 0) AS gross_profit,
		  COALESCE(SUM(gross_loss), 0) AS gross_loss,
		  COUNT(*) FILTER (WHERE outcome = 'win') AS wins,
		  COUNT(*) FILTER (WHERE outcome = 'loss') AS losses
		FROM trades
		WHERE tenant_id = $1 AND close_time IS NOT NULL`
	args := []any{tenantID}
	if accountID != nil {
		q += ` AND trading_account_id = $2`
		args = append(args, *accountID)
	}
	q += ` GROUP BY symbol ORDER BY net_p_l DESC`
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var symbol string
		var count int64
		var netPL, grossProfit, grossLoss float64
		var wins, losses int64
		if err := rows.Scan(&symbol, &count, &netPL, &grossProfit, &grossLoss, &wins, &losses); err != nil {
			return nil, err
		}
		winRate := 0.0
		if count > 0 {
			winRate = float64(wins) / float64(count)
		}
		avgWin, avgLoss := 0.0, 0.0
		if wins > 0 {
			avgWin = grossProfit / float64(wins)
		}
		if losses > 0 {
			avgLoss = grossLoss / float64(losses)
		}
		out = append(out, map[string]any{
			"symbol":    symbol,
			"count":     count,
			"net_p_l":   netPL,
			"wins":      wins,
			"losses":    losses,
			"win_rate":  winRate,
			"avg_win":   avgWin,
			"avg_loss":  avgLoss,
		})
	}
	return out, rows.Err()
}

// fmtSprintfOrder is a tiny helper that appends LIMIT/OFFSET args in order.
// Kept in this file to avoid pulling fmt into the whole package when not needed.
func fmtSprintfOrder(limit, offset, baseIdx int) string {
	// queries
	//nolint:gosimple
	switch baseIdx {
	case 1:
		return ` ORDER BY close_time DESC NULLS LAST LIMIT 2 OFFSET 3`
	case 2:
		return ` ORDER BY close_time DESC NULLS LAST LIMIT 3 OFFSET 4`
	}
	return ` ORDER BY close_time DESC NULLS LAST LIMIT $2 OFFSET $3`
}

// ── JournalEntryRepo ──────────────────────────────────────────────────

type JournalEntryRepo struct{ pool DBTX }

func (r *JournalEntryRepo) WithTx(tx DBTX) *JournalEntryRepo { return &JournalEntryRepo{pool: tx} }

func (r *JournalEntryRepo) Upsert(ctx context.Context, e *journal_entry.JournalEntry) error {
	meta, _ := json.Marshal(e.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO journal_entries (id, trade_id, tenant_id, user_id, strategy, entry_reason, exit_reason, emotion, mistake, notes, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (trade_id) DO UPDATE SET
		  strategy = EXCLUDED.strategy,
		  entry_reason = EXCLUDED.entry_reason,
		  exit_reason = EXCLUDED.exit_reason,
		  emotion = EXCLUDED.emotion,
		  mistake = EXCLUDED.mistake,
		  notes = EXCLUDED.notes,
		  metadata = EXCLUDED.metadata,
		  updated_at = NOW()
		RETURNING created_at, updated_at`,
		e.ID, e.TradeID, e.TenantID, e.UserID,
		e.Strategy, e.EntryReason, e.ExitReason, e.Emotion, e.Mistake, e.Notes, meta)
	return row.Scan(&e.CreatedAt, &e.UpdatedAt)
}

func (r *JournalEntryRepo) GetByTradeID(ctx context.Context, tradeID uuid.UUID) (*journal_entry.JournalEntry, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, trade_id, tenant_id, user_id, strategy, entry_reason, exit_reason, emotion, mistake, notes, metadata, created_at, updated_at
		FROM journal_entries WHERE trade_id = $1`, tradeID)
	var e journal_entry.JournalEntry
	var metaRaw []byte
	if err := row.Scan(&e.ID, &e.TradeID, &e.TenantID, &e.UserID,
		&e.Strategy, &e.EntryReason, &e.ExitReason, &e.Emotion, &e.Mistake, &e.Notes, &metaRaw, &e.CreatedAt, &e.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(metaRaw) > 0 {
		_ = json.Unmarshal(metaRaw, &e.Metadata)
	}
	if e.Metadata == nil {
		e.Metadata = map[string]any{}
	}
	return &e, nil
}

// ── TagRepo + TradeTagsRepo ───────────────────────────────────────────

type TagRepo struct{ pool DBTX }

func (r *TagRepo) WithTx(tx DBTX) *TagRepo { return &TagRepo{pool: tx} }

func (r *TagRepo) Create(ctx context.Context, t *tag.Tag) error {
	meta, _ := json.Marshal(t.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO tags (id, tenant_id, name, color, owner, metadata)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at`,
		t.ID, t.TenantID, t.Name, t.Color, t.Owner, meta)
	return row.Scan(&t.CreatedAt, &t.UpdatedAt)
}

func (r *TagRepo) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*tag.Tag, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, name, color, owner, metadata, created_at, updated_at
		FROM tags WHERE tenant_id = $1 ORDER BY name ASC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*tag.Tag
	for rows.Next() {
		var t tag.Tag
		var metaRaw []byte
		if err := rows.Scan(&t.ID, &t.TenantID, &t.Name, &t.Color, &t.Owner, &metaRaw, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(metaRaw, &t.Metadata)
		if t.Metadata == nil {
			t.Metadata = map[string]any{}
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

func (r *TagRepo) GetByID(ctx context.Context, id uuid.UUID) (*tag.Tag, error) {
	row := r.pool.QueryRow(ctx, `SELECT id, tenant_id, name, color, owner, metadata, created_at, updated_at FROM tags WHERE id = $1`, id)
	var t tag.Tag
	var metaRaw []byte
	if err := row.Scan(&t.ID, &t.TenantID, &t.Name, &t.Color, &t.Owner, &metaRaw, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(metaRaw) > 0 {
		_ = json.Unmarshal(metaRaw, &t.Metadata)
	}
	if t.Metadata == nil {
		t.Metadata = map[string]any{}
	}
	return &t, nil
}

func (r *TagRepo) Delete(ctx context.Context, id uuid.UUID) error {
	_, e := r.pool.Exec(ctx, `DELETE FROM tags WHERE id = $1`, id)
	return e
}

// TradeTagsRepo manages the many-to-many trade ↔ tag relation.
type TradeTagsRepo struct{ pool DBTX }

func (r *TradeTagsRepo) WithTx(tx DBTX) *TradeTagsRepo { return &TradeTagsRepo{pool: tx} }

func (r *TradeTagsRepo) SetTagsForTrade(ctx context.Context, tradeID uuid.UUID, tagIDs []uuid.UUID) error {
	if _, err := r.pool.Exec(ctx, `DELETE FROM trade_tags WHERE trade_id = $1`, tradeID); err != nil {
		return err
	}
	for _, tagID := range tagIDs {
		if _, err := r.pool.Exec(ctx, `INSERT INTO trade_tags (trade_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, tradeID, tagID); err != nil {
			return err
		}
	}
	return nil
}

func (r *TradeTagsRepo) ListTagsForTrade(ctx context.Context, tradeID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.pool.Query(ctx, `SELECT tag_id FROM trade_tags WHERE trade_id = $1`, tradeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ── TradeImageRepo ────────────────────────────────────────────────────

type TradeImageRepo struct{ pool DBTX }

func (r *TradeImageRepo) WithTx(tx DBTX) *TradeImageRepo { return &TradeImageRepo{pool: tx} }

func (r *TradeImageRepo) Insert(ctx context.Context, i *trade_image.TradeImage) error {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO trade_images (id, tenant_id, trade_id, user_id, cloudinary_public_id, cloudinary_asset_id, resource_type, format, width, height, bytes, secure_url, folder, version, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING created_at`,
		i.ID, i.TenantID, i.TradeID, i.UserID,
		i.CloudinaryPublicID, i.CloudinaryAssetID, i.ResourceType, i.Format,
		i.Width, i.Height, i.Bytes, i.SecureURL, i.Folder, i.Version, i.Status)
	return row.Scan(&i.CreatedAt)
}

func (r *TradeImageRepo) ListByTrade(ctx context.Context, tradeID uuid.UUID) ([]*trade_image.TradeImage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, trade_id, user_id, cloudinary_public_id, cloudinary_asset_id, resource_type, format, width, height, bytes, secure_url, folder, version, status, deleted_at, created_at
		FROM trade_images
		WHERE trade_id = $1 AND deleted_at IS NULL
		ORDER BY created_at ASC`, tradeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*trade_image.TradeImage
	for rows.Next() {
		var i trade_image.TradeImage
		if err := rows.Scan(&i.ID, &i.TenantID, &i.TradeID, &i.UserID,
			&i.CloudinaryPublicID, &i.CloudinaryAssetID, &i.ResourceType, &i.Format,
			&i.Width, &i.Height, &i.Bytes, &i.SecureURL, &i.Folder, &i.Version, &i.Status, &i.DeletedAt, &i.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &i)
	}
	return out, rows.Err()
}

// ── DailyMetricRepo ───────────────────────────────────────────────────

type DailyMetricRepo struct{ pool DBTX }

func (r *DailyMetricRepo) WithTx(tx DBTX) *DailyMetricRepo { return &DailyMetricRepo{pool: tx} }

func (r *DailyMetricRepo) Upsert(ctx context.Context, m *daily_metric.DailyMetric) error {
	meta, _ := json.Marshal(m.Metadata)
	if meta == nil {
		meta = []byte("{}")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO daily_metrics (id, tenant_id, trading_account_id, trade_date, trade_count, win_count, loss_count, gross_profit, gross_loss, net_p_l, commission, swap, ending_equity, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (tenant_id, trading_account_id, trade_date) DO UPDATE SET
		  trade_count = EXCLUDED.trade_count,
		  win_count = EXCLUDED.win_count,
		  loss_count = EXCLUDED.loss_count,
		  gross_profit = EXCLUDED.gross_profit,
		  gross_loss = EXCLUDED.gross_loss,
		  net_p_l = EXCLUDED.net_p_l,
		  commission = EXCLUDED.commission,
		  swap = EXCLUDED.swap,
		  ending_equity = EXCLUDED.ending_equity,
		  metadata = EXCLUDED.metadata,
		  updated_at = NOW()
		RETURNING created_at, updated_at`,
		m.ID, m.TenantID, m.TradingAccountID, m.TradeDate,
		m.TradeCount, m.WinCount, m.LossCount,
		m.GrossProfit, m.GrossLoss, m.NetPL, m.Commission, m.Swap,
		m.EndingEquity, meta)
	return row.Scan(&m.CreatedAt, &m.UpdatedAt)
}

func (r *DailyMetricRepo) ListByTenantRange(ctx context.Context, tenantID uuid.UUID, from, to time.Time) ([]*daily_metric.DailyMetric, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, trading_account_id, trade_date, trade_count, win_count, loss_count, gross_profit, gross_loss, net_p_l, commission, swap, ending_equity, metadata, created_at, updated_at
		FROM daily_metrics
		WHERE tenant_id = $1 AND trade_date BETWEEN $2 AND $3
		ORDER BY trade_date ASC`, tenantID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*daily_metric.DailyMetric
	for rows.Next() {
		var m daily_metric.DailyMetric
		var metaRaw []byte
		if err := rows.Scan(&m.ID, &m.TenantID, &m.TradingAccountID, &m.TradeDate,
			&m.TradeCount, &m.WinCount, &m.LossCount,
			&m.GrossProfit, &m.GrossLoss, &m.NetPL, &m.Commission, &m.Swap,
			&m.EndingEquity, &metaRaw, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(metaRaw, &m.Metadata)
		if m.Metadata == nil {
			m.Metadata = map[string]any{}
		}
		out = append(out, &m)
	}
	return out, rows.Err()
}