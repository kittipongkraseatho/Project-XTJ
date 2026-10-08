# Trading Journal — Phase 10 Design

| | |
|---|---|
| **Status** | ✅ Approved 2026-10-08 — implementation starting |
| **Date** | 2026-10-08 |
| **Phase** | 10 — Public API Write Scopes (write:trades + write:journal + write:webhooks + write:accounts) |
| **Scope** | Extend Phase 5's read-only public API with 4 write scopes. Idempotency-Key (UUID, required on POST) with 24h response replay cache. Per-action audit log row. |
| **Out of scope** | `write:recaps` (kicking recaps is admin-only — too niche) · `write:chat` (chat send is user-only — defer) · `delete:trades` (admin-only, destructive — defer) · Bulk operations (batch POST — defer) · Webhook delivery replay (separate from subscription CRUD — defer) |

---

## 1. Decisions Resolved

| Fork | Decision |
|---|---|
| Scope surface | **4 write scopes**: `write:trades`, `write:journal`, `write:webhooks`, `write:accounts` |
| Idempotency | **Required** `Idempotency-Key: <UUID>` header on every POST. 24h response replay cache per (api_key, key). |
| Audit log | **Per-action row** in `audit_logs` with `action='api_key_write'`, `resource_type`, `resource_id`, `payload_diff` (JSONB of request body) |
| Rate limit | Reuse Phase 5.1 per-key bucket — already in place. No new throttling. |
| Storage for idempotency | New table `api_key_idempotency` (UNIQUE on `api_key_id + idempotency_key`, FK to `api_keys`, 24h TTL via background sweep) |
| Idempotency scope | Per API key (NOT per tenant) — different keys may reuse the same UUID independently. |
| Conflict policy | Same key + different request body → 409 `IDEMPOTENCY_KEY_CONFLICT` (Stripe semantics). Same key + same body → return cached response. |
| Auth model | Same Phase 5 middleware (`apikey.Middleware(svc, bucket, scope)`) with new `write:*` scope constants. No new middleware. |
| Audit retention | Append-only (existing Phase 1 contract) — no delete. |

---

## 2. New Endpoints (under `/v1/public/*`)

### 2.1 Scope: `write:trades`

| Method | Path | Purpose | Auth scope |
|---|---|---|---|
| `POST` | `/v1/public/trades` | Create a single trade (manual entry) | `write:trades` |
| `PATCH` | `/v1/public/trades/:id` | Update trade fields (close_price, close_time, journal only — no destructive updates) | `write:trades` |

Body for POST:
```json
{
  "trading_account_id": "uuid",
  "symbol": "EURUSD",
  "direction": "long",
  "volume": 1.0,
  "open_price": 1.10,
  "open_time": "2026-10-01T09:30:00Z",
  "close_price": 1.11,
  "close_time": "2026-10-01T11:30:00Z",
  "magic": 12345,
  "metadata": {}
}
```

Notes:
- `tenant_id` is derived from the API key — never accepted in body.
- `id`, `created_at`, `updated_at` are server-generated.
- `outcome` is computed (win/loss/breakeven) by Phase 1 logic on close.
- `net_p_l` is computed by Phase 1 logic.
- Validation: required fields = `trading_account_id, symbol, direction, volume, open_price, open_time`.

Body for PATCH (only the listed fields are mutable):
- `close_price`, `close_time`, `magic`, `metadata`

### 2.2 Scope: `write:journal`

| Method | Path | Purpose | Auth scope |
|---|---|---|---|
| `PUT` | `/v1/public/trades/:id/journal` | Upsert trade journal (text + tags + image refs) | `write:journal` |

Body:
```json
{
  "text": "Discipline: scaled out at 1R, missed re-entry on pullback.",
  "tag_ids": ["uuid1", "uuid2"],
  "image_ids": ["uuid3"]
}
```

Behavior:
- If journal entry exists → update text + replace tag associations + replace image associations (in same tx).
- If not → create.
- Tag/image associations: delete old, insert new (idempotent — same set on retry produces same row state).

### 2.3 Scope: `write:webhooks`

