// Package ai — ChatService orchestrates the multi-turn Q&A flow (Phase 9).
//
// Lifecycle:
//
//	POST /v1/chat/threads                        → find-or-create thread
//	POST /v1/chat/threads/:id/messages { content }
//	  → 1. insert user message (role=user)
//	  → 2. enqueue ChatJob on pool (caller is returned 202 immediately)
//	  → 3. worker:
//	       a. embed user message → query_vec
//	       b. retrieve top-10 relevant trades via pgvector cosine (RAG)
//	          (or last-10 trades for account when embedder is stub)
//	       c. load last 50 thread messages
//	       d. build chat prompt (system + history + RAG context + question)
//	       e. call provider.Generate
//	       f. insert assistant message with context_trade_ids
//	       g. trim thread to last 50
//	       h. touch thread.last_message_at + bump chat_requests counter
package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/ai"
)

// ChatMaxTurns is the trim threshold — keep last 50 messages per thread.
const ChatMaxTurns = 50

// ChatRAGTopK is the number of retrieved trades fed into the LLM as RAG context.
const ChatRAGTopK = 10

// ChatService is the orchestration contract.
type ChatService interface {
	// Thread ops
	ListThreads(ctx context.Context, tenantID, userID uuid.UUID, limit int) ([]*ai.ChatThread, error)
	GetThread(ctx context.Context, tenantID, threadID uuid.UUID) (*ai.ChatThread, error)
	CreateThread(ctx context.Context, in ChatCreateInput) (*ai.ChatThread, error)
	DeleteThread(ctx context.Context, tenantID, threadID uuid.UUID) error

	// Message ops
	ListMessages(ctx context.Context, tenantID, threadID uuid.UUID, limit int) ([]*ai.ChatMessage, error)
	SendMessage(ctx context.Context, in ChatSendInput) (*ai.ChatMessage, error)

	// Worker handler (called by ChatJobPayload).
	BuildReply(ctx context.Context, userMessageID uuid.UUID) error
}

// ChatCreateInput is the input to CreateThread.
type ChatCreateInput struct {
	TenantID  uuid.UUID
	UserID    uuid.UUID
	AccountID *uuid.UUID // optional scope
	Title     string     // optional; default = empty
}

// ChatSendInput is the input to SendMessage.
type ChatSendInput struct {
	TenantID  uuid.UUID
	UserID    uuid.UUID
	ThreadID  uuid.UUID
	Content   string
	PlanCode  string
}

// ChatStore is the persistence contract.
type ChatStore interface {
	ThreadFindOrCreate(ctx context.Context, tenantID, userID uuid.UUID, accountID *uuid.UUID, title string) (*ai.ChatThread, error)
	ThreadGetByID(ctx context.Context, tenantID, id uuid.UUID) (*ai.ChatThread, error)
	ThreadListByUser(ctx context.Context, tenantID, userID uuid.UUID, limit int) ([]*ai.ChatThread, error)
	ThreadDelete(ctx context.Context, tenantID, id uuid.UUID) error
	ThreadTouchLastMessage(ctx context.Context, id uuid.UUID) error

	MessageInsert(ctx context.Context, m *ai.ChatMessage) error
	MessageListByThread(ctx context.Context, tenantID, threadID uuid.UUID, limit int) ([]*ai.ChatMessage, error)
	MessageGetByID(ctx context.Context, id uuid.UUID) (*ai.ChatMessage, error)
	MessageTrimToLast50(ctx context.Context, tenantID, threadID uuid.UUID) error

	IncrementChatRequests(ctx context.Context, tenantID uuid.UUID, day time.Time) error
	AddTokens(ctx context.Context, tenantID uuid.UUID, day time.Time, tokensIn, tokensOut int64) error
	GetForDay(ctx context.Context, tenantID uuid.UUID, day time.Time) (*ai.DailyUsage, error)
}

// ChatRetriever returns trade IDs + the loaded trade rows for RAG.
type ChatRetriever interface {
	Retrieve(ctx context.Context, tenantID uuid.UUID, accountID *uuid.UUID, userMessage string, topK int) ([]uuid.UUID, error)
}

// ChatServiceImpl is the production implementation.
type ChatServiceImpl struct {
	store        ChatStore
	retriever    ChatRetriever
	provider     Provider
	enqueueReply func(userMessageID uuid.UUID) error
	log          *slog.Logger
}

// ChatConfig wires the service.
type ChatConfig struct {
	Store        ChatStore
	Retriever    ChatRetriever
	Provider     Provider
	Log          *slog.Logger
	EnqueueReply func(userMessageID uuid.UUID) error
}

// NewChatService builds the orchestrator.
func NewChatService(cfg ChatConfig) *ChatServiceImpl {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	return &ChatServiceImpl{
		store:        cfg.Store,
		retriever:    cfg.Retriever,
		provider:     cfg.Provider,
		enqueueReply: cfg.EnqueueReply,
		log:          cfg.Log,
	}
}

