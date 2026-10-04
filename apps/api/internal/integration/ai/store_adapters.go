package ai

import (
	"context"
	"time"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/ai"
	"github.com/trading-journal/api/internal/store"
)

// ── RecapStoreAdapter ────────────────────────────────────────────────

// RecapStoreAdapter wires *store.Store into aisvc.RecapStore (Phase 9).
type RecapStoreAdapter struct{ S *store.Store }

// NewRecapStoreAdapter builds the adapter.
func NewRecapStoreAdapter(s *store.Store) *RecapStoreAdapter { return &RecapStoreAdapter{S: s} }

func (a *RecapStoreAdapter) Create(ctx context.Context, r *ai.Recap) error {
	return a.S.Recaps.Create(ctx, r)
}
func (a *RecapStoreAdapter) GetByID(ctx context.Context, id uuid.UUID) (*ai.Recap, error) {
	return a.S.Recaps.GetByID(ctx, id)
}
func (a *RecapStoreAdapter) ListByAccount(ctx context.Context, tenantID, accountID uuid.UUID, limit int) ([]*ai.Recap, error) {
	return a.S.Recaps.ListByAccount(ctx, tenantID, accountID, limit)
}
func (a *RecapStoreAdapter) FindRecentWithHash(ctx context.Context, tenantID, accountID uuid.UUID, weekStart time.Time, hash string, sinceMinutes int) (*ai.Recap, error) {
	return a.S.Recaps.FindRecentWithHash(ctx, tenantID, accountID, weekStart, hash, sinceMinutes)
}
func (a *RecapStoreAdapter) MarkGenerating(ctx context.Context, id uuid.UUID) error {
	return a.S.Recaps.MarkGenerating(ctx, id)
}
func (a *RecapStoreAdapter) MarkSuccess(ctx context.Context, id uuid.UUID, content string, tokensIn, tokensOut int) error {
	return a.S.Recaps.MarkSuccess(ctx, id, content, tokensIn, tokensOut)
}
func (a *RecapStoreAdapter) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string, tokensIn int) error {
	return a.S.Recaps.MarkFailed(ctx, id, errMsg, tokensIn)
}
func (a *RecapStoreAdapter) IncrementRecapRequests(ctx context.Context, tenantID uuid.UUID, day time.Time) error {
	return a.S.AIUsage.IncrementRecapRequests(ctx, tenantID, day)
}
func (a *RecapStoreAdapter) AddTokens(ctx context.Context, tenantID uuid.UUID, day time.Time, tokensIn, tokensOut int64) error {
	return a.S.AIUsage.AddTokens(ctx, tenantID, day, tokensIn, tokensOut)
}
func (a *RecapStoreAdapter) GetForDay(ctx context.Context, tenantID uuid.UUID, day time.Time) (*ai.DailyUsage, error) {
	return a.S.AIUsage.GetForDay(ctx, tenantID, day)
}

// ── ChatStoreAdapter ─────────────────────────────────────────────────

// ChatStoreAdapter wires *store.Store into aisvc.ChatStore (Phase 9).
type ChatStoreAdapter struct{ S *store.Store }

// NewChatStoreAdapter builds the adapter.
func NewChatStoreAdapter(s *store.Store) *ChatStoreAdapter { return &ChatStoreAdapter{S: s} }

func (a *ChatStoreAdapter) ThreadFindOrCreate(ctx context.Context, tenantID, userID uuid.UUID, accountID *uuid.UUID, title string) (*ai.ChatThread, error) {
	return a.S.ChatThreads.FindOrCreate(ctx, tenantID, userID, accountID, title)
}
func (a *ChatStoreAdapter) ThreadGetByID(ctx context.Context, tenantID, id uuid.UUID) (*ai.ChatThread, error) {
	return a.S.ChatThreads.GetByID(ctx, tenantID, id)
}
func (a *ChatStoreAdapter) ThreadListByUser(ctx context.Context, tenantID, userID uuid.UUID, limit int) ([]*ai.ChatThread, error) {
	return a.S.ChatThreads.ListByUser(ctx, tenantID, userID, limit)
}
func (a *ChatStoreAdapter) ThreadDelete(ctx context.Context, tenantID, id uuid.UUID) error {
	return a.S.ChatThreads.Delete(ctx, tenantID, id)
}
func (a *ChatStoreAdapter) ThreadTouchLastMessage(ctx context.Context, id uuid.UUID) error {
	return a.S.ChatThreads.TouchLastMessage(ctx, id)
}
func (a *ChatStoreAdapter) MessageInsert(ctx context.Context, m *ai.ChatMessage) error {
	return a.S.ChatMessages.Insert(ctx, m)
}
func (a *ChatStoreAdapter) MessageListByThread(ctx context.Context, tenantID, threadID uuid.UUID, limit int) ([]*ai.ChatMessage, error) {
	return a.S.ChatMessages.ListByThread(ctx, tenantID, threadID, limit)
}
func (a *ChatStoreAdapter) MessageGetByID(ctx context.Context, id uuid.UUID) (*ai.ChatMessage, error) {
	return a.S.ChatMessages.GetByID(ctx, id)
}
func (a *ChatStoreAdapter) MessageTrimToLast50(ctx context.Context, tenantID, threadID uuid.UUID) error {
	return a.S.ChatMessages.TrimToLast50(ctx, tenantID, threadID)
}
func (a *ChatStoreAdapter) IncrementChatRequests(ctx context.Context, tenantID uuid.UUID, day time.Time) error {
	return a.S.AIUsage.IncrementChatRequests(ctx, tenantID, day)
}
func (a *ChatStoreAdapter) AddTokens(ctx context.Context, tenantID uuid.UUID, day time.Time, tokensIn, tokensOut int64) error {
	return a.S.AIUsage.AddTokens(ctx, tenantID, day, tokensIn, tokensOut)
}
func (a *ChatStoreAdapter) GetForDay(ctx context.Context, tenantID uuid.UUID, day time.Time) (*ai.DailyUsage, error) {
	return a.S.AIUsage.GetForDay(ctx, tenantID, day)
}

// Ensure interfaces are satisfied at compile time.
var (
	_ RecapStore = (*RecapStoreAdapter)(nil)
	_ ChatStore  = (*ChatStoreAdapter)(nil)
)