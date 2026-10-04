package ai_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/ai"
	"github.com/trading-journal/api/internal/domain/trade"
	intai "github.com/trading-journal/api/internal/integration/ai"
)

// ── Fake store ──────────────────────────────────────────────────────

type fakeStore struct {
	rows     map[uuid.UUID]*ai.Analysis
	usage    map[string]*ai.DailyUsage // key = "tenant|day"
	trades   map[uuid.UUID]*trade.Trade
	recent   []*trade.Trade
	stats    intai.AccountStats
	loadErr  error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		rows:   map[uuid.UUID]*ai.Analysis{},
		usage:  map[string]*ai.DailyUsage{},
		trades: map[uuid.UUID]*trade.Trade{},
		stats:  intai.AccountStats{WindowDays: 30, TotalTrades: 10, WinRate: 0.6, AvgNetCents: 250, AvgHoldMinutes: 90},
		recent: nil,
	}
}

func (f *fakeStore) Create(ctx context.Context, a *ai.Analysis) error {
	f.rows[a.ID] = a
	return nil
}
func (f *fakeStore) GetByID(ctx context.Context, id uuid.UUID) (*ai.Analysis, error) {
	if a, ok := f.rows[id]; ok {
		return a, nil
	}
	return nil, errors.New("not found")
}
func (f *fakeStore) GetLatestForTrade(ctx context.Context, tenantID, tradeID uuid.UUID) (*ai.Analysis, error) {
	for _, a := range f.rows {
		if a.TenantID == tenantID && a.TradeID == tradeID {
			return a, nil
		}
	}
	return nil, errors.New("not found")
}
func (f *fakeStore) FindRecentWithHash(ctx context.Context, tenantID, tradeID uuid.UUID, hash string, sinceMinutes int) (*ai.Analysis, error) {
	cutoff := time.Now().Add(-time.Duration(sinceMinutes) * time.Minute)
	for _, a := range f.rows {
		if a.TenantID == tenantID && a.TradeID == tradeID && a.PromptHash == hash && a.Status == ai.StatusSuccess && a.RequestedAt.After(cutoff) {
			return a, nil
		}
	}
	return nil, nil
}
func (f *fakeStore) MarkGenerating(ctx context.Context, id uuid.UUID) error {
	if a, ok := f.rows[id]; ok {
		a.Status = ai.StatusGenerating
	}
	return nil
}
func (f *fakeStore) MarkSuccess(ctx context.Context, id uuid.UUID, content string, tokensIn, tokensOut int) error {
	if a, ok := f.rows[id]; ok {
		a.Status = ai.StatusSuccess
		a.Content = &content
		a.TokensIn = tokensIn
		a.TokensOut = tokensOut
		ts := time.Now().UTC()
		a.CompletedAt = &ts
	}
	return nil
}
func (f *fakeStore) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string, tokensIn int) error {
	if a, ok := f.rows[id]; ok {
		a.Status = ai.StatusFailed
		a.Error = &errMsg
	}
	return nil
}
func (f *fakeStore) IncrementRequests(ctx context.Context, tenantID uuid.UUID, day time.Time) (*ai.DailyUsage, error) {
	k := tenantID.String() + "|" + day.Format("2006-01-02")
	u, ok := f.usage[k]
	if !ok {
		u = &ai.DailyUsage{Day: day}
		f.usage[k] = u
	}
	u.Requests++
	return u, nil
}
func (f *fakeStore) AddTokens(ctx context.Context, tenantID uuid.UUID, day time.Time, tokensIn, tokensOut int64) error {
	k := tenantID.String() + "|" + day.Format("2006-01-02")
	if u, ok := f.usage[k]; ok {
		u.TokensIn += tokensIn
		u.TokensOut += tokensOut
	}
	return nil
}
func (f *fakeStore) GetForDay(ctx context.Context, tenantID uuid.UUID, day time.Time) (*ai.DailyUsage, error) {
	k := tenantID.String() + "|" + day.Format("2006-01-02")
	if u, ok := f.usage[k]; ok {
		return u, nil
	}
	return &ai.DailyUsage{Day: day}, nil
}

