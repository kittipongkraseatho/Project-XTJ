package ai

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/ai"
)

// ── Fakes ────────────────────────────────────────────────────────────

type fakeChatStore struct {
	mu          sync.Mutex
	threads     map[uuid.UUID]*ai.ChatThread
	messages    map[uuid.UUID]*ai.ChatMessage
	usage       map[string]*ai.DailyUsage
}

func newFakeChatStore() *fakeChatStore {
	return &fakeChatStore{
		threads:  map[uuid.UUID]*ai.ChatThread{},
		messages: map[uuid.UUID]*ai.ChatMessage{},
		usage:    map[string]*ai.DailyUsage{},
	}
}

func (f *fakeChatStore) ThreadFindOrCreate(ctx context.Context, tenantID, userID uuid.UUID, accountID *uuid.UUID, title string) (*ai.ChatThread, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if accountID != nil {
		for _, t := range f.threads {
			if t.TenantID == tenantID && t.UserID == userID && t.AccountID != nil && *t.AccountID == *accountID {
				t2 := *t
				return &t2, nil
			}
		}
	}
	id := uuid.Must(uuid.NewV7())
	now := time.Now().UTC()
	t := &ai.ChatThread{ID: id, TenantID: tenantID, UserID: userID, AccountID: accountID, Title: title, CreatedAt: now, LastMessageAt: now}
	f.threads[id] = t
	t2 := *t
	return &t2, nil
}

func (f *fakeChatStore) ThreadGetByID(ctx context.Context, tenantID, id uuid.UUID) (*ai.ChatThread, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.threads[id]
	if !ok || t.TenantID != tenantID {
		return nil, nil
	}
	t2 := *t
	return &t2, nil
}

func (f *fakeChatStore) ThreadListByUser(ctx context.Context, tenantID, userID uuid.UUID, limit int) ([]*ai.ChatThread, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []*ai.ChatThread{}
	for _, t := range f.threads {
		if t.TenantID == tenantID && t.UserID == userID {
			t2 := *t
			out = append(out, &t2)
		}
	}
	return out, nil
}

func (f *fakeChatStore) ThreadDelete(ctx context.Context, tenantID, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.threads[id]; ok && t.TenantID == tenantID {
		delete(f.threads, id)
		// Cascade-delete messages.
		for mid, m := range f.messages {
			if m.ThreadID == id {
				delete(f.messages, mid)
			}
		}
	}
	return nil
}

func (f *fakeChatStore) ThreadTouchLastMessage(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if t, ok := f.threads[id]; ok {
		t.LastMessageAt = time.Now().UTC()
	}
	return nil
}

func (f *fakeChatStore) MessageInsert(ctx context.Context, m *ai.ChatMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m2 := *m
	if m2.RequestedAt.IsZero() {
		m2.RequestedAt = time.Now().UTC()
	}
	f.messages[m2.ID] = &m2
	return nil
}

func (f *fakeChatStore) MessageListByThread(ctx context.Context, tenantID, threadID uuid.UUID, limit int) ([]*ai.ChatMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []*ai.ChatMessage{}
	for _, m := range f.messages {
		if m.TenantID == tenantID && m.ThreadID == threadID {
			m2 := *m
			out = append(out, &m2)
		}
	}
	// ASC by time.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].RequestedAt.Before(out[i].RequestedAt) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (f *fakeChatStore) MessageGetByID(ctx context.Context, id uuid.UUID) (*ai.ChatMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.messages[id]
	if !ok {
		return nil, nil
	}
	m2 := *m
	return &m2, nil
}

func (f *fakeChatStore) MessageTrimToLast50(ctx context.Context, tenantID, threadID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := []*ai.ChatMessage{}
	for _, m := range f.messages {
		if m.TenantID == tenantID && m.ThreadID == threadID {
			all = append(all, m)
		}
	}
	if len(all) <= 50 {
		return nil
	}
	// Sort DESC by time.
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].RequestedAt.After(all[i].RequestedAt) {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	for i := 50; i < len(all); i++ {
		delete(f.messages, all[i].ID)
	}
	return nil
}

