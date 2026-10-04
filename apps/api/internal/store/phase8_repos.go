package store

import (
	"context"
	"errors"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/trading-journal/api/internal/domain/ai"
)

// ── TradeAnalysisRepo ────────────────────────────────────────────────

// TradeAnalysisRepo manages trade_analyses rows.
type TradeAnalysisRepo struct {
	pool DBTX
}

func (r *TradeAnalysisRepo) WithTx(tx DBTX) *TradeAnalysisRepo {
	return &TradeAnalysisRepo{pool: tx}
}

// Create inserts a new analysis row (status=pending by default).
func (r *TradeAnalysisRepo) Create(ctx context.Context, a *ai.Analysis) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO trade_analyses
			(id, tenant_id, trade_id, prompt_hash, content, tokens_in, tokens_out,
			 model_id, provider, status, requested_by_user_id, requested_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		a.ID, a.TenantID, a.TradeID, a.PromptHash, a.Content,
		a.TokensIn, a.TokensOut, a.ModelID, a.Provider,
		a.Status, a.RequestedByUserID, a.RequestedAt,
	)
	return err
}

// MarkGenerating transitions pending → generating.
func (r *TradeAnalysisRepo) MarkGenerating(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE trade_analyses
		SET status = 'generating'
		WHERE id = $1 AND status IN ('pending', 'generating')`, id)
	return err
}

// MarkSuccess finalizes a successful generation.
func (r *TradeAnalysisRepo) MarkSuccess(ctx context.Context, id uuid.UUID, content string, tokensIn, tokensOut int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE trade_analyses
		SET status = 'success', content = $1, tokens_in = $2, tokens_out = $3,
		    completed_at = now()
		WHERE id = $4`, content, tokensIn, tokensOut, id)
	return err
}

// MarkFailed records a provider error.
func (r *TradeAnalysisRepo) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string, tokensIn int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE trade_analyses
		SET status = 'failed', error = $1, tokens_in = $2, completed_at = now()
		WHERE id = $3`, errMsg, tokensIn, id)
	return err
}

// GetByID returns one analysis (no tenant scoping — workers may look up
// by id during generation).
func (r *TradeAnalysisRepo) GetByID(ctx context.Context, id uuid.UUID) (*ai.Analysis, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, trade_id, prompt_hash, content, tokens_in, tokens_out,
		       model_id, provider, status, error, requested_by_user_id,
		       requested_at, completed_at
		FROM trade_analyses WHERE id = $1`, id)
	return scanAnalysis(row)
}

// GetLatestForTrade returns the most recent analysis for a trade scoped by tenant.
func (r *TradeAnalysisRepo) GetLatestForTrade(ctx context.Context, tenantID, tradeID uuid.UUID) (*ai.Analysis, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, trade_id, prompt_hash, content, tokens_in, tokens_out,
		       model_id, provider, status, error, requested_by_user_id,
		       requested_at, completed_at
		FROM trade_analyses
		WHERE tenant_id = $1 AND trade_id = $2
		ORDER BY requested_at DESC
		LIMIT 1`, tenantID, tradeID)
	return scanAnalysis(row)
}

// FindRecentWithHash returns the most recent successful analysis for the
// (trade, prompt_hash) tuple — used by the idempotency check.
func (r *TradeAnalysisRepo) FindRecentWithHash(ctx context.Context, tenantID, tradeID uuid.UUID, hash string, sinceMinutes int) (*ai.Analysis, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(sinceMinutes) * time.Minute)
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, trade_id, prompt_hash, content, tokens_in, tokens_out,
		       model_id, provider, status, error, requested_by_user_id,
		       requested_at, completed_at
		FROM trade_analyses
		WHERE tenant_id = $1 AND trade_id = $2 AND prompt_hash = $3
		  AND status = 'success' AND requested_at >= $4
		ORDER BY requested_at DESC
		LIMIT 1`, tenantID, tradeID, hash, cutoff)
	a, err := scanAnalysis(row)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return a, nil
}

// ── AIUsageDailyRepo ─────────────────────────────────────────────────

// AIUsageDailyRepo manages ai_usage_daily rows.
type AIUsageDailyRepo struct {
	pool DBTX
}

func (r *AIUsageDailyRepo) WithTx(tx DBTX) *AIUsageDailyRepo {
	return &AIUsageDailyRepo{pool: tx}
}

