// Package ai — AnalysisService orchestrates the per-trade analysis flow.
//
// Lifecycle:
//
//	HTTP POST /v1/trades/:id/analyze
//	  → service.Analyze(ctx, {tenant, trade, user})
//	    → load trade + account stats + recent 5 trades
//	    → compute prompt_hash from the input set
//	    → if a successful analysis exists with same hash within the last
//	      CACHE_TTL_MINUTES, return it (idempotent)
//	    → else enforce daily budget; if over, return ErrBudgetExhausted
//	    → insert trade_analyses row (status=pending) + bump ai_usage_daily
//	    → enqueue AnalysisJob on the worker pool (returns immediately;
//	      worker Generate()s → success/failed)
//	  → return the pending row; FE polls /v1/trades/:id/analysis
package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/ai"
	"github.com/trading-journal/api/internal/domain/trade"
)

// CacheTTL is how long a successful analysis is considered fresh.
// Re-clicking "Analyze" within this window returns the cached row.
const CacheTTL = 30 * time.Minute

// JobContextKey is the key the worker uses to find the analysis row.
const JobContextKey = "analysis_id"

// Errors.
var (
	ErrBudgetExhausted = errors.New("ai: daily budget exhausted")
	ErrNoSuchTrade     = errors.New("ai: trade not found")
	ErrProviderMissing = errors.New("ai: provider not configured")
)

// PromptBuilder is the function that turns a TradeContext into the
// (system, user) prompt pair. Tests can override.
type PromptBuilder func(ctx TradeContext) (system, user string)

// TradeContext is the input to the prompt builder.
type TradeContext struct {
	Trade       *trade.Trade
	RecentTrades []*trade.Trade
	AccountStats AccountStats
}

// AccountStats is the summary shown to the LLM as context.
type AccountStats struct {
	WindowDays    int
	TotalTrades   int
	WinRate       float64 // 0..1
	AvgNetCents   int64
	AvgHoldMinutes int
}

// ── Service ──────────────────────────────────────────────────────────

// Service is the orchestration contract.
type Service interface {
	// Analyze returns the analysis row (pending on first call, or a
	// cached success on repeat within CacheTTL). The worker takes over
	// asynchronously — the caller polls GetLatestForTrade.
	Analyze(ctx context.Context, in AnalyzeInput) (*ai.Analysis, error)

	// GetLatestForTrade returns the most recent analysis (any status).
	GetLatestForTrade(ctx context.Context, tenantID, tradeID uuid.UUID) (*ai.Analysis, error)

	// GetDailyUsage returns the tenant's usage + budget headroom.
	GetDailyUsage(ctx context.Context, tenantID uuid.UUID) (*ai.DailyUsage, error)

	// BuildAnalysis runs the prompt + provider. Called by AnalysisWorkerJob
	// (Phase 6 §5). Returns the result for storage.
	BuildAnalysis(ctx context.Context, analysisID uuid.UUID) error
}

// AnalyzeInput is the input to Analyze.
type AnalyzeInput struct {
	TenantID uuid.UUID
	TradeID  uuid.UUID
	UserID   uuid.UUID

	// PlanCode is the tenant's plan ("free" / "pro"). Drives budget.
	PlanCode string
}

// AnalysisStore is the persistence contract the service depends on.
// Hides the public `store.Store` to avoid a circular import risk.
type AnalysisStore interface {
	Create(ctx context.Context, a *ai.Analysis) error
	GetByID(ctx context.Context, id uuid.UUID) (*ai.Analysis, error)
	GetLatestForTrade(ctx context.Context, tenantID, tradeID uuid.UUID) (*ai.Analysis, error)
	FindRecentWithHash(ctx context.Context, tenantID, tradeID uuid.UUID, hash string, sinceMinutes int) (*ai.Analysis, error)
	MarkGenerating(ctx context.Context, id uuid.UUID) error
	MarkSuccess(ctx context.Context, id uuid.UUID, content string, tokensIn, tokensOut int) error
	MarkFailed(ctx context.Context, id uuid.UUID, errMsg string, tokensIn int) error

	IncrementRequests(ctx context.Context, tenantID uuid.UUID, day time.Time) (*ai.DailyUsage, error)
	AddTokens(ctx context.Context, tenantID uuid.UUID, day time.Time, tokensIn, tokensOut int64) error
	GetForDay(ctx context.Context, tenantID uuid.UUID, day time.Time) (*ai.DailyUsage, error)
}