// TradeLoader impl
func (f *fakeStore) LoadTradeForAnalysis(ctx context.Context, tenantID, tradeID uuid.UUID) (*trade.Trade, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	if t, ok := f.trades[tradeID]; ok {
		return t, nil
	}
	return nil, errors.New("trade not found")
}
func (f *fakeStore) LoadRecentTrades(ctx context.Context, tenantID, tradingAccountID uuid.UUID, n int) ([]*trade.Trade, error) {
	return f.recent, nil
}
func (f *fakeStore) AccountStats30d(ctx context.Context, tenantID, tradingAccountID uuid.UUID) (intai.AccountStats, error) {
	return f.stats, nil
}

func mkTrade(t *testing.T) *trade.Trade {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	return &trade.Trade{
		ID:        id,
		TenantID:  uuid.Must(uuid.NewV7()),
		Symbol:    "EURUSD",
		Direction: trade.DirectionLong,
		Volume:    1.0,
		OpenTime:  time.Now().UTC().Add(-2 * time.Hour),
		CloseTime: func() *time.Time { t := time.Now().UTC(); return &t }(),
		NetPL:     250.5,
		Outcome:   trade.OutcomeWin,
	}
}

// ── Tests ───────────────────────────────────────────────────────────

func TestServiceImpl_Analyze_FirstCall_PersistsPending(t *testing.T) {
	svc := intai.NewServiceImpl(intai.ServiceConfig{
		Store:    newFakeStore(),
		Provider: intai.NewStubProvider(),
		Log:      slog.Default(),
	})
	trade := mkTrade(t)
	// Inject into fake store via interface (cast).
	fs := svc.Store().(*fakeStore)
	fs.trades[trade.ID] = trade

	in := intai.AnalyzeInput{
		TenantID: trade.TenantID,
		TradeID:  trade.ID,
		UserID:   uuid.Must(uuid.NewV7()),
		PlanCode: "free",
	}
	a, err := svc.Analyze(context.Background(), in)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if a.Status != ai.StatusPending {
		t.Errorf("want pending, got %s", a.Status)
	}
	if a.PromptHash == "" {
		t.Errorf("prompt hash should be set")
	}
}

func TestServiceImpl_Analyze_IdempotentOnSameInput(t *testing.T) {
	svc := intai.NewServiceImpl(intai.ServiceConfig{
		Store:    newFakeStore(),
		Provider: intai.NewStubProvider(),
		Log:      slog.Default(),
	})
	fs := svc.Store().(*fakeStore)
	tr := mkTrade(t)
	fs.trades[tr.ID] = tr
	in := intai.AnalyzeInput{TenantID: tr.TenantID, TradeID: tr.ID, UserID: uuid.Must(uuid.NewV7()), PlanCode: "free"}

	// First call → pending.
	a1, err := svc.Analyze(context.Background(), in)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	// Simulate the worker.
	if err := svc.BuildAnalysis(context.Background(), a1.ID); err != nil {
		t.Fatalf("BuildAnalysis: %v", err)
	}
	// Second call → returns the cached success.
	a2, err := svc.Analyze(context.Background(), in)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if a1.ID != a2.ID {
		t.Errorf("expected cached same id %s, got %s", a1.ID, a2.ID)
	}
	if a2.Status != ai.StatusSuccess {
		t.Errorf("want success, got %s", a2.Status)
	}
}

func TestServiceImpl_Analyze_BudgetExhausted(t *testing.T) {
	svc := intai.NewServiceImpl(intai.ServiceConfig{
		Store:    newFakeStore(),
		Provider: intai.NewStubProvider(),
		Log:      slog.Default(),
	})
	fs := svc.Store().(*fakeStore)
	tr := mkTrade(t)
	fs.trades[tr.ID] = tr

	// Pre-fill usage to the free ceiling.
	day := time.Now().UTC().Truncate(24 * time.Hour)
	fs.usage[tr.TenantID.String()+"|"+day.Format("2006-01-02")] = &ai.DailyUsage{Day: day, Requests: 5}

	in := intai.AnalyzeInput{TenantID: tr.TenantID, TradeID: tr.ID, UserID: uuid.Must(uuid.NewV7()), PlanCode: "free"}
	if _, err := svc.Analyze(context.Background(), in); !errors.Is(err, intai.ErrBudgetExhausted) {
		t.Fatalf("want ErrBudgetExhausted, got %v", err)
	}
}