// SetEnqueueReply wires the worker-pool callback after construction.
func (s *ChatServiceImpl) SetEnqueueReply(fn func(userMessageID uuid.UUID) error) {
	s.enqueueReply = fn
}

// ── Threads ─────────────────────────────────────────────────────────

// ListThreads implements ChatService.
func (s *ChatServiceImpl) ListThreads(ctx context.Context, tenantID, userID uuid.UUID, limit int) ([]*ai.ChatThread, error) {
	return s.store.ThreadListByUser(ctx, tenantID, userID, limit)
}

// GetThread implements ChatService.
func (s *ChatServiceImpl) GetThread(ctx context.Context, tenantID, threadID uuid.UUID) (*ai.ChatThread, error) {
	t, err := s.store.ThreadGetByID(ctx, tenantID, threadID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, ErrChatThreadNotFound
	}
	return t, nil
}

// CreateThread implements ChatService. Find-or-create semantics: when
// accountID is set, returns the existing thread for (user, account);
// otherwise creates a new tenant-wide thread.
func (s *ChatServiceImpl) CreateThread(ctx context.Context, in ChatCreateInput) (*ai.ChatThread, error) {
	if in.Title == "" {
		in.Title = "New chat"
	}
	return s.store.ThreadFindOrCreate(ctx, in.TenantID, in.UserID, in.AccountID, in.Title)
}

// DeleteThread implements ChatService.
func (s *ChatServiceImpl) DeleteThread(ctx context.Context, tenantID, threadID uuid.UUID) error {
	return s.store.ThreadDelete(ctx, tenantID, threadID)
}

// ── Messages ────────────────────────────────────────────────────────

// ListMessages implements ChatService.
func (s *ChatServiceImpl) ListMessages(ctx context.Context, tenantID, threadID uuid.UUID, limit int) ([]*ai.ChatMessage, error) {
	if limit <= 0 {
		limit = ChatMaxTurns
	}
	return s.store.MessageListByThread(ctx, tenantID, threadID, limit)
}

// SendMessage implements ChatService. Inserts the user message + kicks off
// the worker for the assistant reply.
func (s *ChatServiceImpl) SendMessage(ctx context.Context, in ChatSendInput) (*ai.ChatMessage, error) {
	// 1. Verify the thread belongs to the tenant.
	thread, err := s.store.ThreadGetByID(ctx, in.TenantID, in.ThreadID)
	if err != nil {
		return nil, fmt.Errorf("load thread: %w", err)
	}
	if thread == nil || thread.UserID != in.UserID {
		return nil, ErrChatThreadNotFound
	}

	// 2. Enforce daily budget.
	day := time.Now().UTC().Truncate(24 * time.Hour)
	u, err := s.store.GetForDay(ctx, in.TenantID, day)
	if err != nil {
		return nil, fmt.Errorf("get usage: %w", err)
	}
	u.DailyBudget = ai.BudgetForPlan(in.PlanCode)
	if ai.BudgetHeadroom(*u) <= 0 {
		return nil, ErrBudgetExhausted
	}

	// 3. Insert the user message.
	m := &ai.ChatMessage{
		ID:          uuid.Must(uuid.NewV7()),
		TenantID:    in.TenantID,
		ThreadID:    in.ThreadID,
		Role:        ai.RoleUser,
		Content:     strings.TrimSpace(in.Content),
		RequestedAt: time.Now().UTC(),
	}
	if m.Content == "" {
		return nil, errors.New("ai: message content empty")
	}
	if err := s.store.MessageInsert(ctx, m); err != nil {
		return nil, fmt.Errorf("insert user message: %w", err)
	}

	// 4. Bump chat counter (best-effort).
	if err := s.store.IncrementChatRequests(ctx, in.TenantID, day); err != nil {
		s.log.Warn("increment chat usage failed",
			slog.String("err", err.Error()),
			slog.String("tenant_id", in.TenantID.String()))
	}

	// 5. Touch thread.
	if err := s.store.ThreadTouchLastMessage(ctx, in.ThreadID); err != nil {
		s.log.Warn("touch thread last_message_at failed",
			slog.String("err", err.Error()),
			slog.String("thread_id", in.ThreadID.String()))
	}

	// 6. Enqueue worker job (best-effort).
	if s.enqueueReply != nil {
		if err := s.enqueueReply(m.ID); err != nil {
			s.log.Warn("enqueue chat reply failed",
				slog.String("err", err.Error()),
				slog.String("message_id", m.ID.String()))
		}
	}

	return m, nil
}