// ServiceImpl is the production implementation.
type ServiceImpl struct {
	store           AnalysisStore
	provider        Provider
	promptFn        PromptBuilder
	maxTokens       int
	enqueueAnalysis func(analysisID uuid.UUID) error
	log             *slog.Logger
}

// Store exposes the underlying AnalysisStore (used by tests that need
// to seed data; production code should not need this).
func (s *ServiceImpl) Store() AnalysisStore { return s.store }

// ServiceConfig configures the service.
type ServiceConfig struct {
	Store    AnalysisStore
	Provider Provider
	PromptFn PromptBuilder // optional; default uses DefaultPromptBuilder
	MaxTokens int          // default 600
	Log       *slog.Logger

	// EnqueueAnalysis is invoked after the pending row is inserted so
	// the worker can pick it up. If nil, the service returns the
	// pending row synchronously and the caller must drive
	// BuildAnalysis themselves (useful in tests).
	EnqueueAnalysis func(analysisID uuid.UUID) error
}

// NewServiceImpl builds the service.
func NewServiceImpl(cfg ServiceConfig) *ServiceImpl {
	if cfg.PromptFn == nil {
		cfg.PromptFn = DefaultPromptBuilder
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 600
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &ServiceImpl{
		store:          cfg.Store,
		provider:       cfg.Provider,
		promptFn:       cfg.PromptFn,
		maxTokens:      cfg.MaxTokens,
		enqueueAnalysis: cfg.EnqueueAnalysis,
		log:            cfg.Log,
	}
}

// Analyze implements Service.
func (s *ServiceImpl) Analyze(ctx context.Context, in AnalyzeInput) (*ai.Analysis, error) {
	// 1. Load the trade.
	t, err := s.loadTrade(ctx, in.TenantID, in.TradeID)
	if err != nil {
		return nil, err
	}

	// 2. Load context (recent trades + account stats).
	recent, stats, err := s.loadContext(ctx, in.TenantID, t)
	if err != nil {
		return nil, fmt.Errorf("load context: %w", err)
	}

	// 3. Compute prompt hash.
	hash := computePromptHash(t, recent, stats, s.provider.Name())

	// 4. Idempotent cache hit.
	if existing, err := s.store.FindRecentWithHash(ctx, in.TenantID, in.TradeID, hash, int(CacheTTL.Minutes())); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}

	// 5. Enforce daily budget.
	budget := ai.BudgetForPlan(in.PlanCode)
	day := time.Now().UTC().Truncate(24 * time.Hour)
	u, err := s.store.GetForDay(ctx, in.TenantID, day)
	if err != nil {
		return nil, fmt.Errorf("get usage: %w", err)
	}
	u.DailyBudget = budget
	if ai.BudgetHeadroom(*u) <= 0 {
		return nil, ErrBudgetExhausted
	}

	// 6. Insert pending row.
	a := &ai.Analysis{
		ID:                uuid.Must(uuid.NewV7()),
		TenantID:          in.TenantID,
		TradeID:           in.TradeID,
		PromptHash:        hash,
		ModelID:           s.providerModel(),
		Provider:          s.provider.Name(),
		Status:            ai.StatusPending,
		RequestedByUserID: in.UserID,
		RequestedAt:       time.Now().UTC(),
	}
	if err := s.store.Create(ctx, a); err != nil {
		return nil, fmt.Errorf("create analysis row: %w", err)
	}

	// 7. Increment usage (atomic).
	if _, err := s.store.IncrementRequests(ctx, in.TenantID, day); err != nil {
		// Non-fatal — log + still proceed (we don't want a counter bug to
		// block legitimate analyses). The next tick will catch up.
		s.log.Warn("increment ai usage failed",
			slog.String("err", err.Error()),
			slog.String("tenant_id", in.TenantID.String()))
	}

	// 8. Enqueue the worker job (best-effort — caller can re-trigger if
	// the worker is down by re-running Analyze; the idempotency layer
	// above will short-circuit).
	if s.enqueueAnalysis != nil {
		if err := s.enqueueAnalysis(a.ID); err != nil {
			s.log.Warn("enqueue analysis failed",
				slog.String("err", err.Error()),
				slog.String("analysis_id", a.ID.String()))
		}
	}

	return a, nil
}

