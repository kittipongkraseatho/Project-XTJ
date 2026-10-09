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
	"github.com/trading-journal/api/internal/domain/trade"
)

// ── Fakes ────────────────────────────────────────────────────────────

type fakeRecapStore struct {
	mu          sync.Mutex
	recaps      map[uuid.UUID]*ai.Recap
	weeklyUsage map[string]*ai.DailyUsage
}

func newFakeRecapStore() *fakeRecapStore {
	return &fakeRecapStore{
		recaps:      map[uuid.UUID]*ai.Recap{},
		weeklyUsage: map[string]*ai.DailyUsage{},
	}
}

func (f *fakeRecapStore) Create(ctx context.Context, r *ai.Recap) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r2 := *r
	f.recaps[r.ID] = &r2
	return nil
}

func (f *fakeRecapStore) GetByID(ctx context.Context, id uuid.UUID) (*ai.Recap, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.recaps[id]
	if !ok {
		return nil, errors.New("not found")
	}
	r2 := *r
	return &r2, nil
}

func (f *fakeRecapStore) ListByAccount(ctx context.Context, tenantID, accountID uuid.UUID, limit int) ([]*ai.Recap, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []*ai.Recap{}
	for _, r := range f.recaps {
		if r.TenantID == tenantID && r.AccountID == accountID {
			r2 := *r
			out = append(out, &r2)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f *fakeRecapStore) FindRecentWithHash(ctx context.Context, tenantID, accountID uuid.UUID, weekStart time.Time, hash string, sinceMinutes int) (*ai.Recap, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	since := time.Now().UTC().Add(-time.Duration(sinceMinutes) * time.Minute)
	for _, r := range f.recaps {
		if r.TenantID == tenantID && r.AccountID == accountID &&
			r.WeekStart.Equal(weekStart) && r.PromptHash == hash &&
			r.Status == ai.StatusSuccess && r.CompletedAt != nil && r.CompletedAt.After(since) {
			r2 := *r
			return &r2, nil
		}
	}
	return nil, nil
}

func (f *fakeRecapStore) MarkGenerating(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.recaps[id]
	r.Status = ai.StatusGenerating
	return nil
}

func (f *fakeRecapStore) MarkSuccess(ctx context.Context, id uuid.UUID, content string, tokensIn, tokensOut int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.recaps[id]
	r.Status = ai.StatusSuccess
	r.Content = &content
	r.TokensIn = tokensIn
	r.TokensOut = tokensOut
	now := time.Now().UTC()
	r.CompletedAt = &now
	return nil
}

func (f *fakeRecapStore) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string, tokensIn int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := f.recaps[id]
	r.Status = ai.StatusFailed
	r.Error = &errMsg
	now := time.Now().UTC()
	r.CompletedAt = &now
	return nil
}

func (f *fakeRecapStore) IncrementRecapRequests(ctx context.Context, tenantID uuid.UUID, day time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.weeklyUsage[tenantID.String()]
	if !ok {
		u = &ai.DailyUsage{Day: day}
		f.weeklyUsage[tenantID.String()] = u
	}
	u.RecapRequests++
	return nil
}

func (f *fakeRecapStore) AddTokens(ctx context.Context, tenantID uuid.UUID, day time.Time, tokensIn, tokensOut int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.weeklyUsage[tenantID.String()]
	if !ok {
		u = &ai.DailyUsage{Day: day}
		f.weeklyUsage[tenantID.String()] = u
	}
	u.TokensIn += tokensIn
	u.TokensOut += tokensOut
	return nil
}

func (f *fakeRecapStore) GetForDay(ctx context.Context, tenantID uuid.UUID, day time.Time) (*ai.DailyUsage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.weeklyUsage[tenantID.String()]
	if !ok {
		return &ai.DailyUsage{Day: day}, nil
	}
	u2 := *u
	return &u2, nil
}

type fakeRecapLoader struct {
	trades []*trade.Trade
	err    error
}

func (f *fakeRecapLoader) LoadWeekTrades(ctx context.Context, tenantID, accountID uuid.UUID, from, to time.Time) ([]*trade.Trade, error) {
	return f.trades, f.err
}

// ── Tests ────────────────────────────────────────────────────────────

func makeTrade(sym string, pl float64, outcome trade.Outcome, openT time.Time) *trade.Trade {
	closeT := openT.Add(time.Hour)
	cp := pl // arbitrary
	return &trade.Trade{
		ID:               uuid.Must(uuid.NewV7()),
		TenantID:         uuid.Must(uuid.NewV7()),
		TradingAccountID: uuid.Must(uuid.NewV7()),
		Symbol:           sym,
		Direction:        trade.DirectionLong,
		OpenTime:         openT,
		OpenPrice:        100,
		CloseTime:        &closeT,
		ClosePrice:       &cp,
		NetPL:            pl,
		Outcome:          outcome,
	}
}

func TestRecapService_RecapAccount_HappyPath(t *testing.T) {
	tenantID := uuid.Must(uuid.NewV7())
	accountID := uuid.Must(uuid.NewV7())
	userID := uuid.Must(uuid.NewV7())
	loader := &fakeRecapLoader{
		trades: []*trade.Trade{
			makeTrade("EURUSD", 100, trade.OutcomeWin, time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)),
			makeTrade("GBPUSD", -50, trade.OutcomeLoss, time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)),
		},
	}
	store := newFakeRecapStore()
	svc := NewRecapService(RecapConfig{
		Store:    store,
		Loader:   loader,
		Provider: NewStubProvider(),
	})
	enqueued := false
	svc.SetEnqueueRecap(func(id uuid.UUID) error { enqueued = true; return nil })

	in := RecapInput{
		TenantID: tenantID, AccountID: accountID, UserID: userID,
		WeekStart: mondayOf(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)),
		PlanCode:  "pro",
	}
	r, err := svc.RecapAccount(context.Background(), in)
	if err != nil {
		t.Fatalf("RecapAccount: %v", err)
	}
	if r.Status != ai.StatusPending {
		t.Fatalf("expected pending, got %q", r.Status)
	}
	if !enqueued {
		t.Fatalf("expected enqueue callback")
	}
	if len(store.recaps) != 1 {
		t.Fatalf("expected 1 stored recap, got %d", len(store.recaps))
	}
	u := store.weeklyUsage[tenantID.String()]
	if u == nil || u.RecapRequests != 1 {
		t.Fatalf("expected recap_requests=1, got %v", u)
	}
}

