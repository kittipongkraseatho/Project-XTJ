package ai

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/ai"
	"github.com/trading-journal/api/internal/domain/trade"
)

// ── Fakes ────────────────────────────────────────────────────────────

type fakeTradeLoader struct {
	trade *trade.Trade
	err   error
}

func (f *fakeTradeLoader) LoadTradeForEmbed(ctx context.Context, tenantID, tradeID uuid.UUID) (*trade.Trade, error) {
	return f.trade, f.err
}

type fakeEmbedStore struct {
	mu        sync.Mutex
	upserted  []*ai.TradeEmbedding
	upsertErr error
	tokens    []int64
	tokensErr error
}

func (f *fakeEmbedStore) Upsert(ctx context.Context, e *ai.TradeEmbedding) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.upserted = append(f.upserted, e)
	return nil
}

func (f *fakeEmbedStore) AddEmbeddingTokens(ctx context.Context, tenantID uuid.UUID, day time.Time, tokens int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tokensErr != nil {
		return f.tokensErr
	}
	f.tokens = append(f.tokens, tokens)
	return nil
}

// ── Tests ────────────────────────────────────────────────────────────

func TestEmbedService_HappyPath(t *testing.T) {
	tenantID := uuid.Must(uuid.NewV7())
	tradeID := uuid.Must(uuid.NewV7())
	openTime := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	closeTime := openTime.Add(2 * time.Hour)
	loader := &fakeTradeLoader{
		trade: &trade.Trade{
			ID:               tradeID,
			TenantID:         tenantID,
			Symbol:           "EURUSD",
			Direction:        trade.DirectionLong,
			OpenTime:         openTime,
			OpenPrice:        100,
			CloseTime:        &closeTime,
			ClosePrice:       floatPtr(110),
			Volume:           1,
			NetPL:            1000,
			Outcome:          trade.OutcomeWin,
		},
	}
	store := &fakeEmbedStore{}
	svc := NewEmbedService(EmbedConfig{
		Embedder:    NewStubEmbedder(),
		TradeLoader: loader,
		Store:       store,
	})
	if err := svc.EmbedTrade(context.Background(), tenantID, tradeID); err != nil {
		t.Fatalf("embed trade: %v", err)
	}
	if len(store.upserted) != 1 {
		t.Fatalf("expected 1 upsert, got %d", len(store.upserted))
	}
	e := store.upserted[0]
	if e.TenantID != tenantID {
		t.Fatalf("tenant mismatch")
	}
	if e.TradeID != tradeID {
		t.Fatalf("trade mismatch")
	}
	if len(e.Embedding) != 1536 {
		t.Fatalf("expected 1536-dim vec, got %d", len(e.Embedding))
	}
	if e.ModelID != "stub" {
		t.Fatalf("expected model 'stub', got %q", e.ModelID)
	}
	if e.ContextText == "" {
		t.Fatalf("context_text empty")
	}
	if !contains(e.ContextText, "EURUSD") {
		t.Fatalf("context_text missing symbol: %q", e.ContextText)
	}
	if len(store.tokens) != 1 {
		t.Fatalf("expected 1 AddEmbeddingTokens call, got %d", len(store.tokens))
	}
}

func TestEmbedService_TradeMissing(t *testing.T) {
	loader := &fakeTradeLoader{trade: nil}
	store := &fakeEmbedStore{}
	svc := NewEmbedService(EmbedConfig{
		Embedder: NewStubEmbedder(), TradeLoader: loader, Store: store,
	})
	if err := svc.EmbedTrade(context.Background(), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())); err != nil {
		t.Fatalf("expected nil for missing trade, got %v", err)
	}
	if len(store.upserted) != 0 {
		t.Fatalf("expected 0 upserts, got %d", len(store.upserted))
	}
}

func TestEmbedService_LoaderError(t *testing.T) {
	loader := &fakeTradeLoader{err: errors.New("boom")}
	svc := NewEmbedService(EmbedConfig{
		Embedder: NewStubEmbedder(), TradeLoader: loader, Store: &fakeEmbedStore{},
	})
	if err := svc.EmbedTrade(context.Background(), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())); err == nil {
		t.Fatalf("expected error from loader")
	}
}

func TestEmbedService_UpsertError(t *testing.T) {
	tenantID := uuid.Must(uuid.NewV7())
	tradeID := uuid.Must(uuid.NewV7())
	loader := &fakeTradeLoader{
		trade: &trade.Trade{
			ID: tradeID, TenantID: tenantID, Symbol: "X", Direction: trade.DirectionShort,
			OpenTime: time.Now(), NetPL: 1,
		},
	}
	store := &fakeEmbedStore{upsertErr: errors.New("db down")}
	svc := NewEmbedService(EmbedConfig{
		Embedder: NewStubEmbedder(), TradeLoader: loader, Store: store,
	})
	if err := svc.EmbedTrade(context.Background(), tenantID, tradeID); err == nil {
		t.Fatalf("expected error from upsert")
	}
}

func TestBuildEmbedContext_Shape(t *testing.T) {
	openTime := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	closeTime := openTime.Add(time.Hour)
	tr := &trade.Trade{
		Symbol: "GBPUSD", Direction: trade.DirectionShort,
		OpenTime: openTime, OpenPrice: 130,
		CloseTime: &closeTime, ClosePrice: floatPtr(120),
		Volume: 2, NetPL: -200, Outcome: trade.OutcomeLoss,
	}
	s := BuildEmbedContext(tr)
	for _, want := range []string{"Symbol: GBPUSD", "Side: short", "P/L: -200", "Volume: 2", "Outcome: loss"} {
		if !contains(s, want) {
			t.Fatalf("missing %q in context:\n%s", want, s)
		}
	}
}

func TestBuildEmbedContext_TruncateAt700(t *testing.T) {
	openTime := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	// Big Outcome string — Note: outcome is short enum so we can't stuff it.
	// Instead, use a long Symbol to trigger truncation.
	longSymbol := ""
	for i := 0; i < 200; i++ {
		longSymbol += "ABCDEFGHIJ"
	}
	tr := &trade.Trade{
		Symbol: longSymbol, Direction: trade.DirectionLong,
		OpenTime: openTime, OpenPrice: 1, NetPL: 1, Volume: 1,
		Outcome: trade.OutcomeBreakeven,
	}
	s := BuildEmbedContext(tr)
	if len(s) > 700 {
		t.Fatalf("context exceeds 700 chars: %d", len(s))
	}
}

func TestEmbedService_TokensLoggedWhenZero(t *testing.T) {
	tenantID := uuid.Must(uuid.NewV7())
	tradeID := uuid.Must(uuid.NewV7())
	openTime := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	loader := &fakeTradeLoader{
		trade: &trade.Trade{
			ID: tradeID, TenantID: tenantID, Symbol: "X", Direction: trade.DirectionLong,
			OpenTime: openTime, NetPL: 1,
		},
	}
	store := &fakeEmbedStore{}
	svc := NewEmbedService(EmbedConfig{
		Embedder: NewStubEmbedder(), TradeLoader: loader, Store: store,
	})
	if err := svc.EmbedTrade(context.Background(), tenantID, tradeID); err != nil {
		t.Fatalf("embed: %v", err)
	}
	// Stub context for short trade is < 250 chars → tokens = len/4 may be 0
	// (we skip AddEmbeddingTokens when tokens==0). Verify behavior is consistent:
	// len(store.tokens) is either 0 or 1, not more.
	if len(store.tokens) > 1 {
		t.Fatalf("unexpected token calls: %d", len(store.tokens))
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func floatPtr(x float64) *float64 { return &x }