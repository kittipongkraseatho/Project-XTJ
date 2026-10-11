package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/trading-journal/api/internal/domain/behavior"
	"github.com/trading-journal/api/internal/domain/strategy"
)

// ── R2 behavior profile repositories ───────────────────────────────

type BehaviorProfileRepo struct{ pool DBTX }

func (r *BehaviorProfileRepo) WithTx(tx DBTX) *BehaviorProfileRepo {
	return &BehaviorProfileRepo{pool: tx}
}

func (r *BehaviorProfileRepo) Upsert(ctx context.Context, profile *behavior.Profile) error {
	if profile.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		profile.ID = id
	}
	if profile.CreatedAt.IsZero() {
		profile.CreatedAt = time.Now().UTC()
	}
	metrics, err := json.Marshal(profile.Metrics)
	if err != nil {
		return err
	}
	return r.pool.QueryRow(ctx, `
		INSERT INTO trade_behavior_profiles
			(id, tenant_id, trading_account_id, window_start, window_end,
			 algorithm_version, status, sample_size, metrics, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (tenant_id, trading_account_id, window_start, window_end, algorithm_version)
		DO UPDATE SET status = EXCLUDED.status,
		              sample_size = EXCLUDED.sample_size,
		              metrics = EXCLUDED.metrics,
		              created_at = EXCLUDED.created_at
		RETURNING id, created_at`,
		profile.ID, profile.TenantID, profile.TradingAccountID,
		profile.WindowStart, profile.WindowEnd, profile.AlgorithmVersion,
		profile.Status, profile.SampleSize, metrics, profile.CreatedAt,
	).Scan(&profile.ID, &profile.CreatedAt)
}

func (r *BehaviorProfileRepo) GetLatest(ctx context.Context, tenantID, accountID uuid.UUID) (*behavior.Profile, error) {
	return scanBehaviorProfile(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, trading_account_id, window_start, window_end,
		       algorithm_version, status, sample_size, metrics, created_at
		FROM trade_behavior_profiles
		WHERE tenant_id = $1 AND trading_account_id = $2
		ORDER BY window_end DESC, created_at DESC
		LIMIT 1`, tenantID, accountID))
}

func (r *BehaviorProfileRepo) GetByWindow(ctx context.Context, tenantID, accountID uuid.UUID, from, to time.Time, algorithmVersion string) (*behavior.Profile, error) {
	return scanBehaviorProfile(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, trading_account_id, window_start, window_end,
		       algorithm_version, status, sample_size, metrics, created_at
		FROM trade_behavior_profiles
		WHERE tenant_id = $1 AND trading_account_id = $2
		  AND window_start = $3 AND window_end = $4 AND algorithm_version = $5`,
		tenantID, accountID, from, to, algorithmVersion))
}

type BehaviorFindingRepo struct{ pool DBTX }

func (r *BehaviorFindingRepo) WithTx(tx DBTX) *BehaviorFindingRepo {
	return &BehaviorFindingRepo{pool: tx}
}

func (r *BehaviorFindingRepo) ReplaceForProfile(ctx context.Context, tenantID, profileID uuid.UUID, findings []behavior.Finding) error {
	if _, err := r.pool.Exec(ctx, `
		DELETE FROM trade_behavior_findings
		WHERE tenant_id = $1 AND profile_id = $2`, tenantID, profileID); err != nil {
		return err
	}
	for _, finding := range findings {
		evidence, err := json.Marshal(struct {
			TradeIDs   []uuid.UUID `json:"trade_ids"`
			Timestamps []time.Time `json:"timestamps"`
		}{finding.EvidenceIDs, finding.EvidenceAt})
		if err != nil {
			return err
		}
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if _, err := r.pool.Exec(ctx, `
			INSERT INTO trade_behavior_findings
				(id, tenant_id, profile_id, code, severity, finding_count, evidence)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			id, tenantID, profileID, finding.Code, finding.Severity, finding.Count, evidence); err != nil {
			return err
		}
	}
	return nil
}

// ListCompletedByAccountWindow returns the canonical trade inputs used by the
// deterministic behavior calculator. The close-time predicate excludes open
// trades from behavior scoring while the open-time bounds define the window.
func (r *TradeRepo) ListCompletedByAccountWindow(ctx context.Context, tenantID, accountID uuid.UUID, from, to time.Time) ([]behavior.InputTrade, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, symbol, direction, volume, net_p_l, outcome, open_time, close_time, magic
		FROM trades
		WHERE tenant_id = $1 AND trading_account_id = $2
		  AND close_time IS NOT NULL
		  AND open_time >= $3 AND open_time < $4
		ORDER BY open_time ASC, id ASC`, tenantID, accountID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	trades := make([]behavior.InputTrade, 0)
	for rows.Next() {
		var input behavior.InputTrade
		if err := rows.Scan(
			&input.ID, &input.Symbol, &input.Direction, &input.Volume,
			&input.NetPL, &input.Outcome, &input.OpenTime, &input.CloseTime, &input.Magic,
		); err != nil {
			return nil, err
		}
		trades = append(trades, input)
	}
	return trades, rows.Err()
}

func (r *TradeRepo) LinkStrategyVersion(ctx context.Context, tenantID, tradeID, versionID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE trades
		SET strategy_version_id = $3, updated_at = now()
		WHERE id = $1 AND tenant_id = $2`, tradeID, tenantID, versionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ── R2 strategy repositories ──────────────────────────────────────

type StrategyRepo struct{ pool DBTX }

func (r *StrategyRepo) WithTx(tx DBTX) *StrategyRepo { return &StrategyRepo{pool: tx} }

func (r *StrategyRepo) Create(ctx context.Context, item *strategy.Strategy) error {
	if item.ID == uuid.Nil {
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		item.ID = id
	}
	return r.pool.QueryRow(ctx, `
		INSERT INTO strategies
			(id, tenant_id, owner_user_id, name, description, status)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING created_at, updated_at`,
		item.ID, item.TenantID, item.OwnerUserID, item.Name, item.Description, item.Status,
	).Scan(&item.CreatedAt, &item.UpdatedAt)
}

func (r *StrategyRepo) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*strategy.Strategy, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, owner_user_id, name, description, status,
		       current_version_id, created_at, updated_at
		FROM strategies
		WHERE tenant_id = $1
		ORDER BY created_at DESC, id DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*strategy.Strategy, 0)
	for rows.Next() {
		item, err := scanStrategy(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *StrategyRepo) GetByIDByTenant(ctx context.Context, tenantID, id uuid.UUID) (*strategy.Strategy, error) {
	return scanStrategy(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, owner_user_id, name, description, status,
		       current_version_id, created_at, updated_at
		FROM strategies
		WHERE id = $1 AND tenant_id = $2`, id, tenantID))
}

