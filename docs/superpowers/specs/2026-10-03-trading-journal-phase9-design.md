# Trading Journal — Phase 9 Design

| | |
|---|---|
| **Status** | ✅ Approved 2026-10-05 — implementation starting |
| **Date** | 2026-10-05 |
| **Phase** | 9 — AI Expansion (Weekly Recap + Q&A Chat with RAG) |
| **Scope** | Build on Phase 6's `ai.Service` to add: (1) **weekly recap** (per-account, button-triggered, async) and (2) **Q&A chat** (multi-turn, RAG over trade history). Both reuse the same Provider interface + per-tenant daily budget. |
| **Out of scope** | Recap auto-email · Multi-account rollup · Period comparison · Local embedding model · Streaming · Function-calling agents · Voice input |

---

## 1. Decisions Resolved

| Fork | Decision |
|---|---|
| Recap scope | **Single-account weekly summary** (one recap per account per week). Multi-account rollup deferred. |
| Q&A architecture | **RAG** — embed user question + top-K relevant trades (7-day window) fed into LLM as context |
| Recap trigger | **Button only** — POST → async worker → poll. No ScheduleWorker. Email-out deferred. |
| Embedding source | **OpenAI `text-embedding-3-small`** (1536 dim, $0.02/1M tokens). Stub fallback when `OPENAI_API_KEY` unset (deterministic hash → substring match in SQL filter — quality low but zero dep). |
| Chat retention | **Last 50 turns per thread** — older turns dropped on insert; LLM context window = 50 turns + system prompt |
| Recap + Q&A vs per-trade budget | **Shared daily budget** — all AI generation (per-trade + recap + Q&A) draws from `ai_usage_daily`. 1 unit per kick-off. Cap unchanged (free=5, pro=50, hard=100). |
| Recap → reuse Phase 6? | **Yes** — recap uses same `ai.Service.Generate` pipeline with a different prompt + reuse `AnalysisJob` worker payload. New row in `ai_recaps` table mirrors `trade_analyses` shape. |
| Embedding persistence | **pgvector** extension + new `trade_embeddings` table (1 row per trade, embedding of trade-context text). Recomputed on trade insert/update (background hook). |
| Thread persistence | New tables `ai_chat_threads` + `ai_chat_messages` (multi-turn). Thread identified by `(tenant_id, account_id?, user_id, slug?)` — default = 1 thread per user per account. |

---

## 2. New Tables (Migration 0010)

### 2.1 `trade_embeddings`

Stores one embedding row per trade for RAG.

| Column | Type | Notes |
|---|---|---|
| `id` | UUID PK | UUIDv7 |
| `tenant_id` | UUID NOT NULL FK tenants CASCADE | |
| `trade_id` | UUID NOT NULL FK trades CASCADE UNIQUE | one embedding per trade |
| `embedding` | `vector(1536)` | pgvector — cosine distance |
| `context_text` | TEXT | raw text that was embedded (for debugging + fallback substring search) |
| `model_id` | TEXT NOT NULL | e.g. `text-embedding-3-small` |
| `embedded_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

Indexes:
- UNIQUE(`tenant_id`, `trade_id`)
- IVFFlat index on `embedding` (lists=100, `vector_cosine_ops`) — created with `CONCURRENTLY` outside migration tx
- INDEX(`tenant_id`, `embedded_at` DESC)

> pgvector extension: `CREATE EXTENSION IF NOT EXISTS vector;` prepended to migration. CI testcontainers uses `pgvector/pgvector:pg16` image.

### 2.2 `ai_recaps`

One row per recap kick-off (mirrors `trade_analyses` shape).

| Column | Type | Notes |
|---|---|---|
| `id` | UUID PK | UUIDv7 |
| `tenant_id` | UUID NOT NULL FK tenants CASCADE | |
| `account_id` | UUID NOT NULL FK trading_accounts CASCADE | single-account recap |
| `week_start` | DATE NOT NULL | Monday UTC of the target week |
| `week_end` | DATE NOT NULL | Sunday UTC |
| `prompt_hash` | TEXT NOT NULL | hash of (account_id + week_start + provider_model + trade_signature). Stale-cache detection. |
| `content` | TEXT | nullable while generating |
| `tokens_in` | INT | |
| `tokens_out` | INT | |
| `model_id` | TEXT NOT NULL | |
| `provider` | TEXT NOT NULL | |
| `status` | TEXT NOT NULL | `pending` / `generating` / `success` / `failed` |
| `error` | TEXT | |
| `requested_by_user_id` | UUID FK users | |
| `requested_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| `completed_at` | TIMESTAMPTZ | |

