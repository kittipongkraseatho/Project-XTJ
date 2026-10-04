# Trading Journal — Phase 6 Design

| | |
|---|---|
| **Status** | ✅ Approved 2026-10-04 — implementation starting |
| **Date** | 2026-10-04 |
| **Phase** | 6 — AI Analysis (LLM trade commentary) |
| **Scope** | Provider interface · OpenAI + Anthropic + Stub · Per-trade commentary · Async generation · Per-tenant daily budget |
| **Out of scope** | Weekly/monthly recap · Q&A chat · Streaming · Image input (screenshots) · Per-user prompt caching · Anthropic prompt caching layer  |

---

## 1. Decisions Resolved

| Fork | Decision |
|---|---|
| Feature surface | **Per-trade commentary**: 2-3 paragraphs analyzing a single trade with context (account stats + recent N trades) |
| Provider impls | **OpenAI + Anthropic + Stub** (interface-based, env-gated switch) |
| Generation mode | **Async** via `worker.Pool` — kick-off returns 202; client polls `/v1/trades/:id/analysis` |
| Storage | New table `trade_analyses` (1 row per "current" analysis per trade; superseded on regenerate) |
| Daily budget | Free plan: 5/day · Pro plan: 50/day · Hard cap: 100/day regardless of plan |

---

## 2. Provider interface

```go
// internal/integration/ai/provider.go

type Provider interface {
    Name() string
    Generate(ctx context.Context, in GenerateInput) (*GenerateResult, error)
}

type GenerateInput struct {
    SystemPrompt string  // role + rules
    UserPrompt   string  // trade + context
    MaxTokens   int
}

type GenerateResult struct {
    Text       string
    TokensIn   int
    TokensOut  int
    ModelID    string  // e.g. "gpt-4o-mini-2024-07-18"
    LatencyMs  int
}
```

### 2.1 Providers shipped

| Provider | Env vars | Library |
|---|---|---|
| `StubProvider` | none | hardcoded canned response |
| `OpenAIProvider` | `OPENAI_API_KEY`, `OPENAI_MODEL` | `github.com/sashabaranov/go-openai` |
| `AnthropicProvider` | `ANTHROPIC_API_KEY`, `ANTHROPIC_MODEL` | `github.com/anthropics/anthropic-sdk-go` |

Router wiring (Phase 6 §6):

```go
var aiProvider provider.Provider = provider.NewStub()
switch cfg.AIProvider {
case "openai":
    p, err := provider.NewOpenAI(cfg.OpenAIKey, cfg.OpenAIModel, logger)
    if err != nil { ... } else { aiProvider = p }
case "anthropic":
    p, err := provider.NewAnthropic(cfg.AnthropicKey, cfg.AnthropicModel, logger)
    if err != nil { ... } else { aiProvider = p }
}
```

Default: `stub` (canned response, no external calls). CI/dev runs stub.

---

## 3. New Tables (Migration 0008)

### 3.1 `trade_analyses`

| Column | Type | Notes |
|---|---|---|
| `id` | UUID PK | UUIDv7 |
| `tenant_id` | UUID NOT NULL FK tenants CASCADE | |
| `trade_id` | UUID NOT NULL FK trades CASCADE | |
| `prompt_hash` | TEXT NOT NULL | hash of (trade_id + recent_trade_ids + provider_model). Detects stale cache. |
| `content` | TEXT | generated text (nullable while generating) |
| `tokens_in` | INT | |
| `tokens_out` | INT | |
| `model_id` | TEXT NOT NULL | e.g. "gpt-4o-mini-..." |
| `provider` | TEXT NOT NULL | "openai", "anthropic", "stub" |
| `status` | TEXT NOT NULL | `pending` / `generating` / `success` / `failed` |
| `error` | TEXT | nullable; failure reason |
| `requested_by_user_id` | UUID FK users | |
| `requested_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| `completed_at` | TIMESTAMPTZ | |

Indexes:
- INDEX(`tenant_id`, `trade_id`, `requested_at` DESC) — get latest for a trade
- INDEX(`tenant_id`, `status`, `requested_at`) — daily budget counting

### 3.2 `ai_usage_daily`

| Column | Type | Notes |
|---|---|---|
| `id` | UUID PK | |
| `tenant_id` | UUID NOT NULL FK tenants CASCADE | |
| `day` | DATE NOT NULL | UTC date |
| `requests` | INT NOT NULL DEFAULT 0 | count of started analyses |
| `tokens_in` | BIGINT NOT NULL DEFAULT 0 | |
| `tokens_out` | BIGINT NOT NULL DEFAULT 0 | |

Indexes:
- UNIQUE(`tenant_id`, `day`) — one row per tenant per day
- INDEX(`tenant_id`, `day` DESC)

---

## 4. Prompt shape

System prompt:
```
You are a trading coach. Analyze the trade below in 2-3 paragraphs.
Focus on: (1) What worked, (2) What didn't, (3) One concrete suggestion.
Be specific. Use the account context and recent trades to inform your analysis.
Never recommend specific instruments or financial products. Be brief.
```

User prompt (built by `prompt_builder.go`):
```
# Trade
{trade fields}

# Recent trades (last 5)
{compact table}

# Account stats (last 30 days)
{win rate, avg p/l, hold time}

# Analysis
```

---

## 5. Service interface

```go
// internal/integration/ai/service.go