func (f *fakeChatStore) IncrementChatRequests(ctx context.Context, tenantID uuid.UUID, day time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.usage[tenantID.String()]
	if !ok {
		u = &ai.DailyUsage{Day: day}
		f.usage[tenantID.String()] = u
	}
	u.ChatRequests++
	return nil
}

func (f *fakeChatStore) AddTokens(ctx context.Context, tenantID uuid.UUID, day time.Time, tokensIn, tokensOut int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.usage[tenantID.String()]
	if !ok {
		u = &ai.DailyUsage{Day: day}
		f.usage[tenantID.String()] = u
	}
	u.TokensIn += tokensIn
	u.TokensOut += tokensOut
	return nil
}

func (f *fakeChatStore) GetForDay(ctx context.Context, tenantID uuid.UUID, day time.Time) (*ai.DailyUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.usage[tenantID.String()]
	if !ok {
		return &ai.DailyUsage{Day: day}, nil
	}
	u2 := *u
	return &u2, nil
}

type fakeRetriever struct {
	tradeIDs []uuid.UUID
	err      error
}

func (f *fakeRetriever) Retrieve(ctx context.Context, tenantID uuid.UUID, accountID *uuid.UUID, userMessage string, topK int) ([]uuid.UUID, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := f.tradeIDs
	if topK > 0 && len(out) > topK {
		out = out[:topK]
	}
	return out, nil
}

// ── Tests ────────────────────────────────────────────────────────────