func TestRecapService_RecapAccount_DefaultToLastCompletedWeek(t *testing.T) {
	tenantID := uuid.Must(uuid.NewV7())
	loader := &fakeRecapLoader{trades: nil}
	store := newFakeRecapStore()
	svc := NewRecapService(RecapConfig{Store: store, Loader: loader, Provider: NewStubProvider()})

	r, err := svc.RecapAccount(context.Background(), RecapInput{
		TenantID: tenantID, AccountID: uuid.Must(uuid.NewV7()), UserID: uuid.Must(uuid.NewV7()),
		PlanCode: "pro",
	})
	if err != nil {
		t.Fatalf("RecapAccount: %v", err)
	}
	if r.WeekStart.IsZero() {
		t.Fatalf("week_start should be set by default")
	}
	if r.WeekStart.Weekday() != time.Monday {
		t.Fatalf("week_start should be Monday UTC, got %s", r.WeekStart.Weekday())
	}
	if !r.WeekEnd.After(r.WeekStart) {
		t.Fatalf("week_end should be after week_start")
	}
}

func TestRecapService_RecapAccount_BudgetExhausted(t *testing.T) {
	tenantID := uuid.Must(uuid.NewV7())
	loader := &fakeRecapLoader{trades: nil}
	store := newFakeRecapStore()
	// Pre-fill today's usage to the cap.
	day := time.Now().UTC().Truncate(24 * time.Hour)
	store.weeklyUsage[tenantID.String()] = &ai.DailyUsage{Day: day, Requests: 5}
	svc := NewRecapService(RecapConfig{Store: store, Loader: loader, Provider: NewStubProvider()})

	_, err := svc.RecapAccount(context.Background(), RecapInput{
		TenantID: tenantID, AccountID: uuid.Must(uuid.NewV7()), UserID: uuid.Must(uuid.NewV7()),
		PlanCode: "free",
	})
	if !errors.Is(err, ErrBudgetExhausted) {
		t.Fatalf("expected ErrBudgetExhausted, got %v", err)
	}
}

