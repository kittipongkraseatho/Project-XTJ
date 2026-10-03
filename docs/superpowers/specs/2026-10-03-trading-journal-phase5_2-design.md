# Trading Journal — Phase 5.2 Design

| | |
|---|---|
| **Status** | ✅ Approved 2026-10-03 — implementation starting |
| **Date** | 2026-10-03 |
| **Phase** | 5.2 — Webhook Delivery (outbound) |
| **Scope** | Outbound webhooks · Tenant subscriptions · HMAC signing · Async retry · Event emitters (import.completed + trade.created) |
| **Out of scope** | Inbound webhooks · AI analysis · Real MetaApi SDK · Stripe webhook state machine · write:webhooks scope (still JWT-only) |

---

## 1. Decisions Resolved

| Fork | Decision |
|---|---|
| Event set (Phase 5.2) | **2 events**: `import.completed`, `trade.created` |
| Secret storage | **Plaintext in DB** (DB at-rest encryption protects at disk level — same approach as Phase 4 Stripe API keys) |
| Auth on admin endpoints | **JWT** (admin), no API-key access yet (`write:webhooks` scope deferred) |
| Delivery worker | **Reuse `internal/worker.Pool`** from Phase 1; add a `WebhookWorker` poller (60s, single-flight) |
| Retry policy | **Exponential backoff**: 1m, 5m, 30m, 2h, 12h (max 5 attempts → `exhausted`) |
| Retry classification | 2xx → success · 408/429/5xx/timeout → retry · other 4xx → no retry (client bug) |

---

## 2. New Tables (Migration 0007)

### 2.1 `webhook_subscriptions`

| Column | Type | Notes |
|---|---|---|
| `id` | UUID PK | UUIDv7 |
| `tenant_id` | UUID NOT NULL FK tenants CASCADE | tenant scope |
| `url` | TEXT NOT NULL | receiver endpoint |
| `event_types` | TEXT[] NOT NULL DEFAULT `{}` | `import.completed`, `trade.created` (extensible) |
| `secret` | TEXT NOT NULL | plaintext, shown ONCE; HMAC-SHA256 signing |
| `description` | TEXT | human label |
| `is_active` | BOOLEAN NOT NULL DEFAULT TRUE | soft delete via toggle |
| `created_by_user_id` | UUID NOT NULL FK users | audit |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| `updated_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| `revoked_at` | TIMESTAMPTZ | set when admin revokes |

Indexes:
- INDEX(`tenant_id`, `is_active`) — FE list
- INDEX(`tenant_id`) — emit query (lookup active subs for tenant + event_type)

### 2.2 `webhook_deliveries`

| Column | Type | Notes |
|---|---|---|
| `id` | UUID PK | UUIDv7 (idempotency-key for receiver) |
| `tenant_id` | UUID NOT NULL | denormalized for tenant scoping + audit |
| `subscription_id` | UUID NOT NULL FK webhook_subscriptions CASCADE | |
| `event_type` | TEXT NOT NULL | matches subscription's allowed types |
| `event_id` | TEXT NOT NULL | unique idempotency token (UUID per emit) |
| `payload` | JSONB NOT NULL | the signed body |
| `attempt` | INT NOT NULL DEFAULT 0 | current attempt number (1-indexed on first try) |
| `max_attempts` | INT NOT NULL DEFAULT 5 | |
| `status` | TEXT NOT NULL | `pending` / `success` / `failed` / `exhausted` |
| `response_code` | INT | nullable; last HTTP response code |
| `response_body` | TEXT | nullable; truncated to 4 KB |
| `error` | TEXT | nullable; "timeout", "dns_error", "tls_error" |
| `next_attempt_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | when worker should pick up |
| `last_attempted_at` | TIMESTAMPTZ | |
| `completed_at` | TIMESTAMPTZ | when terminal status set

Indexes:
- INDEX(`status`, `next_attempt_at`) WHERE status='pending' — worker poll
- INDEX(`subscription_id`, `created_at` DESC) — FE drill-down
- UNIQUE(`event_id`) — receiver idempotency

---

## 3. Event payload shape

```json
{
  "id": "<webhook-delivery-id>",
  "event": {
    "type": "trade.created",
    "id": "<unique-event-id>",
    "created_at": "2026-10-03T22:30:00Z"
  },
  "data": {
    "trade_id": "...",
    "tenant_id": "...",
    "trading_account_id": "...",
    "symbol": "EURUSD",
    "side": "buy",
    "open_time": "...",
    "close_time": "...",
    "profit_cents": 12345,
    "...": "..."
  }
}
```

### 3.1 HTTP headers

```
POST <subscription.url>
Content-Type: application/json
User-Agent: Trading-Journal-Webhooks/1.0
X-Webhook-Id: <delivery-id>
X-Webhook-Event: trade.created
X-Webhook-Timestamp: 1696367800
X-Webhook-Signature: sha256=<hex>
X-Webhook-Attempt: 1
```

Signature:
```
hex(hmac_sha256(secret, timestamp + "." + body))
```
Receiver verifies by recomputing and comparing (`subtle.ConstantTimeCompare`). Timestamp window: 5 minutes (anti-replay).

---

## 4. Service interface

