// Package ai — RecapService orchestrates the weekly-recap flow (Phase 9).
//
// Lifecycle:
//
//	HTTP POST /v1/accounts/:id/recaps { week_start? }
//	  → service.RecapAccount(ctx, {tenant, account, user, week_start?})
//	    → compute week_start (default: last completed week's Monday UTC)
//	    → load week's trades + compute stats
//	    → compute prompt_hash from (account, week, trade_signature, model_id)
//	    → if a successful recap exists with same hash within CacheTTL → return cached
//	    → enforce daily budget (recap_requests counter) → 402 if exhausted
//	    → insert ai_recaps row (status=pending) + bump recap_requests
//	    → enqueue RecapJob on worker.Pool → returns immediately
//	  → return the pending row; FE polls GET /v1/recaps/:id
package ai

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/ai"
	"github.com/trading-journal/api/internal/domain/trade"
)

// RecapCacheTTL is how long a successful recap is considered fresh.
const RecapCacheTTL = 30 * time.Minute

// RecapService is the orchestration contract.
type RecapService interface {
	// RecapAccount kicks off a recap for the given week (or last completed week
	// when weekStart is zero). Returns the row (or a cached success row when
	// prompt_hash matches a successful prior recap within RecapCacheTTL).
	RecapAccount(ctx context.Context, in RecapInput) (*ai.Recap, error)

	// GetRecap returns the recap row.
	GetRecap(ctx context.Context, tenantID, recapID uuid.UUID) (*ai.Recap, error)

	// ListRecapsForAccount returns recent recaps for an account (newest first).
	ListRecapsForAccount(ctx context.Context, tenantID, accountID uuid.UUID, limit int) ([]*ai.Recap, error)

	// BuildRecap is the worker handler — invoked by RecapJobPayload.
	BuildRecap(ctx context.Context, recapID uuid.UUID) error
}

// RecapInput is the input to RecapAccount.
type RecapInput struct {
	TenantID  uuid.UUID
	AccountID uuid.UUID
	UserID    uuid.UUID
	WeekStart time.Time // optional; zero → last completed week's Monday UTC
	PlanCode  string    // for budget
}

// RecapStore is the persistence contract.
type RecapStore interface {
	Create(ctx context.Context, r *ai.Recap) error
	GetByID(ctx context.Context, id uuid.UUID) (*ai.Recap, error)
	ListByAccount(ctx context.Context, tenantID, accountID uuid.UUID, limit int) ([]*ai.Recap, error)
	FindRecentWithHash(ctx context.Context, tenantID, accountID uuid.UUID, weekStart time.Time, hash string, sinceMinutes int) (*ai.Recap, error)
	MarkGenerating(ctx context.Context, id uuid.UUID) error
	MarkSuccess(ctx context.Context, id uuid.UUID, content string, tokensIn, tokensOut int) error
	MarkFailed(ctx context.Context, id uuid.UUID, errMsg string, tokensIn int) error
	IncrementRecapRequests(ctx context.Context, tenantID uuid.UUID, day time.Time) error
	AddTokens(ctx context.Context, tenantID uuid.UUID, day time.Time, tokensIn, tokensOut int64) error
	GetForDay(ctx context.Context, tenantID uuid.UUID, day time.Time) (*ai.DailyUsage, error)
}

// RecapTradeLoader reads trades + stats for the week.
type RecapTradeLoader interface {
	LoadWeekTrades(ctx context.Context, tenantID, accountID uuid.UUID, from, to time.Time) ([]*trade.Trade, error)
}

// RecapServiceImpl is the production implementation.
type RecapServiceImpl struct {
	store        RecapStore
	loader       RecapTradeLoader
	provider     Provider
	promptFn     RecapPromptBuilder
	maxTokens    int
	enqueueRecap func(recapID uuid.UUID) error
	log          *slog.Logger
}

// RecapPromptBuilder converts week context into (system, user) prompts.
type RecapPromptBuilder func(ctx RecapWeekContext) (system, user string)

// RecapWeekContext is the input to the prompt builder.
type RecapWeekContext struct {
	AccountName string
	WeekStart   time.Time
	WeekEnd     time.Time
	Trades      []*trade.Trade
	Stats       RecapStats
}

// RecapStats is the summary shown to the LLM.
type RecapStats struct {
	TotalTrades   int
	Wins          int
	Losses        int
	WinRate       float64 // 0..1
	TotalNetCents int64
	BestTrade     *trade.Trade
	WorstTrade    *trade.Trade
}

