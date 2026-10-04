package ai

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/ai"
	"github.com/trading-journal/api/internal/domain/trade"
)

// EmbedService orchestrates the embed-a-trade flow:
//
//  1. Load trade (tenant-scoped)
//  2. Build context_text (~200 tokens max)
//  3. Call embedder.Embed
//  4. UPSERT into trade_embeddings (UNIQUE on tenant_id+trade_id → idempotent)
//  5. Track embedding_tokens in ai_usage_daily (cheap; no budget deduction)
//
// Failures are logged but not retried — the next Phase 1 import or any
// Q&A retrieval re-embeds on demand.
type EmbedService struct {
	embedder     Embedder
	tradeLoader  EmbedTradeLoader
	store        EmbedStore
	log          *slog.Logger
}

// EmbedTradeLoader is the read-side dependency (trade lookup by ID, scoped).
type EmbedTradeLoader interface {
	LoadTradeForEmbed(ctx context.Context, tenantID, tradeID uuid.UUID) (*trade.Trade, error)
}

// EmbedStore is the persistence side.
type EmbedStore interface {
	Upsert(ctx context.Context, e *ai.TradeEmbedding) error
	AddEmbeddingTokens(ctx context.Context, tenantID uuid.UUID, day time.Time, tokens int64) error
}

// EmbedConfig wires the service.
type EmbedConfig struct {
	Embedder    Embedder
	TradeLoader EmbedTradeLoader
	Store       EmbedStore
	Log         *slog.Logger
}

// NewEmbedService builds the orchestrator.
func NewEmbedService(cfg EmbedConfig) *EmbedService {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &EmbedService{
		embedder:    cfg.Embedder,
		tradeLoader: cfg.TradeLoader,
		store:       cfg.Store,
		log:         cfg.Log,
	}
}

// EmbedTrade loads + embeds + persists one trade. Safe to retry.
func (s *EmbedService) EmbedTrade(ctx context.Context, tenantID, tradeID uuid.UUID) error {
	t, err := s.tradeLoader.LoadTradeForEmbed(ctx, tenantID, tradeID)
	if err != nil {
		return fmt.Errorf("load trade for embed: %w", err)
	}
	if t == nil {
		return nil // trade missing — nothing to do
	}
	text := BuildEmbedContext(t)
	vecs, err := s.embedder.Embed(ctx, []string{text})
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}
	if len(vecs) != 1 {
		return fmt.Errorf("embed: expected 1 vector, got %d", len(vecs))
	}
	e := &ai.TradeEmbedding{
		TenantID:    tenantID,
		TradeID:     tradeID,
		Embedding:   vecs[0],
		ContextText: text,
		ModelID:     s.embedder.Name(),
	}
	if err := s.store.Upsert(ctx, e); err != nil {
		return fmt.Errorf("upsert embedding: %w", err)
	}
	// Track token usage (rough: ~4 chars per token).
	tokens := int64(len(text) / 4)
	if tokens > 0 {
		day := time.Now().UTC().Truncate(24 * time.Hour)
		if err := s.store.AddEmbeddingTokens(ctx, tenantID, day, tokens); err != nil {
			s.log.Warn("track embedding tokens failed",
				slog.String("err", err.Error()),
				slog.String("tenant_id", tenantID.String()))
		}
	}
	return nil
}

// BuildEmbedContext returns the deterministic context_text for embedding a trade.
// Trimmed to ~200 tokens by truncating journal/notes if needed.
func BuildEmbedContext(t *trade.Trade) string {
	// Trade fields (compact, deterministic).
	var b2 []byte
	b2 = append(b2, "Symbol: "...)
	b2 = append(b2, t.Symbol...)
	b2 = append(b2, '\n')
	b2 = append(b2, "Side: "...)
	b2 = append(b2, t.Direction...)
	b2 = append(b2, '\n')
	if !t.OpenTime.IsZero() {
		b2 = append(b2, []byte(fmt.Sprintf("Open: %s UTC @ %g\n",
			t.OpenTime.UTC().Format("2006-01-02 15:04"), t.OpenPrice))...)
	}
	if t.CloseTime != nil && !t.CloseTime.IsZero() {
		b2 = append(b2, []byte(fmt.Sprintf("Close: %s UTC @ %g\n",
			t.CloseTime.UTC().Format("2006-01-02 15:04"), *t.ClosePrice))...)
	}
	b2 = append(b2, []byte(fmt.Sprintf("P/L: %g (currency units)\n", t.NetPL))...)
	if t.Volume != 0 {
		b2 = append(b2, []byte(fmt.Sprintf("Volume: %g\n", t.Volume))...)
	}
	if t.Outcome != "" {
		b2 = append(b2, "Outcome: "...)
		b2 = append(b2, t.Outcome...)
		b2 = append(b2, '\n')
	}
	// Truncate to ~700 chars (~200 tokens).
	const maxLen = 700
	if len(b2) > maxLen {
		b2 = b2[:maxLen]
	}
	return string(b2)
}