func TestRecapService_RecapAccount_IdempotentCacheHit(t *testing.T) {
	tenantID := uuid.Must(uuid.NewV7())
	accountID := uuid.Must(uuid.NewV7())
	loader := &fakeRecapLoader{trades: []*trade.Trade{
		makeTrade("EURUSD", 100, trade.OutcomeWin, time.Now()),
	}}
	store := newFakeRecapStore()
	svc := NewRecapService(RecapConfig{Store: store, Loader: loader, Provider: NewStubProvider()})

	// Seed an existing success recap.
	hash := computeRecapHash(accountID, mondayOf(time.Now()), loader.trades, "stub")
	seed := &ai.Recap{
		ID: uuid.Must(uuid.NewV7()), TenantID: tenantID, AccountID: accountID,
		WeekStart: mondayOf(time.Now()), WeekEnd: mondayOf(time.Now()).AddDate(0, 0, 6),
		PromptHash: hash, Provider: "stub", ModelID: "stub-default", Status: ai.StatusSuccess,
		Content:           strPtr("cached recap content"),
		RequestedByUserID: uuid.Must(uuid.NewV7()),
		RequestedAt:       time.Now().Add(-time.Minute),
	}
	completedAt := time.Now().Add(-30 * time.Second)
	seed.CompletedAt = &completedAt
	store.recaps[seed.ID] = seed

	r, err := svc.RecapAccount(context.Background(), RecapInput{
		TenantID: tenantID, AccountID: accountID, UserID: uuid.Must(uuid.NewV7()),
		WeekStart: mondayOf(time.Now()), PlanCode: "pro",
	})
	if err != nil {
		t.Fatalf("RecapAccount: %v", err)
	}
	if r.ID != seed.ID {
		t.Fatalf("expected cached recap ID %s, got %s", seed.ID, r.ID)
	}
	if r.Content == nil || *r.Content != "cached recap content" {
		t.Fatalf("expected cached content")
	}
	if len(store.recaps) != 1 {
		t.Fatalf("expected still 1 recap (no new insert), got %d", len(store.recaps))
	}
}

func TestRecapService_BuildRecap_PersistsContent(t *testing.T) {
	tenantID := uuid.Must(uuid.NewV7())
	accountID := uuid.Must(uuid.NewV7())
	loader := &fakeRecapLoader{trades: []*trade.Trade{
		makeTrade("EURUSD", 100, trade.OutcomeWin, time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)),
	}}
	store := newFakeRecapStore()
	svc := NewRecapService(RecapConfig{Store: store, Loader: loader, Provider: NewStubProvider()})

	in := RecapInput{
		TenantID: tenantID, AccountID: accountID, UserID: uuid.Must(uuid.NewV7()),
		WeekStart: mondayOf(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)),
		PlanCode:  "pro",
	}
	r, err := svc.RecapAccount(context.Background(), in)
	if err != nil {
		t.Fatalf("kick: %v", err)
	}

	if err := svc.BuildRecap(context.Background(), r.ID); err != nil {
		t.Fatalf("BuildRecap: %v", err)
	}
	got, err := svc.GetRecap(context.Background(), tenantID, r.ID)
	if err != nil {
		t.Fatalf("GetRecap: %v", err)
	}
	if got.Status != ai.StatusSuccess {
		t.Fatalf("expected success, got %q", got.Status)
	}
	if got.Content == nil || *got.Content == "" {
		t.Fatalf("expected non-empty content")
	}
}

func TestRecapService_BuildRecap_IdempotentOnSuccess(t *testing.T) {
	store := newFakeRecapStore()
	svc := NewRecapService(RecapConfig{Store: store, Loader: &fakeRecapLoader{}, Provider: NewStubProvider()})
	id := uuid.Must(uuid.NewV7())
	store.recaps[id] = &ai.Recap{
		ID: id, Status: ai.StatusSuccess, Content: strPtr("done"),
		CompletedAt: timePtr(time.Now()),
	}
	if err := svc.BuildRecap(context.Background(), id); err != nil {
		t.Fatalf("BuildRecap idempotent should be no-op, got %v", err)
	}
}