// RecapConfig wires the service.
type RecapConfig struct {
	Store         RecapStore
	Loader        RecapTradeLoader
	Provider      Provider
	PromptFn      RecapPromptBuilder // optional; default uses DefaultRecapPromptBuilder
	MaxTokens     int                // default 800
	Log           *slog.Logger
	EnqueueRecap  func(recapID uuid.UUID) error
}

// NewRecapService builds the orchestrator.
func NewRecapService(cfg RecapConfig) *RecapServiceImpl {
	if cfg.PromptFn == nil {
		cfg.PromptFn = DefaultRecapPromptBuilder
	}
	if cfg.MaxTokens == 0 {
		cfg.MaxTokens = 800
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &RecapServiceImpl{
		store:        cfg.Store,
		loader:       cfg.Loader,
		provider:     cfg.Provider,
		promptFn:    cfg.PromptFn,
		maxTokens:    cfg.MaxTokens,
		enqueueRecap: cfg.EnqueueRecap,
		log:          cfg.Log,
	}
}

// SetEnqueueRecap wires the worker-pool callback after construction.
func (s *RecapServiceImpl) SetEnqueueRecap(fn func(recapID uuid.UUID) error) {
	s.enqueueRecap = fn
}

// RecapAccount implements RecapService.
func (s *RecapServiceImpl) RecapAccount(ctx context.Context, in RecapInput) (*ai.Recap, error) {
	weekStart, weekEnd := normalizeWeek(in.WeekStart)

	// 1. Load week's trades.
	trades, err := s.loader.LoadWeekTrades(ctx, in.TenantID, in.AccountID, weekStart, weekEnd)
	if err != nil {
		return nil, fmt.Errorf("load week trades: %w", err)
	}

	// 2. Compute prompt hash (stats are recomputed in BuildRecap from fresh data).
	hash := computeRecapHash(in.AccountID, weekStart, trades, s.provider.Name())

	// 4. Idempotent cache hit.
	if existing, err := s.store.FindRecentWithHash(ctx, in.TenantID, in.AccountID, weekStart, hash, int(RecapCacheTTL.Minutes())); err != nil {
		return nil, err
	} else if existing != nil {
		return existing, nil
	}

	// 5. Enforce daily budget.
	day := time.Now().UTC().Truncate(24 * time.Hour)
	u, err := s.store.GetForDay(ctx, in.TenantID, day)
	if err != nil {
		return nil, fmt.Errorf("get usage: %w", err)
	}
	u.DailyBudget = ai.BudgetForPlan(in.PlanCode)
	if ai.BudgetHeadroom(*u) <= 0 {
		return nil, ErrBudgetExhausted
	}

	// 6. Insert pending row.
	r := &ai.Recap{
		ID:                uuid.Must(uuid.NewV7()),
		TenantID:          in.TenantID,
		AccountID:         in.AccountID,
		WeekStart:         weekStart,
		WeekEnd:           weekEnd,
		PromptHash:        hash,
		ModelID:           s.providerModel(),
		Provider:          s.provider.Name(),
		Status:            ai.StatusPending,
		RequestedByUserID: in.UserID,
		RequestedAt:       time.Now().UTC(),
	}
	if err := s.store.Create(ctx, r); err != nil {
		return nil, fmt.Errorf("create recap row: %w", err)
	}

	// 7. Increment recap counter.
	if err := s.store.IncrementRecapRequests(ctx, in.TenantID, day); err != nil {
		s.log.Warn("increment recap usage failed",
			slog.String("err", err.Error()),
			slog.String("tenant_id", in.TenantID.String()))
	}

	// 8. Enqueue worker job (best-effort).
	if s.enqueueRecap != nil {
		if err := s.enqueueRecap(r.ID); err != nil {
			s.log.Warn("enqueue recap failed",
				slog.String("err", err.Error()),
				slog.String("recap_id", r.ID.String()))
		}
	}

	return r, nil
}

// GetRecap implements RecapService.
func (s *RecapServiceImpl) GetRecap(ctx context.Context, tenantID, recapID uuid.UUID) (*ai.Recap, error) {
	r, err := s.store.GetByID(ctx, recapID)
	if err != nil {
		return nil, err
	}
	if r.TenantID != tenantID {
		return nil, ErrRecapNotFound
	}
	return r, nil
}

// ListRecapsForAccount implements RecapService.
func (s *RecapServiceImpl) ListRecapsForAccount(ctx context.Context, tenantID, accountID uuid.UUID, limit int) ([]*ai.Recap, error) {
	return s.store.ListByAccount(ctx, tenantID, accountID, limit)
}

// BuildRecap is the worker handler (called by RecapJobPayload).
func (s *RecapServiceImpl) BuildRecap(ctx context.Context, recapID uuid.UUID) error {
	r, err := s.store.GetByID(ctx, recapID)
	if err != nil {
		return fmt.Errorf("load recap: %w", err)
	}
	if r.Status == ai.StatusSuccess {
		return nil // already done
	}
	if err := s.store.MarkGenerating(ctx, recapID); err != nil {
		return fmt.Errorf("mark generating: %w", err)
	}

	// Re-compute prompt (re-running the same prompt should be idempotent).
	trades, err := s.loader.LoadWeekTrades(ctx, r.TenantID, r.AccountID, r.WeekStart, r.WeekEnd)
	if err != nil {
		_ = s.store.MarkFailed(ctx, recapID, "load week trades: "+err.Error(), 0)
		return fmt.Errorf("load week trades: %w", err)
	}
	stats := computeWeekStats(trades)
	accountName := "" // populated by loader if available; we keep blank for now

	system, user := s.promptFn(RecapWeekContext{
		AccountName: accountName,
		WeekStart:   r.WeekStart,
		WeekEnd:     r.WeekEnd,
		Trades:      trades,
		Stats:       stats,
	})

	res, err := s.provider.Generate(ctx, GenerateInput{
		SystemPrompt: system,
		UserPrompt:   user,
		MaxTokens:    s.maxTokens,
	})
	if err != nil {
		_ = s.store.MarkFailed(ctx, recapID, "generate: "+err.Error(), 0)
		return fmt.Errorf("generate: %w", err)
	}

	if err := s.store.MarkSuccess(ctx, recapID, res.Text, res.TokensIn, res.TokensOut); err != nil {
		return fmt.Errorf("mark success: %w", err)
	}
	day := time.Now().UTC().Truncate(24 * time.Hour)
	if err := s.store.AddTokens(ctx, r.TenantID, day, int64(res.TokensIn), int64(res.TokensOut)); err != nil {
		s.log.Warn("add recap tokens failed",
			slog.String("err", err.Error()),
			slog.String("tenant_id", r.TenantID.String()))
	}
	return nil
}

// providerModel returns the model id stubbed to the provider name + version.
func (s *RecapServiceImpl) providerModel() string {
	// Same heuristic as Phase 6 — model id is tracked per-call by GenerateResult
	// but at kick-off we don't have it yet; use provider name + version hint.
	return s.provider.Name() + "-default"
}

// ── Helpers ──────────────────────────────────────────────────────────

// ErrRecapNotFound is returned when the recap doesn't belong to the tenant.
var ErrRecapNotFound = errors.New("ai: recap not found")

// normalizeWeek returns the Monday-UTC → Sunday-UTC range for the given
// timestamp. When ts is zero, returns the *last completed* week.
func normalizeWeek(ts time.Time) (start, end time.Time) {
	if ts.IsZero() {
		now := time.Now().UTC()
		// Last completed week: find this week's Monday and step back 7 days.
		thisMonday := mondayOf(now)
		start = thisMonday.AddDate(0, 0, -7)
		end = start.AddDate(0, 0, 6) // Sunday
		return
	}
	start = mondayOf(ts)
	end = start.AddDate(0, 0, 6)
	return
}

// mondayOf returns the Monday of the week containing t (UTC).
func mondayOf(t time.Time) time.Time {
	t = t.UTC()
	d := int(t.Weekday()) // Sunday=0, Monday=1, ..., Saturday=6
	if d == 0 {
		d = 7
	}
	delta := d - 1 // days since Monday
	return time.Date(t.Year(), t.Month(), t.Day()-delta, 0, 0, 0, 0, time.UTC)
}

// computeWeekStats summarizes the week's trades.
func computeWeekStats(trades []*trade.Trade) RecapStats {
	stats := RecapStats{TotalTrades: len(trades)}
	for _, t := range trades {
		switch t.Outcome {
		case trade.OutcomeWin:
			stats.Wins++
		case trade.OutcomeLoss:
			stats.Losses++
		}
		stats.TotalNetCents += int64(t.NetPL)
		if stats.BestTrade == nil || t.NetPL > stats.BestTrade.NetPL {
			stats.BestTrade = t
		}
		if stats.WorstTrade == nil || t.NetPL < stats.WorstTrade.NetPL {
			stats.WorstTrade = t
		}
	}
	if stats.TotalTrades > 0 {
		stats.WinRate = float64(stats.Wins) / float64(stats.TotalTrades)
	}
	return stats
}

// computeRecapHash is SHA256(account_id + week_start + sorted(trade_id)[:20] + model_id).
func computeRecapHash(accountID uuid.UUID, weekStart time.Time, trades []*trade.Trade, modelID string) string {
	ids := make([]string, 0, len(trades))
	for _, t := range trades {
		ids = append(ids, t.ID.String())
	}
	sort.Strings(ids)
	if len(ids) > 20 {
		ids = ids[:20]
	}
	h := sha256.New()
	h.Write([]byte(accountID.String()))
	h.Write([]byte{'|'})
	h.Write([]byte(weekStart.UTC().Format("2006-01-02")))
	h.Write([]byte{'|'})
	for _, id := range ids {
		h.Write([]byte(id))
		h.Write([]byte{','})
	}
	h.Write([]byte{'|'})
	h.Write([]byte(modelID))
	return hex.EncodeToString(h.Sum(nil))
}

// ── Default prompt ───────────────────────────────────────────────────

// DefaultRecapPromptBuilder renders the recap prompt.
func DefaultRecapPromptBuilder(c RecapWeekContext) (system, user string) {
	system = "You are a trading analyst. Write a concise weekly recap (3-5 paragraphs) " +
		"covering: (1) overall P/L and win rate, (2) notable wins, " +
		"(3) notable losses, (4) patterns observed, (5) one suggestion for next week. " +
		"Be specific with numbers. Reference specific trades when relevant."

	winPct := int(c.Stats.WinRate * 100)
	userLines := []string{
		fmt.Sprintf("# Account: %s", c.AccountName),
		fmt.Sprintf("# Week: %s to %s (%d trades)",
			c.WeekStart.Format("2006-01-02"), c.WeekEnd.Format("2006-01-02"), c.Stats.TotalTrades),
		"",
		"## Stats",
		fmt.Sprintf("Win rate: %d%% (%d wins / %d losses)", winPct, c.Stats.Wins, c.Stats.Losses),
		fmt.Sprintf("Net: %.2f", float64(c.Stats.TotalNetCents)),
	}
	if c.Stats.BestTrade != nil {
		userLines = append(userLines, fmt.Sprintf("Best: %s %s @ %.5g → P/L %.2f",
			c.Stats.BestTrade.Symbol, c.Stats.BestTrade.Direction, c.Stats.BestTrade.OpenPrice, c.Stats.BestTrade.NetPL))
	}
	if c.Stats.WorstTrade != nil {
		userLines = append(userLines, fmt.Sprintf("Worst: %s %s @ %.5g → P/L %.2f",
			c.Stats.WorstTrade.Symbol, c.Stats.WorstTrade.Direction, c.Stats.WorstTrade.OpenPrice, c.Stats.WorstTrade.NetPL))
	}
	userLines = append(userLines, "", fmt.Sprintf("## Trades (%d)", len(c.Trades)))
	// Compact table, oldest → newest, max 50 rows.
	sorted := append([]*trade.Trade(nil), c.Trades...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].OpenTime.Before(sorted[j].OpenTime) })
	maxRows := 50
	for i, t := range sorted {
		if i >= maxRows {
			userLines = append(userLines, "... (truncated)")
			break
		}
		userLines = append(userLines, fmt.Sprintf("- %s %s %s open=%.5g close=%.5g P/L=%.2f outcome=%s",
			t.OpenTime.UTC().Format("01-02 15:04"), t.Symbol, t.Direction,
			t.OpenPrice, closePriceOf(t), t.NetPL, t.Outcome))
	}
	userLines = append(userLines, "", "## Recap")
	user = joinLines(userLines)
	return
}

func closePriceOf(t *trade.Trade) float64 {
	if t.ClosePrice != nil {
		return *t.ClosePrice
	}
	return 0
}

func joinLines(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "\n"
		}
		out += p
	}
	return out
}