| Method | Path | Purpose | Auth scope |
|---|---|---|---|
| `POST` | `/v1/public/webhooks/subscriptions` | Create webhook subscription | `write:webhooks` |
| `PATCH` | `/v1/public/webhooks/subscriptions/:id` | Update URL / events / status | `write:webhooks` |
| `DELETE` | `/v1/public/webhooks/subscriptions/:id` | Revoke subscription | `write:webhooks` |

Body for POST:
```json
{
  "url": "https://example.com/webhook",
  "description": "Zapier: trade log",
  "events": ["trade.created", "import.completed"]
}
```

Returns the new subscription row PLUS the plaintext secret (shown ONCE — same as Phase 5.2). Body:
```json
{
  "id": "uuid",
  "url": "...",
  "events": [...],
  "secret": "whsec_...",
  "secret_shown_once": true,
  "created_at": "..."
}
```

PATCH: only `url`, `description`, `events`, `is_active` mutable.
DELETE: soft-delete (`is_active = false` + `revoked_at = now()` — same as Phase 5.2 revoke). 204 on success.

### 2.4 Scope: `write:accounts`

| Method | Path | Purpose | Auth scope |
|---|---|---|---|
| `POST` | `/v1/public/accounts` | Create a trading account | `write:accounts` |
| `PATCH` | `/v1/public/accounts/:id` | Update display_name / currency / leverage / metadata | `write:accounts` |

Body for POST:
```json
{
  "broker": "manual",
  "broker_account_id": "ext-import-001",
  "display_name": "Manual Import",
  "currency": "USD",
  "leverage": 100,
  "kind": "manual"
}
```

`tenant_id` is derived from the API key.