Indexes:
- INDEX(`tenant_id`, `account_id`, `week_start` DESC) — latest recap for an account+week
- INDEX(`tenant_id`, `status`, `requested_at`) — daily-budget counting

### 2.3 `ai_chat_threads`

| Column | Type | Notes |
|---|---|---|
| `id` | UUID PK | UUIDv7 |
| `tenant_id` | UUID NOT NULL FK tenants CASCADE | |
| `user_id` | UUID NOT NULL FK users CASCADE | thread owner |
| `account_id` | UUID FK trading_accounts CASCADE | optional — scopes Q&A to one account; null = tenant-wide |
| `title` | TEXT | auto-derived from first user message (first 60 chars) |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| `last_message_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | updated on each insert |

Indexes:
- INDEX(`tenant_id`, `user_id`, `last_message_at` DESC) — list user's threads
- UNIQUE(`tenant_id`, `user_id`, `account_id`) WHERE account_id IS NOT NULL — one default thread per user/account (partial unique). NULL account_id allowed multiple.

### 2.4 `ai_chat_messages`

| Column | Type | Notes |
|---|---|---|
| `id` | UUID PK | UUIDv7 |
| `tenant_id` | UUID NOT NULL FK tenants CASCADE | |
| `thread_id` | UUID NOT NULL FK ai_chat_threads CASCADE | |
| `role` | TEXT NOT NULL | `user` / `assistant` |
| `content` | TEXT NOT NULL | |
| `tokens_in` | INT | 0 for user; LLM tokens for assistant |
| `tokens_out` | INT | 0 for user; LLM tokens for assistant |
| `model_id` | TEXT | nullable for user messages |
| `provider` | TEXT | nullable for user messages |
| `context_trade_ids` | UUID[] | trade IDs used as RAG context (assistant only) |
| `requested_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

Indexes:
- INDEX(`tenant_id`, `thread_id`, `requested_at` ASC) — load thread history
- INDEX(`tenant_id`, `requested_at`) — daily-budget counting

> **Trim policy:** on each new assistant message, prune to keep last 50 messages per thread (delete oldest excess in same tx). Last 50 = "context window" passed to LLM.

---

## 3. Embedding pipeline

### 3.1 Embedder interface (mirrors `ai.Provider`)

```go
// internal/integration/ai/embedder.go

type Embedder interface {
    Name() string
    Embed(ctx context.Context, texts []string) ([][]float32, error)
    Dimensions() int  // 1536 for text-embedding-3-small
}

type EmbedResult struct {
    ModelID    string
    TokensIn   int
    LatencyMs  int
}
```

| Provider | Env vars | Library |
|---|---|---|
| `StubEmbedder` | none | FNV-1a hash → 1536-dim sparse vector (deterministic, no semantic) |
| `OpenAIEmbedder` | `OPENAI_API_KEY` | stdlib `net/http` to `POST /v1/embeddings` (no SDK, consistent with Phase 6/8) |

Default: stub. OpenAI enabled when `OPENAI_API_KEY` is non-empty AND `ai.Service` is configured to use it.

### 3.2 Embedding on trade insert/update

After a trade row is committed (Phase 1's import + Phase 8's broker sync both call `dealRepo.Insert`), enqueue a `TradeEmbedJob` to the worker pool:

```go
type TradeEmbedJob struct {
    TenantID  uuid.UUID
    TradeID   uuid.UUID
}

func (j TradeEmbedJob) Handle(ctx context.Context, deps EmbedDeps) error {
    // 1. Load trade + recent context (account stats, win rate)
    // 2. Build context_text (deterministic format, see §3.3)
    // 3. Call Embedder.Embed(ctx, [context_text])
    // 4. UPSERT trade_embeddings (tenant_id, trade_id) ON CONFLICT
    // 5. Update ai_usage_daily.embeddings_tokens (separate counter)
}
```

Cost: `text-embedding-3-small` = ~$0.02 per 1M tokens. A trade-context text is ~200 tokens. 1000 trades ≈ 200K tokens ≈ $0.004. Negligible.

### 3.3 Embedding context_text shape

```
Symbol: {symbol}
Side: {side}
Open: {open_time} UTC @ {open_price}
Close: {close_time} UTC @ {close_price}
P/L: {profit} (currency)
Volume: {volume}
Account: {account_name}
Notes: {journal_text or "none"}
```

Trimmed to fit ~200 tokens max. If longer, truncate journal_text.

### 3.4 RAG retrieval flow

```
1. User asks question → Q&A embeddings (1 call)
2. SELECT trade_id, embedding FROM trade_embeddings
   WHERE tenant_id = $1
     AND (account_id = $account_id OR $account_id IS NULL)  -- scoped to thread
     AND embedding <=> $query_vec  -- pgvector cosine distance
   ORDER BY embedding <=> $query_vec
   LIMIT 10;
3. Load full trade rows by id (for prompt)
4. Build chat prompt:
   - System role: "You are a trading analyst. Answer using the trade context below. Be concise."
   - Last 50 messages of thread (history)
   - Top-10 retrieved trades (compact table)
   - User question
5. Call ai.Provider.Generate → response
6. Persist user + assistant messages, snapshot context_trade_ids
```

**Stub fallback path** (when `OPENAI_API_KEY` empty):
- Skip embedding call
- SQL filter by date window (last 7 days) + `journal_text ILIKE '%<keyword>%'` extracted via simple keyword splitting
- Returns up to 10 trades; otherwise signals "no context — answer generically"

---

## 4. Weekly recap flow

```
POST /v1/accounts/:id/recaps { week_start: "2026-09-28" (YYYY-MM-DD, optional — defaults to last completed week) }
  → service.RecapAccount(ctx, RecapInput{...})
  → 1. Check daily budget (1 unit) — 402 if exhausted
  → 2. Compute prompt_hash = SHA256(account_id + week_start + sorted_trade_ids[:20] + model_id)
  → 3. If a SUCCESS recap with same prompt_hash exists → return 200 with cached content (idempotent)
  → 4. Insert ai_recaps row (status=pending)
  → 5. Enqueue RecapJob to worker.Pool
  → 6. Return 202 with row

Worker (RecapJob):
  → 1. Mark status=generating
  → 2. Load week's trades for account
  → 3. Compute account stats (win rate, avg P/L, best/worst trade)
  → 4. Build prompt (see §4.1)
  → 5. Call ai.Provider.Generate
  → 6. Save content, mark status=success/failed
  → 7. Increment ai_usage_daily.{tokens_in, tokens_out}
```

### 4.1 Recap prompt

System: "You are a trading analyst. Write a concise weekly recap (3-5 paragraphs) covering: (1) overall P/L and win rate, (2) notable wins, (3) notable losses, (4) patterns observed, (5) one suggestion for next week. Be specific with numbers."

User:
```
# Account: {account_name}
# Week: {week_start} to {week_end} ({trade_count} trades)

## Trades ({trade_count})
{compact table: open_time, symbol, side, P/L — 50 rows max, oldest→newest}

## Stats
Win rate: {win_rate}%
Avg P/L: {avg_pl}
Best trade: {best_trade_summary}
Worst trade: {worst_trade_summary}
Total P/L: {total_pl}

## Recap
```

### 4.2 Idempotency