func TestChatService_CreateThread_FindOrCreate(t *testing.T) {
	store := newFakeChatStore()
	svc := NewChatService(ChatConfig{Store: store, Provider: NewStubProvider()})
	tenantID := uuid.Must(uuid.NewV7())
	userID := uuid.Must(uuid.NewV7())
	accountID := uuid.Must(uuid.NewV7())

	t1, err := svc.CreateThread(context.Background(), ChatCreateInput{
		TenantID: tenantID, UserID: userID, AccountID: &accountID, Title: "EURUSD chat",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t2, err := svc.CreateThread(context.Background(), ChatCreateInput{
		TenantID: tenantID, UserID: userID, AccountID: &accountID, Title: "different title",
	})
	if err != nil {
		t.Fatalf("create 2: %v", err)
	}
	if t1.ID != t2.ID {
		t.Fatalf("expected find-or-create → same thread id; got %s vs %s", t1.ID, t2.ID)
	}
}

func TestChatService_CreateThread_NullAccount_Multiple(t *testing.T) {
	store := newFakeChatStore()
	svc := NewChatService(ChatConfig{Store: store, Provider: NewStubProvider()})
	tenantID := uuid.Must(uuid.NewV7())
	userID := uuid.Must(uuid.NewV7())
	t1, _ := svc.CreateThread(context.Background(), ChatCreateInput{TenantID: tenantID, UserID: userID})
	t2, _ := svc.CreateThread(context.Background(), ChatCreateInput{TenantID: tenantID, UserID: userID})
	if t1.ID == t2.ID {
		t.Fatalf("null-account should create multiple threads")
	}
}

func TestChatService_GetThread_NotFound(t *testing.T) {
	store := newFakeChatStore()
	svc := NewChatService(ChatConfig{Store: store, Provider: NewStubProvider()})
	_, err := svc.GetThread(context.Background(), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()))
	if !errors.Is(err, ErrChatThreadNotFound) {
		t.Fatalf("expected ErrChatThreadNotFound, got %v", err)
	}
}

func TestChatService_DeleteThread_Cascade(t *testing.T) {
	store := newFakeChatStore()
	svc := NewChatService(ChatConfig{Store: store, Provider: NewStubProvider()})
	tenantID := uuid.Must(uuid.NewV7())
	threadID := uuid.Must(uuid.NewV7())
	store.threads[threadID] = &ai.ChatThread{ID: threadID, TenantID: tenantID, UserID: uuid.Must(uuid.NewV7())}
	store.messages[uuid.Must(uuid.NewV7())] = &ai.ChatMessage{ID: uuid.Must(uuid.NewV7()), TenantID: tenantID, ThreadID: threadID, Role: ai.RoleUser, Content: "hi"}
	if err := svc.DeleteThread(context.Background(), tenantID, threadID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := store.threads[threadID]; ok {
		t.Fatalf("expected thread removed")
	}
	if len(store.messages) != 0 {
		t.Fatalf("expected cascade-delete messages")
	}
}

func TestChatService_ListThreads(t *testing.T) {
	store := newFakeChatStore()
	svc := NewChatService(ChatConfig{Store: store, Provider: NewStubProvider()})
	tenantID := uuid.Must(uuid.NewV7())
	userID := uuid.Must(uuid.NewV7())
	svc.CreateThread(context.Background(), ChatCreateInput{TenantID: tenantID, UserID: userID})
	svc.CreateThread(context.Background(), ChatCreateInput{TenantID: tenantID, UserID: userID})
	ts, err := svc.ListThreads(context.Background(), tenantID, userID, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(ts) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(ts))
	}
}

func TestChatService_SendMessage_HappyPath(t *testing.T) {
	store := newFakeChatStore()
	svc := NewChatService(ChatConfig{Store: store, Provider: NewStubProvider()})
	tenantID := uuid.Must(uuid.NewV7())
	userID := uuid.Must(uuid.NewV7())
	threadID := uuid.Must(uuid.NewV7())
	store.threads[threadID] = &ai.ChatThread{ID: threadID, TenantID: tenantID, UserID: userID}

	enqueued := false
	svc.SetEnqueueReply(func(id uuid.UUID) error { enqueued = true; return nil })

	m, err := svc.SendMessage(context.Background(), ChatSendInput{
		TenantID: tenantID, UserID: userID, ThreadID: threadID,
		Content: "What's my win rate?", PlanCode: "pro",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if m.Role != ai.RoleUser {
		t.Fatalf("expected user role, got %q", m.Role)
	}
	if !enqueued {
		t.Fatalf("expected enqueue")
	}
	u := store.usage[tenantID.String()]
	if u == nil || u.ChatRequests != 1 {
		t.Fatalf("expected chat_requests=1, got %v", u)
	}
}

func TestChatService_SendMessage_EmptyContent(t *testing.T) {
	store := newFakeChatStore()
	svc := NewChatService(ChatConfig{Store: store, Provider: NewStubProvider()})
	tenantID := uuid.Must(uuid.NewV7())
	userID := uuid.Must(uuid.NewV7())
	threadID := uuid.Must(uuid.NewV7())
	store.threads[threadID] = &ai.ChatThread{ID: threadID, TenantID: tenantID, UserID: userID}

	_, err := svc.SendMessage(context.Background(), ChatSendInput{
		TenantID: tenantID, UserID: userID, ThreadID: threadID, Content: "   ", PlanCode: "pro",
	})
	if err == nil {
		t.Fatalf("expected error for empty content")
	}
}

func TestChatService_SendMessage_BudgetExhausted(t *testing.T) {
	store := newFakeChatStore()
	svc := NewChatService(ChatConfig{Store: store, Provider: NewStubProvider()})
	tenantID := uuid.Must(uuid.NewV7())
	userID := uuid.Must(uuid.NewV7())
	threadID := uuid.Must(uuid.NewV7())
	store.threads[threadID] = &ai.ChatThread{ID: threadID, TenantID: tenantID, UserID: userID}
	day := time.Now().UTC().Truncate(24 * time.Hour)
	store.usage[tenantID.String()] = &ai.DailyUsage{Day: day, Requests: 5} // free plan cap

	_, err := svc.SendMessage(context.Background(), ChatSendInput{
		TenantID: tenantID, UserID: userID, ThreadID: threadID, Content: "hi", PlanCode: "free",
	})
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("expected ErrBudgetExhausted, got %v", err)
	}
}

func TestChatService_SendMessage_ThreadMismatch(t *testing.T) {
	store := newFakeChatStore()
	svc := NewChatService(ChatConfig{Store: store, Provider: NewStubProvider()})
	tenantID := uuid.Must(uuid.NewV7())
	threadID := uuid.Must(uuid.NewV7())
	store.threads[threadID] = &ai.ChatThread{ID: threadID, TenantID: tenantID, UserID: uuid.Must(uuid.NewV7())}

	// User sending to a thread that isn't theirs.
	_, err := svc.SendMessage(context.Background(), ChatSendInput{
		TenantID: tenantID, UserID: uuid.Must(uuid.NewV7()), ThreadID: threadID, Content: "hi",
	})
	if !errors.Is(err, ErrChatThreadNotFound) {
		t.Fatalf("expected ErrChatThreadNotFound, got %v", err)
	}
}

func TestChatService_BuildReply_PersistsAssistantMessage(t *testing.T) {
	store := newFakeChatStore()
	svc := NewChatService(ChatConfig{
		Store:     store,
		Retriever: &fakeRetriever{tradeIDs: []uuid.UUID{uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())}},
		Provider:  NewStubProvider(),
	})
	tenantID := uuid.Must(uuid.NewV7())
	userID := uuid.Must(uuid.NewV7())
	threadID := uuid.Must(uuid.NewV7())
	accountID := uuid.Must(uuid.NewV7())
	store.threads[threadID] = &ai.ChatThread{ID: threadID, TenantID: tenantID, UserID: userID, AccountID: &accountID}

	userMsgID := uuid.Must(uuid.NewV7())
	store.messages[userMsgID] = &ai.ChatMessage{
		ID: userMsgID, TenantID: tenantID, ThreadID: threadID, Role: ai.RoleUser,
		Content: "What was my best trade last week?",
	}

	if err := svc.BuildReply(context.Background(), userMsgID); err != nil {
		t.Fatalf("BuildReply: %v", err)
	}
	// Two messages now: user + assistant.
	if len(store.messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(store.messages))
	}
	// Find the assistant.
	var assistant *ai.ChatMessage
	for _, m := range store.messages {
		if m.Role == ai.RoleAssistant {
			assistant = m
		}
	}
	if assistant == nil {
		t.Fatalf("expected assistant message")
	}
	if assistant.Content == "" {
		t.Fatalf("expected non-empty content")
	}
	if len(assistant.ContextTradeIDs) != 2 {
		t.Fatalf("expected 2 context trade IDs, got %d", len(assistant.ContextTradeIDs))
	}
}