func (r *StrategyRepo) SetCurrentVersion(ctx context.Context, tenantID, strategyID, versionID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE strategies
		SET current_version_id = $3, updated_at = now()
		WHERE id = $1 AND tenant_id = $2`, strategyID, tenantID, versionID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type StrategyVersionRepo struct{ pool DBTX }

func (r *StrategyVersionRepo) WithTx(tx DBTX) *StrategyVersionRepo {
	return &StrategyVersionRepo{pool: tx}
}

func (r *StrategyVersionRepo) CreateNext(ctx context.Context, tenantID, strategyID, createdBy uuid.UUID, rules strategy.RuleSet) (*strategy.Version, error) {
	raw, err := json.Marshal(rules)
	if err != nil {
		return nil, err
	}
	if _, err := strategy.ValidateRuleSet(raw); err != nil {
		return nil, err
	}

	var lockedID uuid.UUID
	if err := r.pool.QueryRow(ctx, `
		SELECT id
		FROM strategies
		WHERE id = $1 AND tenant_id = $2
		FOR UPDATE`, strategyID, tenantID).Scan(&lockedID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	var versionNo int
	if err := r.pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(version_no), 0) + 1
		FROM strategy_versions
		WHERE tenant_id = $1 AND strategy_id = $2`, tenantID, strategyID).Scan(&versionNo); err != nil {
		return nil, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	version := &strategy.Version{
		ID:            id,
		TenantID:      tenantID,
		StrategyID:    strategyID,
		VersionNo:     versionNo,
		SchemaVersion: rules.SchemaVersion,
		Rules:         rules,
		CreatedBy:     createdBy,
	}
	if err := r.pool.QueryRow(ctx, `
		INSERT INTO strategy_versions
			(id, tenant_id, strategy_id, version_no, schema_version, rules, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING created_at`,
		version.ID, version.TenantID, version.StrategyID, version.VersionNo,
		version.SchemaVersion, raw, version.CreatedBy,
	).Scan(&version.CreatedAt); err != nil {
		return nil, err
	}
	if err := (&StrategyRepo{pool: r.pool}).SetCurrentVersion(ctx, tenantID, strategyID, version.ID); err != nil {
		return nil, err
	}
	return version, nil
}

func (r *StrategyVersionRepo) ListByStrategy(ctx context.Context, tenantID, strategyID uuid.UUID) ([]*strategy.Version, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, strategy_id, version_no, schema_version,
		       rules, created_by, created_at
		FROM strategy_versions
		WHERE tenant_id = $1 AND strategy_id = $2
		ORDER BY version_no DESC`, tenantID, strategyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]*strategy.Version, 0)
	for rows.Next() {
		item, err := scanStrategyVersion(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *StrategyVersionRepo) GetByIDByTenant(ctx context.Context, tenantID, id uuid.UUID) (*strategy.Version, error) {
	return scanStrategyVersion(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, strategy_id, version_no, schema_version,
		       rules, created_by, created_at
		FROM strategy_versions
		WHERE id = $1 AND tenant_id = $2`, id, tenantID))
}

func scanBehaviorProfile(s interface{ Scan(...any) error }) (*behavior.Profile, error) {
	var profile behavior.Profile
	var status string
	var raw []byte
	if err := s.Scan(
		&profile.ID, &profile.TenantID, &profile.TradingAccountID,
		&profile.WindowStart, &profile.WindowEnd, &profile.AlgorithmVersion,
		&status, &profile.SampleSize, &raw, &profile.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	profile.Status = behavior.ProfileStatus(status)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &profile.Metrics); err != nil {
			return nil, err
		}
	}
	return &profile, nil
}

func scanStrategy(s interface{ Scan(...any) error }) (*strategy.Strategy, error) {
	var item strategy.Strategy
	var status string
	if err := s.Scan(
		&item.ID, &item.TenantID, &item.OwnerUserID, &item.Name,
		&item.Description, &status, &item.CurrentVersionID,
		&item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	item.Status = strategy.Status(status)
	return &item, nil
}

func scanStrategyVersion(s interface{ Scan(...any) error }) (*strategy.Version, error) {
	var item strategy.Version
	var raw []byte
	if err := s.Scan(
		&item.ID, &item.TenantID, &item.StrategyID, &item.VersionNo,
		&item.SchemaVersion, &raw, &item.CreatedBy, &item.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	rules, err := strategy.ValidateRuleSet(raw)
	if err != nil {
		return nil, err
	}
	item.Rules = rules
	return &item, nil
}