func TestServiceImpl_Analyze_ProPlanHigherBudget(t *testing.T) {
	svc := intai.NewServiceImpl(intai.ServiceConfig{
		Store:    newFakeStore(),
		Provider: intai.NewStubProvider(),
		Log:      slog.Default(),
	})
	fs := svc.Store().(*fakeStore)
	tr := mkTrade(t)
	fs.trades[tr.ID] = tr

	day := time.Now().UTC().Truncate(24 * time.Hour)
	// Free plan would block at 5 — Pro plan allows 50.
	fs.usage[tr.TenantID.String()+"|"+day.Format("2006-01-02")] = &ai.DailyUsage{Day: day, Requests: 10}

	in := intai.AnalyzeInput{TenantID: tr.TenantID, TradeID: tr.ID, UserID: uuid.Must(uuid.NewV7()), PlanCode: "pro"}
	if _, err := svc.Analyze(context.Background(), in); err != nil {
		t.Fatalf("pro plan should allow over 5 requests: %v", err)
	}
}

func TestServiceImpl_BuildAnalysis_GeneratesAndUpdates(t *testing.T) {
	svc := intai.NewServiceImpl(intai.ServiceConfig{
		Store:    newFakeStore(),
		Provider: intai.NewStubProvider(),
		Log:      slog.Default(),
	})
	fs := svc.Store().(*fakeStore)
	tr := mkTrade(t)
	fs.trades[tr.ID] = tr

	a := &ai.Analysis{
		ID: uuid.Must(uuid.NewV7()), TenantID: tr.TenantID, TradeID: tr.ID,
		PromptHash: "x", ModelID: "stub-1.0", Provider: "stub",
		Status: ai.StatusPending, RequestedByUserID: uuid.Must(uuid.NewV7()),
		RequestedAt: time.Now().UTC(),
	}
	fs.rows[a.ID] = a

	if err := svc.BuildAnalysis(context.Background(), a.ID); err != nil {
		t.Fatalf("BuildAnalysis: %v", err)
	}
	got := fs.rows[a.ID]
	if got.Status != ai.StatusSuccess {
		t.Errorf("want success, got %s", got.Status)
	}
	if got.Content == nil || *got.Content == "" {
		t.Errorf("want content set")
	}
	if got.TokensOut == 0 {
		t.Errorf("want tokens_out > 0")
	}
}

func TestServiceImpl_GetDailyUsage(t *testing.T) {
	svc := intai.NewServiceImpl(intai.ServiceConfig{
		Store:    newFakeStore(),
		Provider: intai.NewStubProvider(),
		Log:      slog.Default(),
	})
	tenantID := uuid.Must(uuid.NewV7())
	day := time.Now().UTC().Truncate(24 * time.Hour)
	fs := svc.Store().(*fakeStore)
	fs.usage[tenantID.String()+"|"+day.Format("2006-01-02")] = &ai.DailyUsage{Day: day, Requests: 3}

	u, err := svc.GetDailyUsage(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("GetDailyUsage: %v", err)
	}
	if u.Requests != 3 {
		t.Errorf("want 3, got %d", u.Requests)
	}
}

// ai.BudgetForPlan coverage is exercised transitively above; add an
// explicit assertion for the helper.
func TestBudgetForPlan(t *testing.T) {
	cases := map[string]int{"pro": ai.ProDailyBudget, "free": ai.FreeDailyBudget, "starter": ai.FreeDailyBudget, "": ai.FreeDailyBudget}
	for plan, want := range cases {
		if got := ai.BudgetForPlan(plan); got != want {
			t.Errorf("BudgetForPlan(%q) = %d, want %d", plan, got, want)
		}
	}
	if ai.BudgetForPlan("pro") > ai.HardDailyCap {
		t.Errorf("pro ceiling %d > hard cap %d", ai.BudgetForPlan("pro"), ai.HardDailyCap)
	}
}