func TestChatService_BuildReply_ProviderError(t *testing.T) {
	store := newFakeChatStore()
	svc := NewChatService(ChatConfig{
		Store:     store,
		Retriever: &fakeRetriever{},
		Provider:  errorProvider{},
	})
	tenantID := uuid.Must(uuid.NewV7())
	userID := uuid.Must(uuid.NewV7())
	threadID := uuid.Must(uuid.NewV7())
	store.threads[threadID] = &ai.ChatThread{ID: threadID, TenantID: tenantID, UserID: userID}

	userMsgID := uuid.Must(uuid.NewV7())
	store.messages[userMsgID] = &ai.ChatMessage{
		ID: userMsgID, TenantID: tenantID, ThreadID: threadID, Role: ai.RoleUser, Content: "hi",
	}
	if err := svc.BuildReply(context.Background(), userMsgID); err == nil {
		t.Fatalf("expected error")
	}
	if len(store.messages) != 1 {
		t.Fatalf("user msg should remain (assistant never inserted), got %d", len(store.messages))
	}
}

func TestChatService_BuildReply_RetrieverErrorContinuesWithoutContext(t *testing.T) {
	store := newFakeChatStore()
	svc := NewChatService(ChatConfig{
		Store:     store,
		Retriever: &fakeRetriever{err: errors.New("rag down")},
		Provider:  NewStubProvider(),
	})
	tenantID := uuid.Must(uuid.NewV7())
	userID := uuid.Must(uuid.NewV7())
	threadID := uuid.Must(uuid.NewV7())
	store.threads[threadID] = &ai.ChatThread{ID: threadID, TenantID: tenantID, UserID: userID}

	userMsgID := uuid.Must(uuid.NewV7())
	store.messages[userMsgID] = &ai.ChatMessage{
		ID: userMsgID, TenantID: tenantID, ThreadID: threadID, Role: ai.RoleUser, Content: "hi",
	}
	if err := svc.BuildReply(context.Background(), userMsgID); err != nil {
		t.Fatalf("BuildReply: %v", err)
	}
	if len(store.messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(store.messages))
	}
	// Assistant should have empty context_trade_ids (retriever failed).
	for _, m := range store.messages {
		if m.Role == ai.RoleAssistant && len(m.ContextTradeIDs) != 0 {
			t.Fatalf("expected empty context trade IDs, got %d", len(m.ContextTradeIDs))
		}
	}
}