PATCH: only `display_name`, `currency`, `leverage`, `metadata` mutable. `broker` and `broker_account_id` are immutable (they're the identity).

---

## 3. Idempotency

### 3.1 New table (Migration 0011)

```sql
CREATE TABLE api_key_idempotency (
    id                UUID PRIMARY KEY,
    api_key_id        UUID NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    idempotency_key   TEXT NOT NULL,
    request_hash      TEXT NOT NULL,        -- SHA256 of normalized request body
    response_status   INT  NOT NULL,
    response_body     JSONB NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at        TIMESTAMPTZ NOT NULL  -- now() + 24h
);

CREATE UNIQUE INDEX api_key_idempotency_key_idx
    ON api_key_idempotency (api_key_id, idempotency_key);

CREATE INDEX api_key_idempotency_expires_idx
    ON api_key_idempotency (expires_at);
```

### 3.2 Flow

```
POST /v1/public/... { Idempotency-Key: <UUID> }
  → 1. Hash request body (SHA256, normalized — sorted JSON keys)
  → 2. Lookup (api_key_id, idempotency_key) in api_key_idempotency
       a. Cache miss → proceed to handler; on response → store cache
       b. Cache hit + same request_hash → return cached response_status + body
       c. Cache hit + different request_hash → 409 IDEMPOTENCY_KEY_CONFLICT
  → 3. If no Idempotency-Key header → 400 IDEMPOTENCY_KEY_REQUIRED
```

### 3.3 Error codes

- `IDEMPOTENCY_KEY_REQUIRED` (400) — POST without header
- `IDEMPOTENCY_KEY_INVALID` (400) — not a valid UUID
- `IDEMPOTENCY_KEY_CONFLICT` (409) — same key + different body
- `IDEMPOTENCY_KEY_TOO_LONG` (400) — > 255 chars (defensive)

### 3.4 Background sweep

Phase 1's `worker.Pool` gets a new `IdempotencySweepJob` that runs every hour:

```go
DELETE FROM api_key_idempotency WHERE expires_at < now();
```

No application code path depends on this — it's a free-up disk only. The middleware's `expires_at` filter is the correctness boundary.

---

## 4. Audit Log

### 4.1 Existing `audit_logs` (Phase 1)

```sql
CREATE TABLE audit_logs (
    id          UUID PRIMARY KEY,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    action      TEXT NOT NULL,
    resource    TEXT NOT NULL,
    metadata    JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 4.2 Phase 10 usage

For every successful write action:

```go
auditLogRepo.Insert(ctx, audit_log.AuditLog{
    Action:   "api_key_write",
    Resource: fmt.Sprintf("%s:%s", resourceType, resourceID), // e.g. "trade:<uuid>"
    Metadata: jsonb.RawMessage(payloadDiffJSON),              // request body minus secrets
    UserID:   apiKey.OwnerUserID,                              // API key's owner
    TenantID: apiKey.TenantID,
})
```

Notes:
- `metadata` MUST exclude `secret` fields (webhook plaintext secret, API key secret) — strip before insert.
- Reads (`GET`) do NOT log (preserves Phase 1's read audit policy).
- Failed writes also do NOT log (only successes — the API key + IP are in slog already).

### 4.3 New helper

```go
// internal/service/audit/apikey_writer.go
func WriteAuditLog(ctx, svc, apiKey, resourceType, resourceID string, payload any) error
```

---

## 5. Scope Constants

`internal/domain/api_key/api_key.go` already has `AllScopes`. Append:

```go
const (
    ScopeReadAccounts   = "read:accounts"
    ScopeReadTrades     = "read:trades"
    ScopeReadAnalytics  = "read:analytics"
    ScopeReadPayments   = "read:payments"
    // ── Phase 10 — write scopes ──
    ScopeWriteTrades    = "write:trades"
    ScopeWriteJournal   = "write:journal"
    ScopeWriteWebhooks  = "write:webhooks"
    ScopeWriteAccounts  = "write:accounts"
)

var AllScopes = []string{
    ScopeReadAccounts, ScopeReadTrades, ScopeReadAnalytics, ScopeReadPayments,
    ScopeWriteTrades, ScopeWriteJournal, ScopeWriteWebhooks, ScopeWriteAccounts,
}
```

Shared package `packages/shared/src/index.ts` is updated to mirror.

---

## 6. Idempotency Middleware

### 6.1 Placement

`apikey.Middleware` already wraps `/v1/public/*` per-scope. Phase 10 wraps the same routes with a new `apikey.IdempotencyMiddleware(svc)` that:

1. Bails early on non-POST (idempotency only on POST — PUT/PATCH/DELETE are inherently idempotent by URL).
2. Reads `Idempotency-Key` header.
3. Validates UUID format.
4. Looks up cache via repo.
5. If hit + same hash → write cached response, return.
6. If hit + different hash → 409.
7. If miss → set context value `idempotency_key` for handler.
8. After handler returns → persist `(api_key_id, key, hash, status, body)` to cache.

### 6.2 Repo

```go
// internal/integration/apikey/idempotency_repo.go
type IdempotencyRepo struct{ pool DBTX }

func (r *IdempotencyRepo) Lookup(ctx, apiKeyID, key string) (*CachedResponse, error)
func (r *IdempotencyRepo) Store(ctx, apiKeyID, key, hash string, status int, body []byte, ttl time.Duration) error
```

### 6.3 Response capture

The middleware wraps the `echo.HandlerFunc` and captures the response body via a `bytes.Buffer` + `echo.ResponseRecorder`. This is the standard Echo pattern.

---

## 7. Public API key middleware — write-scope wiring

`internal/httpapi/phase5/public_handler.go` `PublicDeps` adds:

```go
type PublicDeps struct {
    // ... existing read handlers ...

    // ── Phase 10 — write handlers (thin wrappers around phase1/4/5.2 services) ──
    CreateTrade         func(c, in CreateTradeInput) (*trade.Trade, error)
    UpdateTrade         func(c, in UpdateTradeInput) (*trade.Trade, error)
    UpsertJournal       func(c, tradeID uuid.UUID, in UpsertJournalInput) (*JournalEntry, error)
    CreateWebhook       func(c, in CreateWebhookInput) (*WebhookCreated, error)
    UpdateWebhook       func(c, id uuid.UUID, in UpdateWebhookInput) (*WebhookSubscription, error)
    RevokeWebhook       func(c, id uuid.UUID) error
    CreateAccount       func(c, in CreateAccountInput) (*TradingAccount, error)
    UpdateAccount       func(c, id uuid.UUID, in UpdateAccountInput) (*TradingAccount, error)

    IdempotencyRepo     *apikey.IdempotencyRepo
}
```

Mount points in `RegisterRoutes`:
```go
pub.POST("/trades", apikey.Middleware(svc, bucket, apikey.ScopeWriteTrades), h.CreatePublicTrade)
pub.PATCH("/trades/:id", apikey.Middleware(svc, bucket, apikey.ScopeWriteTrades), h.UpdatePublicTrade)
pub.PUT("/trades/:id/journal", apikey.Middleware(svc, bucket, apikey.ScopeWriteJournal), h.UpsertPublicJournal)
pub.POST("/webhooks/subscriptions", apikey.Middleware(svc, bucket, apikey.ScopeWriteWebhooks), h.CreatePublicWebhook)
pub.PATCH("/webhooks/subscriptions/:id", apikey.Middleware(svc, bucket, apikey.ScopeWriteWebhooks), h.UpdatePublicWebhook)
pub.DELETE("/webhooks/subscriptions/:id", apikey.Middleware(svc, bucket, apikey.ScopeWriteWebhooks), h.RevokePublicWebhook)
pub.POST("/accounts", apikey.Middleware(svc, bucket, apikey.ScopeWriteAccounts), h.CreatePublicAccount)
pub.PATCH("/accounts/:id", apikey.Middleware(svc, bucket, apikey.ScopeWriteAccounts), h.UpdatePublicAccount)
```

Each handler is a thin wrapper that:
1. Derives `tenant_id` from the API key's context (not the body).
2. Validates input shape.
3. Calls the existing Phase 1 / 4 / 5.2 service.
4. Persists the idempotency cache (via the outer middleware).
5. Appends an audit log row on success.

---

## 8. Frontend

### 8.1 `useApiKeys` composable update

Add to the existing `useApiKeys.ts`:

```ts
const ALL_SCOPES: { value: string; label: string; group: 'read' | 'write' }[] = [
  { value: 'read:accounts',   label: 'Read accounts',     group: 'read' },
  { value: 'read:trades',     label: 'Read trades',       group: 'read' },
  { value: 'read:analytics',  label: 'Read analytics',    group: 'read' },
  { value: 'read:payments',   label: 'Read payments',     group: 'read' },
  { value: 'write:trades',    label: 'Create / update trades',    group: 'write' },
  { value: 'write:journal',   label: 'Upsert journal',            group: 'write' },
  { value: 'write:webhooks',  label: 'Manage webhooks',           group: 'write' },
  { value: 'write:accounts',  label: 'Create / update accounts',  group: 'write' },
]
```

### 8.2 `/api-keys` page — create dialog

- Group read vs write scopes with a divider.
- Show scope description (hover tooltip) so users know what each grants.
- "Generate with write scopes" preset button (selects all 4 write scopes).

### 8.3 Navigation

- No new top-level nav.
- Footer label → "Phase 10".

---

## 9. Env vars

None added. All work uses existing config.

---

## 10. Tests

### 10.1 Unit tests

| # | Test | What it covers |
|---|---|---|
| 1 | `TestIdempotencyRepo_StoreAndLookup` | round-trip |
| 2 | `TestIdempotencyRepo_ConflictDifferentHash` | same key + different body → returns Conflict sentinel |
| 3 | `TestIdempotencyMiddleware_Required` | POST without header → 400 |
| 4 | `TestIdempotencyMiddleware_InvalidUUID` | non-UUID header → 400 |
| 5 | `TestIdempotencyMiddleware_CacheHit_SameHash` | returns cached response |
| 6 | `TestIdempotencyMiddleware_CacheHit_DifferentHash` | 409 |
| 7 | `TestIdempotencyMiddleware_PUT_Bypasses` | PUT/DELETE skip idempotency (URL is the key) |
| 8 | `TestIdempotencySweep_DeletesExpired` | sweep job removes rows past `expires_at` |
| 9 | `TestPublicWriteHandler_TradeCreate` | POST /v1/public/trades with valid body + key |
| 10 | `TestPublicWriteHandler_JournalUpsert` | PUT /v1/public/trades/:id/journal |
| 11 | `TestPublicWriteHandler_WebhookCreate_OneShotSecret` | POST returns plaintext once |
| 12 | `TestPublicWriteHandler_AccountCreate` | POST /v1/public/accounts |
| 13 | `TestPublicWriteHandler_WrongTenant` | API key's tenant != resource tenant → 403 |
| 14 | `TestPublicWriteHandler_AuditLogRow` | success → audit_logs row inserted, secrets stripped |
| 15 | `TestAuditLog_StripsSecretFields` | metadata never includes secret |
| 16 | `TestScope_AllScopes_Len` | 8 scopes total (4 read + 4 write) |
| 17 | `TestScope_AllScopes_Present` | both read and write groups exposed |

### 10.2 Integration (CI testcontainers)

```go
//go:build integration

func TestPhase10_E2E_PublicAPIWriteLifecycle(t *testing.T) {
  // 1. Register tenant + user; mint API key with all 4 write scopes.
  // 2. POST /v1/public/trades (Idempotency-Key=A) → 201
  // 3. POST /v1/public/trades (Idempotency-Key=A) → 201 with same body (replay)
  // 4. POST /v1/public/trades (Idempotency-Key=A) with DIFFERENT body → 409
  // 5. PUT /v1/public/trades/:id/journal → 200
  // 6. POST /v1/public/webhooks/subscriptions → 201 (plaintext secret shown)
  // 7. POST /v1/public/accounts → 201
  // 8. Verify audit_logs rows for each success.
}
```

Skipped unless `INTEGRATION_TESTS=on`.

---

## 11. Definition of Done — Phase 10

- [ ] Migration 0011 applied; `api_key_idempotency` table exists with UNIQUE + expires_at index
- [ ] `ScopeWriteTrades`, `ScopeWriteJournal`, `ScopeWriteWebhooks`, `ScopeWriteAccounts` constants added to `api_key/api_key.go` and `shared/src/index.ts`
- [ ] `apikey.IdempotencyRepo` with `Lookup` + `Store`
- [ ] `apikey.IdempotencyMiddleware` enforces required header on POST, replay on cache hit, 409 on conflict
- [ ] `apikey.IdempotencySweepJob` registered with Phase 1's worker.Pool (hourly)
- [ ] 8 new public write endpoints wired in `phase5/public_handler.go` with correct scope guards
- [ ] Each handler: thin wrapper, derives tenant from API key, calls existing service, emits audit log
- [ ] `audit_logs` writes NEVER include `secret` fields
- [ ] `useApiKeys` exposes all 8 scopes; create dialog groups read vs write
- [ ] Footer → "Phase 10"
- [ ] 17 unit tests + 1 E2E (CI), all green
- [ ] `go build ./...` + `go test -race -short ./...` + `pnpm build` all green
- [ ] AGENTS.md: Phase 10 complete; "Adding a new write scope" guide; "Extending the idempotency middleware" guide

---

## 12. Implementation Order

1. **Design doc** (this file)
2. **Migration 0011** — `api_key_idempotency` table
3. **Scope constants** — extend `AllScopes` in api_key + shared
4. **IdempotencyRepo** + middleware + unit tests
5. **IdempotencySweepJob** + wire to Phase 1 pool
6. **Public write handlers** (8 endpoints) + audit log helper
7. **Router wiring** — mount 8 new routes with `write:*` scope guards
8. **FE: `useApiKeys` + /api-keys create dialog update**
9. **Footer → Phase 10**
10. **E2E test** (testcontainers)
11. **AGENTS.md + .env.example** + final test run + summary report

---

## 13. Out of Scope (Phase 10)

- `write:recaps` (kicking recaps is admin-only — too niche)
- `write:chat` (chat send is user-only — defer)
- `delete:trades` (admin-only, destructive — defer to admin API)
- Bulk operations (batch POST)
- Webhook delivery replay (separate from subscription CRUD)
- API key creation by another API key (chicken-and-egg — admin only)
- Trade deletion (admin only)
- Account deletion (admin only)
- Image upload via public API (Cloudinary signed upload is separate)