func TestRecapService_BuildRecap_MarkFailedOnProviderError(t *testing.T) {
	store := newFakeRecapStore()
	tenantID := uuid.Must(uuid.NewV7())
	accountID := uuid.Must(uuid.NewV7())
	weekStart := mondayOf(time.Now())
	loader := &fakeRecapLoader{}
	// Use a provider that always errors.
	bad := errorProvider{}
	svc := NewRecapService(RecapConfig{Store: store, Loader: loader, Provider: bad})
	id := uuid.Must(uuid.NewV7())
	store.recaps[id] = &ai.Recap{
		ID: id, TenantID: tenantID, AccountID: accountID,
		WeekStart: weekStart, WeekEnd: weekStart.AddDate(0, 0, 6),
		PromptHash: "x", Provider: "stub", Status: ai.StatusPending,
		RequestedByUserID: uuid.Must(uuid.NewV7()),
	}
	if err := svc.BuildRecap(context.Background(), id); err == nil {
		t.Fatalf("expected error")
	}
	got, _ := store.GetByID(context.Background(), id)
	if got.Status != ai.StatusFailed {
		t.Fatalf("expected failed, got %q", got.Status)
	}
}

func TestRecapService_GetRecap_WrongTenant(t *testing.T) {
	store := newFakeRecapStore()
	id := uuid.Must(uuid.NewV7())
	store.recaps[id] = &ai.Recap{
		ID: id, TenantID: uuid.Must(uuid.NewV7()), AccountID: uuid.Must(uuid.NewV7()),
		Status: ai.StatusPending,
	}
	svc := NewRecapService(RecapConfig{Store: store, Loader: &fakeRecapLoader{}, Provider: NewStubProvider()})
	_, err := svc.GetRecap(context.Background(), uuid.Must(uuid.NewV7()), id)
	if !errors.Is(err, ErrRecapNotFound) {
		t.Fatalf("expected ErrRecapNotFound, got %v", err)
	}
}

func TestComputeRecapHash_Deterministic(t *testing.T) {
	accID := uuid.Must(uuid.NewV7())
	t1 := makeTrade("X", 1, trade.OutcomeWin, time.Now())
	t2 := makeTrade("Y", -1, trade.OutcomeLoss, time.Now())
	weekStart := mondayOf(time.Now())
	h1 := computeRecapHash(accID, weekStart, []*trade.Trade{t1, t2}, "stub")
	h2 := computeRecapHash(accID, weekStart, []*trade.Trade{t1, t2}, "stub")
	if h1 != h2 {
		t.Fatalf("expected deterministic hash")
	}
	h3 := computeRecapHash(accID, weekStart, []*trade.Trade{t2, t1}, "stub")
	if h1 != h3 {
		t.Fatalf("hash should be order-independent (sorts trade IDs)")
	}
	h4 := computeRecapHash(accID, weekStart, []*trade.Trade{t1}, "stub")
	if h1 == h4 {
		t.Fatalf("different trade set → different hash")
	}
}

func TestDefaultRecapPromptBuilder_IncludesTradesAndStats(t *testing.T) {
	openT := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	tr := makeTrade("EURUSD", 100, trade.OutcomeWin, openT)
	stats := computeWeekStats([]*trade.Trade{tr})
	sys, user := DefaultRecapPromptBuilder(RecapWeekContext{
		AccountName: "Demo",
		WeekStart:   mondayOf(openT),
		WeekEnd:     mondayOf(openT).AddDate(0, 0, 6),
		Trades:      []*trade.Trade{tr},
		Stats:       stats,
	})
	if !strings.Contains(sys, "trading analyst") {
		t.Fatalf("system prompt missing role: %q", sys)
	}
	if !strings.Contains(user, "EURUSD") {
		t.Fatalf("user prompt missing trade symbol: %q", user)
	}
	if !strings.Contains(user, "Win rate") {
		t.Fatalf("user prompt missing stats: %q", user)
	}
	if !strings.Contains(user, "## Recap") {
		t.Fatalf("user prompt missing # Recap marker: %q", user)
	}
}

// ── Test helpers ──────────────────────────────────────────────────────

type errorProvider struct{}

func (errorProvider) Name() string { return "error-provider" }
func (errorProvider) Generate(ctx context.Context, in GenerateInput) (*GenerateResult, error) {
	return nil, errors.New("provider boom")
}

func strPtr(s string) *string        { return &s }
func timePtr(t time.Time) *time.Time { return &t }