type Service interface {
    // Kick off an analysis. Enforces daily budget.
    // Returns the new analysis row (status=pending).
    // If a recent successful analysis exists with the same prompt_hash,
    // returns it directly (idempotent — no API call).
    Analyze(ctx context.Context, in AnalyzeInput) (*Analysis, error)

    // Get the latest analysis for a trade (any status).
    GetLatestForTrade(ctx context.Context, tenantID, tradeID uuid.UUID) (*Analysis, error)

    // Tenant's daily usage (for UI budget display).
    GetDailyUsage(ctx context.Context, tenantID uuid.UUID) (*DailyUsage, error)
}

type AnalyzeInput struct {
    TenantID   uuid.UUID
    TradeID    uuid.UUID
    UserID     uuid.UUID
}
```

### 5.1 Daily budget enforcement

```go
const (
    FreeDailyBudget  = 5
    ProDailyBudget  = 50
    HardDailyCap    = 100
)

func budgetFor(planCode string) int {
    switch planCode {
    case "pro": return ProDailyBudget
    default:    return FreeDailyBudget
    }
}
```

Before kicking off an analysis:
1. Look up tenant's plan (from `subscriptions` table — Phase 4 already populated it; Phase 5+ reads via plan repo)
2. Get today's usage from `ai_usage_daily`
3. If `usage.requests >= min(budgetFor(plan), HardDailyCap)` → return `ErrBudgetExhausted`
4. Otherwise increment `requests` and proceed

---

## 6. HTTP endpoints (JWT-authed)

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/v1/trades/:id/analyze` | kick off analysis (returns 202 + analysis row, or 200 with cached if recent) |
| `GET`  | `/v1/trades/:id/analysis` | get latest analysis for the trade |
| `GET`  | `/v1/ai/usage` | tenant's daily usage + budget |

Error codes:
- `AI_BUDGET_EXHAUSTED` (402) — over daily limit
- `AI_PROVIDER_ERROR` (502) — provider call failed
- `AI_ANALYSIS_PENDING` (still 202 — async)

---

## 7. Worker

Reuse `internal/worker.Pool` (Phase 1). Add `AnalysisJob` that the service enqueues:

```go
type AnalysisJob struct {
    AnalysisID uuid.UUID
    TenantID   uuid.UUID
    TradeID    uuid.UUID
}
```

Worker handler:
1. Load `trade_analyses` row
2. Mark `status=generating`
3. Build prompt (load trade + 5 recent + account stats)
4. Call `provider.Generate(...)`
5. Save result to row, mark `status=success` or `status=failed`
7. Increment `ai_usage_daily.tokens_in/out`

Failures:
- Provider error → `status=failed`, `error=<message>`, NO retry (LLM errors are usually deterministic; user can retry manually)
- Worker queue full → backpressure (Phase 1's existing pool handles)

---

## 8. Frontend

### 8.1 `useAnalysis` composable

```ts
export function useAnalysis() {
  async function analyze(tradeId: string): Promise<Analysis>
  async function getLatest(tradeId: string): Promise<Analysis | null>
  async function getUsage(): Promise<Usage>
}
```

### 8.2 `/trades/[id]` page — add AI Analysis panel

Below the existing trade details, add:
- "AI Analysis" header + "Generate" button
- Loading state (skeleton while `status=pending|generating`)
- Result panel: rendered text + model badge + "Regenerate" button
- "X / Y used today" indicator

---

## 9. Env vars

```
AI_PROVIDER=stub          # stub | openai | anthropic
OPENAI_API_KEY=
OPENAI_MODEL=gpt-4o-mini
ANTHROPIC_API_KEY=
ANTHROPIC_MODEL=claude-3-5-haiku-latest
AI_MAX_TOKENS=600
```

---

## 10. Definition of Done — Phase 6

- [ ] Migration 0008 applied; 2 tables exist
- [ ] `ai.Provider` interface + `Stub`/`OpenAI`/`Anthropic` impls
- [ ] `ai.Service` with daily-budget enforcement + idempotent cache (prompt_hash)
- [ ] Worker job via Phase 1's `worker.Pool`
- [ ] 3 HTTP endpoints (POST analyze, GET analysis, GET usage)
- [ ] FE: AI panel on `/trades/[id]` + useAnalysis composable
- [ ] Unit tests: stub provider (instant), OpenAI provider (httptest), prompt builder, idempotency, cache hit
- [ ] `go build ./...` + `go test -race -short ./...` + E2E (CI, testcontainers) all green
- [ ] AGENTS.md: Phase 6 complete; "Adding a new provider" guide; "Adding a new analysis type" guide

---

## 11. Implementation Order

1. **Design doc** (this file)
2. **Migration 0008** + domain + store
3. **Provider interface** + Stub + OpenAI + Anthropic impls + unit tests
4. **Service** with budget + idempotent cache + unit tests
5. **Worker job** wired to Phase 1's pool
6. **HTTP handlers** + router
7. **FE /trades/[id] panel** + composable
8. **E2E** (testcontainers) — kick analyze, stub returns content, see it persisted
9. **AGENTS.md + .env.example** + final test run

---

## 12. Out of Scope (Phase 6)

- Weekly/monthly recap
- Q&A chat with RAG
- Streaming responses
- Image input (trade screenshots)
- Prompt caching (OpenAI prompt cache, Anthropic cache layer)
- Multi-language analysis (English only for now)
- Voice narration