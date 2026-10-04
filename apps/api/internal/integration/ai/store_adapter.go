package ai

import (
	"context"
	"time"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/ai"
	"github.com/trading-journal/api/internal/domain/trade"
	"github.com/trading-journal/api/internal/store"
)

// StoreAdapter wires the public *store.Store to the Service contract
// (AnalysisStore + TradeLoader). This is the production binding.
type StoreAdapter struct {
	S *store.Store
}

// NewStoreAdapter builds the adapter.
func NewStoreAdapter(s *store.Store) *StoreAdapter { return &StoreAdapter{S: s} }

// AnalysisStore implementation --------------------------------------------------

func (a *StoreAdapter) Create(ctx context.Context, x *ai.Analysis) error {
	return a.S.TradeAnalyses.Create(ctx, x)
}
func (a *StoreAdapter) GetByID(ctx context.Context, id uuid.UUID) (*ai.Analysis, error) {
	return a.S.TradeAnalyses.GetByID(ctx, id)
}
func (a *StoreAdapter) GetLatestForTrade(ctx context.Context, tenantID, tradeID uuid.UUID) (*ai.Analysis, error) {
	return a.S.TradeAnalyses.GetLatestForTrade(ctx, tenantID, tradeID)
}
func (a *StoreAdapter) FindRecentWithHash(ctx context.Context, tenantID, tradeID uuid.UUID, hash string, sinceMinutes int) (*ai.Analysis, error) {
	return a.S.TradeAnalyses.FindRecentWithHash(ctx, tenantID, tradeID, hash, sinceMinutes)
}
func (a *StoreAdapter) MarkGenerating(ctx context.Context, id uuid.UUID) error {
	return a.S.TradeAnalyses.MarkGenerating(ctx, id)
}
func (a *StoreAdapter) MarkSuccess(ctx context.Context, id uuid.UUID, content string, tokensIn, tokensOut int) error {
	return a.S.TradeAnalyses.MarkSuccess(ctx, id, content, tokensIn, tokensOut)
}
func (a *StoreAdapter) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string, tokensIn int) error {
	return a.S.TradeAnalyses.MarkFailed(ctx, id, errMsg, tokensIn)
}
func (a *StoreAdapter) IncrementRequests(ctx context.Context, tenantID uuid.UUID, day time.Time) (*ai.DailyUsage, error) {
	return a.S.AIUsage.IncrementRequests(ctx, tenantID, day)
}
func (a *StoreAdapter) AddTokens(ctx context.Context, tenantID uuid.UUID, day time.Time, tokensIn, tokensOut int64) error {
	return a.S.AIUsage.AddTokens(ctx, tenantID, day, tokensIn, tokensOut)
}
func (a *StoreAdapter) GetForDay(ctx context.Context, tenantID uuid.UUID, day time.Time) (*ai.DailyUsage, error) {
	return a.S.AIUsage.GetForDay(ctx, tenantID, day)
}

// TradeLoader implementation ---------------------------------------------------

// LoadTradeForAnalysis loads the trade scoped by tenant. Returns
// ErrNoSuchTrade if the row exists in a different tenant.
func (a *StoreAdapter) LoadTradeForAnalysis(ctx context.Context, tenantID, tradeID uuid.UUID) (*trade.Trade, error) {
	t, err := a.S.Trades.GetByID(ctx, tradeID)
	if err != nil {
		return nil, ErrNoSuchTrade
	}
	if t.TenantID != tenantID {
		return nil, ErrNoSuchTrade
	}
	return t, nil
}

// LoadRecentTrades returns up to n trades for the account (excluding
// the trade being analyzed). Simple implementation: fetch the last N+1
// from the same account, drop the analyzed one.
func (a *StoreAdapter) LoadRecentTrades(ctx context.Context, tenantID, tradingAccountID uuid.UUID, n int) ([]*trade.Trade, error) {
	rows, err := a.S.Trades.ListByTenant(ctx, tenantID, &tradingAccountID, n+1, 0)
	if err != nil {
		return nil, err
	}
	out := make([]*trade.Trade, 0, n)
	for _, t := range rows {
		if t.TenantID != tenantID {
			continue
		}
		if len(out) >= n {
			break
		}
		out = append(out, t)
	}
	return out, nil
}

// AccountStats30d returns a 30-day account summary.
// Phase 6 ships a minimal implementation that the FE can interpret;
// richer stats (per symbol, per session) are deferred.
func (a *StoreAdapter) AccountStats30d(ctx context.Context, tenantID, tradingAccountID uuid.UUID) (AccountStats, error) {
	rows, err := a.S.Trades.ListByTenant(ctx, tenantID, &tradingAccountID, 500, 0)
	if err != nil {
		return AccountStats{}, err
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -30)
	wins, total, netSum, holdSum := 0, 0, int64(0), int64(0)
	for _, t := range rows {
		if t.TenantID != tenantID {
			continue
		}
		if t.OpenTime.Before(cutoff) {
			continue
		}
		total++
		if t.Outcome == trade.OutcomeWin {
			wins++
		}
		netSum += int64(t.NetPL * 100) // store as cents (minor units)
		if t.HoldingSeconds != nil {
			holdSum += *t.HoldingSeconds / 60
		}
	}
	var winRate float64
	if total > 0 {
		winRate = float64(wins) / float64(total)
	}
	var avgNet int64
	if total > 0 {
		avgNet = netSum / int64(total)
	}
	var avgHold int
	if total > 0 {
		avgHold = int(holdSum / int64(total))
	}
	return AccountStats{
		WindowDays:     30,
		TotalTrades:    total,
		WinRate:        winRate,
		AvgNetCents:    avgNet,
		AvgHoldMinutes: avgHold,
	}, nil
}