// BuildReply is the worker handler.
func (s *ChatServiceImpl) BuildReply(ctx context.Context, userMessageID uuid.UUID) error {
	userMsg, err := s.store.MessageGetByID(ctx, userMessageID)
	if err != nil {
		return fmt.Errorf("load user message: %w", err)
	}
	if userMsg == nil || userMsg.Role != ai.RoleUser {
		return errors.New("ai: not a user message")
	}

	// 1. Load the thread to know the account scope.
	thread, err := s.store.ThreadGetByID(ctx, userMsg.TenantID, userMsg.ThreadID)
	if err != nil {
		return fmt.Errorf("load thread: %w", err)
	}
	var accountID *uuid.UUID
	if thread != nil {
		accountID = thread.AccountID
	}

	// 2. RAG retrieval — top-K relevant trade IDs for the question.
	tradeIDs, err := s.retriever.Retrieve(ctx, userMsg.TenantID, accountID, userMsg.Content, ChatRAGTopK)
	if err != nil {
		s.log.Warn("chat rag retrieve failed",
			slog.String("err", err.Error()),
			slog.String("message_id", userMessageID.String()))
		tradeIDs = nil // continue without RAG context
	}

	// 3. Load thread history (last 50 messages, ASC order).
	history, err := s.store.MessageListByThread(ctx, userMsg.TenantID, userMsg.ThreadID, ChatMaxTurns)
	if err != nil {
		return fmt.Errorf("load history: %w", err)
	}

	// 4. Build chat prompt.
	system, userPrompt := buildChatPrompt(history, tradeIDs, userMsg.Content)

	// 5. Call LLM.
	res, err := s.provider.Generate(ctx, GenerateInput{
		SystemPrompt: system,
		UserPrompt:   userPrompt,
		MaxTokens:    600,
	})
	if err != nil {
		// Don't persist failure as a message — the FE polls and sees
		// no assistant reply. The user can retry.
		return fmt.Errorf("generate: %w", err)
	}

	// 6. Insert assistant message.
	reply := &ai.ChatMessage{
		ID:              uuid.Must(uuid.NewV7()),
		TenantID:        userMsg.TenantID,
		ThreadID:        userMsg.ThreadID,
		Role:            ai.RoleAssistant,
		Content:         res.Text,
		TokensIn:        res.TokensIn,
		TokensOut:       res.TokensOut,
		ContextTradeIDs: tradeIDs,
		RequestedAt:     time.Now().UTC(),
	}
	provider := s.provider.Name()
	model := s.providerModelForReply(res, provider)
	reply.ModelID = &model
	reply.Provider = &provider
	if err := s.store.MessageInsert(ctx, reply); err != nil {
		return fmt.Errorf("insert assistant message: %w", err)
	}

	// 7. Trim to last 50 + touch thread.
	if err := s.store.MessageTrimToLast50(ctx, userMsg.TenantID, userMsg.ThreadID); err != nil {
		s.log.Warn("trim chat thread failed",
			slog.String("err", err.Error()),
			slog.String("thread_id", userMsg.ThreadID.String()))
	}
	if err := s.store.ThreadTouchLastMessage(ctx, userMsg.ThreadID); err != nil {
		s.log.Warn("touch thread last_message_at failed",
			slog.String("err", err.Error()),
			slog.String("thread_id", userMsg.ThreadID.String()))
	}

	// 8. Track tokens.
	day := time.Now().UTC().Truncate(24 * time.Hour)
	if err := s.store.AddTokens(ctx, userMsg.TenantID, day, int64(res.TokensIn), int64(res.TokensOut)); err != nil {
		s.log.Warn("add chat tokens failed",
			slog.String("err", err.Error()),
			slog.String("tenant_id", userMsg.TenantID.String()))
	}
	return nil
}

func (s *ChatServiceImpl) providerModelForReply(r *GenerateResult, fallback string) string {
	if r != nil && r.ModelID != "" {
		return r.ModelID
	}
	return fallback + "-default"
}

// ── Prompt builder ───────────────────────────────────────────────────

func buildChatPrompt(history []*ai.ChatMessage, tradeIDs []uuid.UUID, userQuestion string) (system, user string) {
	system = "You are a trading analyst. Answer the user's question using the trade context below. " +
		"Reference specific trades when relevant. Be concise (2-4 paragraphs). " +
		"If the context doesn't contain the answer, say so explicitly."

	var lines []string
	if len(tradeIDs) > 0 {
		lines = append(lines, "# Recent trade context (top-10 most relevant)")
		for _, id := range tradeIDs {
			lines = append(lines, "- trade "+id.String())
		}
		lines = append(lines, "")
	}
	if len(history) > 0 {
		lines = append(lines, "# Conversation history")
		for _, m := range history {
			prefix := "User"
			if m.Role == ai.RoleAssistant {
				prefix = "Assistant"
			}
			lines = append(lines, prefix+": "+m.Content)
		}
		lines = append(lines, "")
	}
	lines = append(lines, "# New question")
	lines = append(lines, userQuestion)
	return system, strings.Join(lines, "\n")
}

// ── Errors ───────────────────────────────────────────────────────────

var ErrChatThreadNotFound = errors.New("ai: chat thread not found")