func TestChatService_TrimToLast50(t *testing.T) {
	store := newFakeChatStore()
	svc := NewChatService(ChatConfig{
		Store:     store,
		Retriever: &fakeRetriever{},
		Provider:  NewStubProvider(),
	})
	tenantID := uuid.Must(uuid.NewV7())
	threadID := uuid.Must(uuid.NewV7())
	for i := 0; i < 60; i++ {
		id := uuid.Must(uuid.NewV7())
		store.messages[id] = &ai.ChatMessage{
			ID: id, TenantID: tenantID, ThreadID: threadID, Role: ai.RoleUser,
			Content: "msg", RequestedAt: time.Now().Add(time.Duration(i) * time.Second),
		}
	}
	// Trigger trim via a fresh send + reply (which calls MessageTrimToLast50).
	store.threads[threadID] = &ai.ChatThread{ID: threadID, TenantID: tenantID, UserID: uuid.Must(uuid.NewV7())}
	userMsgID := uuid.Must(uuid.NewV7())
	store.messages[userMsgID] = &ai.ChatMessage{
		ID: userMsgID, TenantID: tenantID, ThreadID: threadID, Role: ai.RoleUser, Content: "trigger",
	}
	if err := svc.BuildReply(context.Background(), userMsgID); err != nil {
		t.Fatalf("BuildReply: %v", err)
	}
	count := 0
	for _, m := range store.messages {
		if m.ThreadID == threadID {
			count++
		}
	}
	if count > 50 {
		t.Fatalf("expected ≤50 messages after trim, got %d", count)
	}
}

func TestBuildChatPrompt_Shape(t *testing.T) {
	history := []*ai.ChatMessage{
		{ID: uuid.Must(uuid.NewV7()), Role: ai.RoleUser, Content: "first question"},
		{ID: uuid.Must(uuid.NewV7()), Role: ai.RoleAssistant, Content: "first answer"},
		{ID: uuid.Must(uuid.NewV7()), Role: ai.RoleUser, Content: "follow up"},
	}
	tradeIDs := []uuid.UUID{uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())}
	sys, user := buildChatPrompt(history, tradeIDs, "what about EURUSD?")
	if !strings.Contains(sys, "trading analyst") {
		t.Fatalf("system missing role")
	}
	if !strings.Contains(user, "Recent trade context") {
		t.Fatalf("user missing RAG marker")
	}
	if !strings.Contains(user, "first question") {
		t.Fatalf("user missing history")
	}
	if !strings.Contains(user, "what about EURUSD?") {
		t.Fatalf("user missing question")
	}
}