// IncrementRequests bumps requests by 1 and returns the new row.
// Uses ON CONFLICT to atomically upsert the day's row.
func (r *AIUsageDailyRepo) IncrementRequests(ctx context.Context, tenantID uuid.UUID, day time.Time) (*ai.DailyUsage, error) {
	id := uuid.Must(uuid.NewV7())
	dayStr := day.UTC().Format("2006-01-02")
	row := r.pool.QueryRow(ctx, `
		INSERT INTO ai_usage_daily (id, tenant_id, day, requests, tokens_in, tokens_out)
		VALUES ($1, $2, $3, 1, 0, 0)
		ON CONFLICT (tenant_id, day) DO UPDATE
		SET requests = ai_usage_daily.requests + 1
		RETURNING requests, tokens_in, tokens_out, day`,
		id, tenantID, dayStr,
	)
	var u ai.DailyUsage
	var dayStrOut string
	if err := row.Scan(&u.Requests, &u.TokensIn, &u.TokensOut, &dayStrOut); err != nil {
		return nil, err
	}
	parsed, _ := time.Parse("2006-01-02", dayStrOut)
	u.Day = parsed
	return &u, nil
}

// AddTokens accumulates token counts.
func (r *AIUsageDailyRepo) AddTokens(ctx context.Context, tenantID uuid.UUID, day time.Time, tokensIn, tokensOut int64) error {
	dayStr := day.UTC().Format("2006-01-02")
	_, err := r.pool.Exec(ctx, `
		UPDATE ai_usage_daily
		SET tokens_in = tokens_in + $1, tokens_out = tokens_out + $2
		WHERE tenant_id = $3 AND day = $4`,
		tokensIn, tokensOut, tenantID, dayStr,
	)
	return err
}

// GetForDay returns the current usage row (zero values if not present).
func (r *AIUsageDailyRepo) GetForDay(ctx context.Context, tenantID uuid.UUID, day time.Time) (*ai.DailyUsage, error) {
	dayStr := day.UTC().Format("2006-01-02")
	row := r.pool.QueryRow(ctx, `
		SELECT requests, tokens_in, tokens_out, recap_requests, chat_requests, embedding_tokens
		FROM ai_usage_daily
		WHERE tenant_id = $1 AND day = $2`, tenantID, dayStr)
	var u ai.DailyUsage
	if err := row.Scan(&u.Requests, &u.TokensIn, &u.TokensOut, &u.RecapRequests, &u.ChatRequests, &u.EmbeddingTokens); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &ai.DailyUsage{Day: day}, nil
		}
		return nil, err
	}
	u.Day = day
	return &u, nil
}

// IncrementRecapRequests bumps recap_requests by 1 (Phase 9 — weekly recap).
func (r *AIUsageDailyRepo) IncrementRecapRequests(ctx context.Context, tenantID uuid.UUID, day time.Time) error {
	id := uuid.Must(uuid.NewV7())
	dayStr := day.UTC().Format("2006-01-02")
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ai_usage_daily (id, tenant_id, day, requests, tokens_in, tokens_out,
		                            chat_requests, recap_requests, embedding_tokens)
		VALUES ($1, $2, $3, 0, 0, 0, 0, 1, 0)
		ON CONFLICT (tenant_id, day) DO UPDATE
		SET recap_requests = ai_usage_daily.recap_requests + 1`,
		id, tenantID, dayStr)
	return err
}

// IncrementChatRequests bumps chat_requests by 1 (Phase 9 — Q&A message kick).
func (r *AIUsageDailyRepo) IncrementChatRequests(ctx context.Context, tenantID uuid.UUID, day time.Time) error {
	id := uuid.Must(uuid.NewV7())
	dayStr := day.UTC().Format("2006-01-02")
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ai_usage_daily (id, tenant_id, day, requests, tokens_in, tokens_out,
		                            chat_requests, recap_requests, embedding_tokens)
		VALUES ($1, $2, $3, 0, 0, 0, 1, 0, 0)
		ON CONFLICT (tenant_id, day) DO UPDATE
		SET chat_requests = ai_usage_daily.chat_requests + 1`,
		id, tenantID, dayStr)
	return err
}

// AddEmbeddingTokens accumulates Phase 9 embedding token usage (cheap; no budget
// deduction — separate counter, tracked for billing visibility only).
func (r *AIUsageDailyRepo) AddEmbeddingTokens(ctx context.Context, tenantID uuid.UUID, day time.Time, tokens int64) error {
	dayStr := day.UTC().Format("2006-01-02")
	_, err := r.pool.Exec(ctx, `
		UPDATE ai_usage_daily
		SET embedding_tokens = embedding_tokens + $1
		WHERE tenant_id = $2 AND day = $3`,
		tokens, tenantID, dayStr)
	return err
}

// ── row scanner ──────────────────────────────────────────────────────

func scanAnalysis(s scanner) (*ai.Analysis, error) {
	var a ai.Analysis
	var content, errMsg *string
	var completedAt *time.Time
	if err := s.Scan(
		&a.ID, &a.TenantID, &a.TradeID, &a.PromptHash, &content,
		&a.TokensIn, &a.TokensOut, &a.ModelID, &a.Provider, &a.Status, &errMsg,
		&a.RequestedByUserID, &a.RequestedAt, &completedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	a.Content = content
	a.Error = errMsg
	a.CompletedAt = completedAt
	return &a, nil
}