`prompt_hash = SHA256(account_id + week_start + sorted(trade_id)[:20] + model_id)`. Detects:
- Same account + same week + same model → return cached
- New trades inserted into the week after recap → different hash → re-generation needed

Cache TTL: 30 minutes (matches Phase 6's analysis cache). After 30 min, regeneration is allowed even with same hash.

---

## 5. Q&A chat flow

```
POST /v1/chat/threads { account_id?: uuid }
  → 1. Find-or-create thread (UNIQUE(tenant_id, user_id, account_id) WHERE account_id IS NOT NULL)
  → 2. If account_id IS NULL → create new thread (multi-thread allowed)
  → 3. Return 201 { thread }

GET /v1/chat/threads
  → List user's threads (most recent first, paginated)

GET /v1/chat/threads/:id/messages?cursor=...
  → Return last 50 messages, ASC order, cursor-based pagination

POST /v1/chat/threads/:id/messages { content: "..." }
  → 1. Check daily budget (1 unit) — 402 if exhausted
  → 2. Insert user message (role=user)
  → 3. Enqueue ChatJob to worker.Pool (async, kick returns 202)
  → 4. Return 202 { user_message_id, status: "pending" }

Worker (ChatJob):
  → 1. Embed user message → query_vec
  → 2. RAG: retrieve top-10 relevant trades (§3.4)
  → 3. Load thread messages (last 50)
  → 4. Build chat prompt (system + history + RAG context + new question)
  → 5. Call ai.Provider.Generate
  → 6. Insert assistant message (role=assistant, content=response, context_trade_ids=[...])
  → 7. Trim thread to last 50 messages (delete excess in same tx)
  → 8. Update thread.last_message_at
  → 9. Increment ai_usage_daily.tokens_in/out
```

### 5.1 Chat prompt template

System: `You are a trading analyst. Answer the user's question using the trade context below. Reference specific trades when relevant. Be concise (2-4 paragraphs). If the context doesn't contain the answer, say so explicitly.`

```
# Recent trade context (top-10 most relevant)
{trade_contexts}

# Conversation history (most recent last)
{role: user|assistant, content}
...

# New question
{user_content}
```

---

## 6. Budget integration

Shared with Phase 6's `ai_usage_daily`. Add 2 columns:

```sql
ALTER TABLE ai_usage_daily
  ADD COLUMN recap_requests INT NOT NULL DEFAULT 0,
  ADD COLUMN chat_requests INT NOT NULL DEFAULT 0,
  ADD COLUMN embedding_tokens BIGINT NOT NULL DEFAULT 0;
```

Total daily budget = `requests + recap_requests + chat_requests <= cap` (where cap = `min(plan_budget, hard_cap)`).

Embeddings are tracked separately (no budget deduction) — they're cheap + idempotent.

---

## 7. Endpoints (JWT-authed, under `/v1/`)

| Method | Path | Purpose | Auth scope |
|---|---|---|---|
| `POST` | `/v1/accounts/:id/recaps` | Kick weekly recap (or specify `week_start`) | account owner |
| `GET`  | `/v1/accounts/:id/recaps?week_start=...` | List recaps for account | account owner |
| `GET`  | `/v1/recaps/:id` | Get recap (with content) | recap owner |
| `GET`  | `/v1/chat/threads` | List user's threads | user |
| `POST` | `/v1/chat/threads` | Create thread (`{account_id?}`) | user |
| `GET`  | `/v1/chat/threads/:id/messages` | List messages (paginated) | thread owner |
| `POST` | `/v1/chat/threads/:id/messages` | Send message (kick assistant reply) | thread owner |
| `DELETE` | `/v1/chat/threads/:id` | Delete thread (cascade messages) | thread owner |
| `GET`  | `/v1/ai/usage` | Tenant daily usage (existing Phase 6, extended) | tenant |

Error codes (new):
- `AI_RECAP_PENDING` (202) — async recap in progress
- `AI_CHAT_PENDING` (202) — async chat reply in progress
- `AI_BUDGET_EXHAUSTED` (402) — existing Phase 6, reused
- `CHAT_THREAD_NOT_FOUND` (404)

---

## 8. Worker integration

Reuse Phase 1's `worker.Pool`. Three new job kinds:

```go
// internal/worker/recap_job.go
type RecapJob struct {
    RecapID  uuid.UUID
    TenantID uuid.UUID
}

// internal/worker/chat_job.go
type ChatJob struct {
    MessageID uuid.UUID
    ThreadID  uuid.UUID
    TenantID  uuid.UUID
}

// internal/worker/trade_embed_job.go
type TradeEmbedJob struct {
    TenantID uuid.UUID
    TradeID  uuid.UUID
}
```

All enqueued via the same pool. Backpressure is automatic (Phase 1's 256-job buffer).

Embedding job is **fire-and-forget** (no async polling needed) — if it fails, next Phase 1 import or any Q&A retrieval re-embeds on demand. No retries; cheaper to skip than retry.

Recap + Chat jobs are user-visible (FE polls) — failures mark `status=failed` with error; user can retry.

---

## 9. Frontend

### 9.1 New composable: `useRecap`

```ts
export function useRecap() {
  async function generateRecap(accountId: string, weekStart?: string): Promise<Recap>
  async function listRecaps(accountId: string, weekStart?: string): Promise<Recap[]>
  async function getRecap(recapId: string): Promise<Recap>
}
```

### 9.2 New composable: `useChat`

```ts
export function useChat() {
  async function listThreads(): Promise<Thread[]>
  async function createThread(input?: { accountId?: string }): Promise<Thread>
  async function listMessages(threadId: string, cursor?: string): Promise<Message[]>
  async function sendMessage(threadId: string, content: string): Promise<Message>
  async function deleteThread(threadId: string): Promise<void>
}
```

### 9.3 New page: `/recaps`

- Top: account dropdown + week picker (defaults to last completed week)
- "Generate weekly recap" button (disabled if budget exhausted)
- Below: list of past recaps (accordion → expand to show content)
- "X / Y used today" budget indicator

### 9.4 New page: `/chat`

- Sidebar: thread list (last 10, click to switch + new-thread button)
- Main: message stream (user/assistant bubbles) + composer at bottom
- Composer: textarea + send button
- Streaming: NOT supported in Phase 9 — show typing indicator while `status=pending`, replace with full text when assistant message arrives (poll every 2s, up to 30s)
- Account scope selector (top right) — filters RAG context
- "X / Y used today" budget indicator

### 9.5 Navigation

Add nav links: `/recaps`, `/chat` in `app/layouts/default.vue` (existing nav group).

---

## 10. Env vars

```
# Embedding (Phase 9)
EMBED_PROVIDER=stub           # stub | openai
OPENAI_EMBED_MODEL=text-embedding-3-small
EMBED_BATCH_SIZE=20           # max texts per OpenAI request

# Existing (Phase 6, reused)
AI_PROVIDER=stub
OPENAI_API_KEY=
OPENAI_MODEL=gpt-4o-mini
AI_MAX_TOKENS=600
```

---

## 11. Tests

### 11.1 Unit tests (Go)

| # | Test | File |
|---|---|---|
| 1 | `TestStubEmbedder_Deterministic` | `embedder_stub_test.go` — same input → same vec |
| 2 | `TestStubEmbedder_Dimensions` | returns 1536 floats |
| 3 | `TestOpenAIEmbedder_Ok` | httptest mock `/v1/embeddings` |
| 4 | `TestOpenAIEmbedder_401` | returns ErrProviderNotConfigured |
| 5 | `TestRecapService_BudgetCheck` | over cap → 402 |
| 6 | `TestRecapService_IdempotentHash` | same trades+week → cached |
| 7 | `TestRecapService_DifferentHash` | new trade in week → new hash → re-gen |
| 8 | `TestChatService_ThreadCreate` | find-or-create semantics |
| 9 | `TestChatService_Trim50` | insert 60 → keeps last 50 |
| 10 | `TestChatService_RAGStub` | embedder=stub → SQL filter fallback |
| 11 | `TestRAG_CosineOrder` | closest vec first |
| 12 | `TestEmbedJob_FireForget` | failure → no panic, next request re-embeds |
| 13 | `TestRecapJob_PromptShape` | trades + stats in prompt |

### 11.2 Integration (CI testcontainers)

```go
//go:build integration

func TestPhase9_E2E_RecapThenChat(t *testing.T) {
  // 1. seed tenant + account + 10 trades in current week
  // 2. POST /v1/accounts/:id/recaps → 202
  // 3. wait worker tick → GET /v1/recaps/:id → status=success, content non-empty
  // 4. POST /v1/chat/threads → 201
  // 5. POST /v1/chat/threads/:id/messages "What's my win rate?" → 202
  // 6. wait worker → GET messages → assistant reply + context_trade_ids non-empty
  // 7. Verify ai_usage_daily.requests + recap_requests + chat_requests all bumped
}
```

Skipped unless `INTEGRATION_TESTS=on`.

---

## 12. Definition of Done — Phase 9

- [ ] Migration 0010 applied; 4 tables (`trade_embeddings`, `ai_recaps`, `ai_chat_threads`, `ai_chat_messages`) + 3 columns on `ai_usage_daily` exist
- [ ] pgvector extension enabled in CI testcontainers image
- [ ] `ai.Embedder` interface + Stub/OpenAI impls + unit tests
- [ ] `ai.RecapService` with idempotent cache + daily budget
- [ ] `ai.ChatService` with thread + trim-50 + RAG (embedder-based or SQL fallback)
- [ ] 3 worker job kinds wired to Phase 1's pool (recap, chat, embed)
- [ ] 9 HTTP endpoints
- [ ] FE: `/recaps` page + `/chat` page + `useRecap` + `useChat` composables + nav links + footer "Phase 9"
- [ ] 13+ unit tests + 1 E2E (CI testcontainers), all green
- [ ] `go build ./...` + `go test -race -short ./...` + `pnpm build` all green
- [ ] AGENTS.md: Phase 9 complete; "Adding a new embedding model" guide; "Adding new chat mode" guide
- [ ] .env.example: `EMBED_PROVIDER`, `OPENAI_EMBED_MODEL`, `EMBED_BATCH_SIZE`

---

## 13. Implementation Order

1. **Design doc** (this file) — `docs/superpowers/specs/2026-10-03-trading-journal-phase9-design.md`
2. **Migration 0010** — `apps/api/migrations/0010_phase9.up.sql` (pgvector + 4 tables + 3 ai_usage_daily columns)
3. **Embedder interface** + Stub + OpenAI impls + unit tests
4. **TradeEmbedJob** + worker wiring + enqueue hook in `dealRepo.Insert` (after Phase 1 import + Phase 8 sync)
5. **RecapService** + idempotent cache + daily budget + RecapJob + worker wiring + unit tests
6. **ChatService** + thread CRUD + trim-50 + RAG integration + ChatJob + worker wiring + unit tests
7. **HTTP handlers** (9 endpoints) + router wiring
8. **FE `/recaps` page** + `useRecap` composable
9. **FE `/chat` page** + `useChat` composable + message polling
10. **Nav links + footer** "Phase 9"
11. **E2E** (testcontainers) — recap + chat + budget integration
12. **AGENTS.md + .env.example** + final test run + summary report

---

## 14. Out of Scope (Phase 9)

- Recap auto-email (Phase 4's mailer exists, but no schedule trigger in Phase 9)
- Multi-account rollup recap
- Period comparison (this week vs last week)
- Streaming responses (FE polls until assistant message lands; no SSE/WS)
- Function-calling agents (Q&A is read-only — no trade mutations)
- Local embedding model (only OpenAI + Stub)
- Voice input
- Multi-language (English-only, consistent with Phase 6)
- Prompt caching (OpenAI prompt cache, Anthropic cache layer)
- Chat thread export