// GetLatestForTrade implements Service.
func (s *ServiceImpl) GetLatestForTrade(ctx context.Context, tenantID, tradeID uuid.UUID) (*ai.Analysis, error) {
	a, err := s.store.GetLatestForTrade(ctx, tenantID, tradeID)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// GetDailyUsage implements Service.
func (s *ServiceImpl) GetDailyUsage(ctx context.Context, tenantID uuid.UUID) (*ai.DailyUsage, error) {
	day := time.Now().UTC().Truncate(24 * time.Hour)
	u, err := s.store.GetForDay(ctx, tenantID, day)
	if err != nil {
		return nil, err
	}
	// We don't know plan code here; caller can pass it in if needed.
	// For now, default to free (caller can read entitlements separately).
	u.DailyBudget = ai.BudgetForPlan("")
	return u, nil
}

// BuildAnalysis is the worker-side implementation: load row, mark
// generating, call provider, store result, mark success/failed.
func (s *ServiceImpl) BuildAnalysis(ctx context.Context, analysisID uuid.UUID) error {
	a, err := s.store.GetByID(ctx, analysisID)
	if err != nil {
		return fmt.Errorf("get analysis: %w", err)
	}
	if a.Status == ai.StatusSuccess {
		// Already done — idempotent (worker retry safety).
		return nil
	}
	if err := s.store.MarkGenerating(ctx, analysisID); err != nil {
		return fmt.Errorf("mark generating: %w", err)
	}

	// Reconstruct context.
	t, err := s.loadTrade(ctx, a.TenantID, a.TradeID)
	if err != nil {
		_ = s.store.MarkFailed(ctx, analysisID, err.Error(), 0)
		return err
	}
	recent, stats, err := s.loadContext(ctx, a.TenantID, t)
	if err != nil {
		_ = s.store.MarkFailed(ctx, analysisID, err.Error(), 0)
		return err
	}

	// Build prompt.
	system, user := s.promptFn(TradeContext{Trade: t, RecentTrades: recent, AccountStats: stats})

	// Call provider.
	res, err := s.provider.Generate(ctx, GenerateInput{
		SystemPrompt: system,
		UserPrompt:   user,
		MaxTokens:    s.maxTokens,
	})
	if err != nil {
		_ = s.store.MarkFailed(ctx, analysisID, err.Error(), 0)
		return err
	}

	// Store result.
	if err := s.store.MarkSuccess(ctx, analysisID, res.Text, res.TokensIn, res.TokensOut); err != nil {
		return fmt.Errorf("mark success: %w", err)
	}

	// Bump token counters.
	day := a.RequestedAt.UTC().Truncate(24 * time.Hour)
	if err := s.store.AddTokens(ctx, a.TenantID, day, int64(res.TokensIn), int64(res.TokensOut)); err != nil {
		s.log.Warn("add tokens failed",
			slog.String("err", err.Error()),
			slog.String("tenant_id", a.TenantID.String()))
	}
	return nil
}

// ── helpers ──────────────────────────────────────────────────────────

// loadTrade / loadContext are stubbed via interfaces to keep this file
// decoupled from the public store. The concrete adapter (in
// service_store.go) wires them up to the real store + repositories.
type TradeLoader interface {
	LoadTradeForAnalysis(ctx context.Context, tenantID, tradeID uuid.UUID) (*trade.Trade, error)
	LoadRecentTrades(ctx context.Context, tenantID, tradingAccountID uuid.UUID, n int) ([]*trade.Trade, error)
	AccountStats30d(ctx context.Context, tenantID, tradingAccountID uuid.UUID) (AccountStats, error)
}

func (s *ServiceImpl) loadTrade(ctx context.Context, tenantID, tradeID uuid.UUID) (*trade.Trade, error) {
	if tld, ok := s.store.(TradeLoader); ok {
		return tld.LoadTradeForAnalysis(ctx, tenantID, tradeID)
	}
	return nil, fmt.Errorf("store does not implement TradeLoader")
}

func (s *ServiceImpl) loadContext(ctx context.Context, tenantID uuid.UUID, t *trade.Trade) ([]*trade.Trade, AccountStats, error) {
	if tld, ok := s.store.(TradeLoader); ok {
		recent, err := tld.LoadRecentTrades(ctx, tenantID, t.TradingAccountID, 5)
		if err != nil {
			return nil, AccountStats{}, err
		}
		stats, err := tld.AccountStats30d(ctx, tenantID, t.TradingAccountID)
		if err != nil {
			return nil, AccountStats{}, err
		}
		return recent, stats, nil
	}
	return nil, AccountStats{}, fmt.Errorf("store does not implement TradeLoader")
}

// providerModel returns the active provider's model ID (for storage).
func (s *ServiceImpl) providerModel() string {
	// Default placeholder until Generate returns the actual model.
	// The real value is overwritten in MarkSuccess via res.ModelID.
	return s.provider.Name() + "-default"
}

// computePromptHash is a content-addressable ID for the input set so that
// re-running on the same input returns the cached result.
func computePromptHash(t *trade.Trade, recent []*trade.Trade, stats AccountStats, provider string) string {
	h := sha256.New()
	h.Write([]byte(provider))
	h.Write([]byte("|"))
	h.Write([]byte(t.ID.String()))
	h.Write([]byte("|"))
	if !t.OpenTime.IsZero() {
		h.Write([]byte(t.OpenTime.Format(time.RFC3339Nano)))
	}
	h.Write([]byte("|"))
	if t.CloseTime != nil {
		h.Write([]byte(t.CloseTime.Format(time.RFC3339Nano)))
	}
	h.Write([]byte("|"))
	h.Write([]byte(fmt.Sprintf("%.4f", t.NetPL)))
	h.Write([]byte(fmt.Sprintf("%.4f", t.Volume)))
	h.Write([]byte(t.Symbol))
	h.Write([]byte(string(t.Direction)))
	for _, r := range recent {
		h.Write([]byte(r.ID.String()))
	}
	h.Write([]byte(fmt.Sprintf("%d/%d/%d/%d",
		stats.WindowDays, int(stats.WinRate*1000), stats.AvgNetCents, stats.AvgHoldMinutes)))
	return hex.EncodeToString(h.Sum(nil))
}

// ── Default prompt builder ──────────────────────────────────────────

// DefaultPromptBuilder returns the (system, user) prompt pair for a
// per-trade commentary. Exposed so it can be referenced by tests.
func DefaultPromptBuilder(ctx TradeContext) (system, user string) {
	system = "You are a trading coach. Analyze the trade below in 2-3 short paragraphs. " +
		"Focus on: (1) what worked, (2) what didn't, (3) one concrete suggestion. " +
		"Be specific. Use the recent trades and account stats to inform your analysis. " +
		"Never recommend specific instruments. Keep it under 200 words."
	var sb strings.Builder
	sb.WriteString("# Trade\n")
	sb.WriteString(fmt.Sprintf("- id: %s\n", ctx.Trade.ID))
	sb.WriteString(fmt.Sprintf("- symbol: %s\n", ctx.Trade.Symbol))
	sb.WriteString(fmt.Sprintf("- direction: %s\n", ctx.Trade.Direction))
	sb.WriteString(fmt.Sprintf("- open_time: %s\n", ctx.Trade.OpenTime.Format(time.RFC3339)))
	if ctx.Trade.CloseTime != nil {
		sb.WriteString(fmt.Sprintf("- close_time: %s\n", ctx.Trade.CloseTime.Format(time.RFC3339)))
	}
	sb.WriteString(fmt.Sprintf("- volume: %.4f\n", ctx.Trade.Volume))
	sb.WriteString(fmt.Sprintf("- net_pl: %.4f\n", ctx.Trade.NetPL))
	if ctx.Trade.Magic != nil {
		sb.WriteString(fmt.Sprintf("- magic: %d\n", *ctx.Trade.Magic))
	}
	if len(ctx.RecentTrades) > 0 {
		sb.WriteString("\n# Recent trades (last " + fmt.Sprintf("%d", len(ctx.RecentTrades)) + ")\n")
		for _, r := range ctx.RecentTrades {
			sb.WriteString(fmt.Sprintf("- %s | %s | %s | %.4f\n",
				r.OpenTime.Format("2006-01-02"), r.Symbol, r.Direction, r.NetPL))
		}
	}
	sb.WriteString("\n# Account stats (last 30d)\n")
	sb.WriteString(fmt.Sprintf("- total_trades: %d\n", ctx.AccountStats.TotalTrades))
	sb.WriteString(fmt.Sprintf("- win_rate: %.1f%%\n", ctx.AccountStats.WinRate*100))
	sb.WriteString(fmt.Sprintf("- avg_net_cents: %d\n", ctx.AccountStats.AvgNetCents))
	sb.WriteString(fmt.Sprintf("- avg_hold_minutes: %d\n", ctx.AccountStats.AvgHoldMinutes))
	sb.WriteString("\n# Analysis\n")
	user = sb.String()
	return system, user
}