```go
// internal/integration/webhook/service.go
type Service interface {
    Create(ctx, in CreateInput) (*CreateResult, error)        // returns plaintext secret ONCE
    List(ctx, tenantID) ([]*WebhookSubscription, error)
    Get(ctx, id) (*WebhookSubscription, error)
    Update(ctx, k) error
    Revoke(ctx, id) error

    // Emitter — called from anywhere; idempotent on event_id.
    Emit(ctx, in EmitInput) error  // enqueues delivery rows for every matching active sub

    // Delivery worker — called from WebhookWorker
    DeliverDue(ctx) ([]DeliveryResult, error)
}
```

`Emit` is the only non-admin entry point. It looks up all active subscriptions for the tenant whose `event_types` contains the event, and inserts one `webhook_deliveries` row per subscription (status=`pending`, `next_attempt_at=now()`).

`DeliverDue` picks due rows, performs the POST with HMAC signing, updates the row based on response.

---

## 5. WebhookWorker

`internal/worker/webhook_worker.go` — 60s poll, single-flight (same pattern as SyncWorker/ScheduleWorker):

1. `SELECT ... WHERE status='pending' AND next_attempt_at <= now() ORDER BY next_attempt_at LIMIT 100`
2. For each row: POST with HMAC, parse response
3. On 2xx: status=`success`, completed_at=now()
4. On retryable (408/429/network/5xx):
   - if attempt < max_attempts: status=`pending`, attempt++, next_attempt_at = exponential backoff
   - else: status=`exhausted`, completed_at=now()
5. On non-retryable 4xx: status=`failed`, completed_at=now() (the receiver is broken)

Single-flight guard: a sync.Mutex prevents the worker from picking rows another replica is working on (Phase 5.2 single-instance; Phase 6+ can use Postgres `SELECT ... FOR UPDATE SKIP LOCKED`).

---

## 6. HTTP endpoints (JWT-authed)

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/v1/webhooks` | list tenant's subscriptions |
| `POST` | `/v1/webhooks` | create (returns plaintext secret ONCE) |
| `GET` | `/v1/webhooks/:id` | fetch one |
| `PATCH` | `/v1/webhooks/:id` | update url / event_types / is_active |
| `DELETE` | `/v1/webhooks/:id` | revoke (soft-delete) |
| `GET` | `/v1/webhooks/:id/deliveries` | list recent deliveries (last 50, drill-down) |

---

## 7. Event emitters

Hooks added to existing service code:

- **import.completed** — `service.ImportService.RunPipeline` (after dealRepo.Insert batch succeeds) — emit once per import job with summary: `import_job_id`, `tenant_id`, `trading_account_id`, `deals_imported`, `positions_derived`, `duration_ms`
- **trade.created** — `internal/store` TradeRepo.Insert (after each successful trade insert) — emit once with trade summary

Both call `webhook.Emit(...)` synchronously. The Emit is best-effort — if it fails (DB error), we log WARN but don't block the import/insert (eventually-consistent semantics).

---

## 8. Frontend

### 8.1 `/webhooks` page

- Table: URL · events · description · active · last delivery
- "Create Webhook" button → dialog (URL + multi-checkbox events + description) → plaintext secret shown ONCE in copy-to-clipboard box
- Per-row "Revoke" button
- Click row → drill-down panel with recent deliveries (timestamp, status, response_code, error)

### 8.2 `useWebhooks` composable

Standard `list/create/update/revoke/listDeliveries` wrappers around `$fetch`.

### 8.3 Nav

Add "Webhooks" link under API Keys.

---

## 9. Definition of Done — Phase 5.2

- [ ] Migration 0007 applied; 2 new tables exist
- [ ] `webhook.Service` interface + impl: create/list/get/update/revoke + Emit
- [ ] HMAC-SHA256 signing with timestamp header; receiver verification documented
- [ ] WebhookWorker poll (60s, single-flight) + exponential backoff (1m, 5m, 30m, 2h, 12h)
- [ ] Event emitters in ImportService + TradeRepo.Insert
- [ ] 6 admin endpoints (JWT-authed) under `/v1/webhooks`
- [ ] FE `/webhooks` page with list, create (one-shot secret), revoke, deliveries drill-down
- [ ] Unit tests: HMAC signing math, retry classification, backoff schedule, scope check
- [ ] Integration test (testcontainers): create subscription, run import, verify POST arrives with expected headers + signature, revoke, verify next delivery skipped
- [ ] `go build ./...` + `go test -race -short ./...` + E2E test all green
- [ ] AGENTS.md: Phase 5.2 complete; "Adding a new event type" guide
- [ ] .env.example: `WEBHOOK_HTTP_TIMEOUT_SECONDS=10`, `WEBHOOK_MAX_ATTEMPTS=5`

---

## 10. Implementation Order

1. **Design doc** (this file)
2. **Migration 0007** + domain + store
3. **`webhook.Service`** — create/list/get/update/revoke + Emit + HMAC sign + unit tests
4. **WebhookWorker** — poll + retry + single-flight
5. **Emitters** — hook into ImportService + TradeRepo
6. **HTTP handlers** — 6 admin routes
8. **FE /webhooks** page + composable + nav
9. **E2E test** — testcontainers, local httptest server as receiver
10. **AGENTS.md + .env.example** + final test run

---

## 11. Out of Scope (Phase 5.2)

- AI analysis
- Real MetaApi SDK
- Stripe webhook state machine
- write:webhooks scope (admin endpoints are JWT-only)
- Per-tenant rate limit on webhook emission
- Webhook event filtering (e.g., "only emit for account X")
- Dead-letter queue UI for exhausted deliveries
- Bulk retry / replay