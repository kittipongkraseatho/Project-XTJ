package worker

import (
	"context"
	"fmt"

	"github.com/gofrs/uuid"
)

// TradeEmbedJobPayload computes (or refreshes) the embedding for one trade
// and persists it to trade_embeddings. Fire-and-forget: failure is logged
// but not retried — the next Phase 1 import or Q&A retrieval will re-embed
// on demand.
type TradeEmbedJobPayload struct {
	TenantID uuid.UUID
	TradeID  uuid.UUID
	Handler  func(ctx context.Context) error
}

// Kind satisfies Job.Kind.
func (p *TradeEmbedJobPayload) Kind() string {
	return "trade_embed:" + p.TradeID.String()
}

// Execute satisfies Job.Execute.
func (p *TradeEmbedJobPayload) Execute(ctx context.Context) error {
	if p.Handler == nil {
		return fmt.Errorf("trade embed handler is nil")
	}
	return p.Handler(ctx)
}

// ── RecapJobPayload ──────────────────────────────────────────────────

// RecapJobPayload generates a single weekly recap for an account.
type RecapJobPayload struct {
	RecapID uuid.UUID
	Handler func(ctx context.Context) error
}

// Kind satisfies Job.Kind.
func (p *RecapJobPayload) Kind() string {
	return "recap:" + p.RecapID.String()
}

// Execute satisfies Job.Execute.
func (p *RecapJobPayload) Execute(ctx context.Context) error {
	if p.Handler == nil {
		return fmt.Errorf("recap job handler is nil")
	}
	return p.Handler(ctx)
}

// ── ChatJobPayload ───────────────────────────────────────────────────

// ChatJobPayload generates one assistant reply in a Q&A thread.
type ChatJobPayload struct {
	MessageID uuid.UUID
	ThreadID uuid.UUID
	Handler  func(ctx context.Context) error
}

// Kind satisfies Job.Kind.
func (p *ChatJobPayload) Kind() string {
	return "chat:" + p.MessageID.String()
}

// Execute satisfies Job.Execute.
func (p *ChatJobPayload) Execute(ctx context.Context) error {
	if p.Handler == nil {
		return fmt.Errorf("chat job handler is nil")
	}
	return p.Handler(ctx)
}