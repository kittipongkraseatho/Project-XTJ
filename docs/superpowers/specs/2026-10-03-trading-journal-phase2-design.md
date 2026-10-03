# Trading Journal — Phase 2 Design

| | |
|---|---|
| **Status** | ✅ Approved 2026-10-03 (2 forks resolved) — implementation starting |
| **Date** | 2026-10-03 |
| **Phase** | 2 — Broker Sync (Mock-only) |
| **Scope** | Provider interface + MockProvider · broker_connections + sync_runs · SyncService · SyncWorker (scheduled poll) · connect/disconnect/sync HTTP · FE connection UI + sync history |
| **Out of scope (later phases)** | Real MetaApi SDK adapter · Multi-account dashboard (Phase 3) · PDF reports (Phase 3) · Billing automation (Phase 4) |
| **Predecessor** | Phase 1 spec §10 + Phase 1 ImportService.WriteDeals dedup pipeline |

---

## 1. Decisions Resolved

| Fork | Decision |
|---|---|
| MetaApi token | **Mock-only** — defer real MetaApi SDK. Provider interface + MockProvider only. Architecture is MetaApi-ready; real adapter is one file swap later. |
| Provider shape | Interface in `internal/integration/broker/` with `Connect / Disconnect / FetchDeals / Ping`. Returns canonical `Deal` (minor units, same shape as parser output → feeds the same dedup pipeline). |

> **Why mock-first?** The user can demo the full sync flow (account connects → background poll → trades appear in dashboard) with zero external setup. The Provider interface is the only surface a future MetaApi adapter implements. Code stays MetaApi-shaped (external account ID, reconnect, polling interval) without the SDK dep.

---

## 2. New Tables (2 — Migration 0004)

| Table | Purpose | Notes |
|---|---|---|
| `broker_connections` | Link from tenant + trading_account to a provider (mock now, metaapi later) | `UNIQUE(tenant_id, trading_account_id, provider)` — one connection per account per provider |
| `sync_runs` | History of each sync attempt (queued → running → succeeded/failed) | append-only, drives the FE sync-history card |

ER (delta only — `trading_accounts` from Phase 1):

```
trading_accounts ──< broker_connections ──< sync_runs
                          │ provider = 'mock' (Phase 2) | 'metaapi' (later)
                          └─ sync_interval_seconds, last_sync_at, last_error, status
```

### 2.1 `broker_connections` columns

| Column | Type | Notes |
|---|---|---|
| `id` | UUID PK | UUIDv7 |
| `tenant_id` | UUID NOT NULL FK→tenants CASCADE | every query scopes by tenant |
| `trading_account_id` | UUID NOT NULL FK→trading_accounts CASCADE | one broker connection per account per provider |
| `provider` | TEXT NOT NULL | `'mock'` (Phase 2), `'metaapi'` (Phase 4+) |
| `status` | TEXT NOT NULL | enum: `pending` / `connecting` / `connected` / `disconnected` / `error` |
| `external_account_id` | TEXT NULL | provider-side id (set after `Connect()` succeeds) |
| `credentials_encrypted` | BYTEA NULL | phase 2: NULL for mock; AES-GCM wrapper ready for phase 3 |
| `metadata` | JSONB NOT NULL DEFAULT `{}` | provider-specific (e.g. server name, currency) |
| `last_sync_at` | TIMESTAMPTZ NULL | NULL on first connect; cursor for next FetchDeals |
| `last_error` | TEXT NULL | human-readable, never a secret |
| `sync_interval_seconds` | INT NOT NULL DEFAULT 60 | how often the SyncWorker polls this connection |
| `created_at` / `updated_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

### 2.2 `sync_runs` columns

| Column | Type | Notes |
|---|---|---|
| `id` | UUID PK | UUIDv7 |
| `tenant_id` | UUID NOT NULL FK→tenants CASCADE | every query scopes by tenant |
| `broker_connection_id` | UUID NOT NULL FK→broker_connections CASCADE | parent drill-down |
| `status` | TEXT NOT NULL | enum: `queued` / `running` / `succeeded` / `failed` |
| `trigger` | TEXT NOT NULL | enum: `manual` / `scheduled` / `reconnect` |
| `started_at` / `finished_at` | TIMESTAMPTZ NULL | NULL while queued |
| `deals_fetched` / `deals_imported` / `deals_skipped` | INT NOT NULL DEFAULT 0 | `skipped` = dedup hits on `(tenant_id, account_id, provider_deal_id)` |
| `error` | TEXT NULL | surfaced to UI; never includes credentials |
| `metadata` | JSONB NOT NULL DEFAULT `{}` | `from`, `to`, `duration_ms`, etc. |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |

### 2.3 Indexes

```sql
CREATE INDEX broker_connections_tenant_idx ON broker_connections(tenant_id, status);
CREATE INDEX broker_connections_due_idx ON broker_connections(status, last_sync_at)
  WHERE status = 'connected';  -- partial index for SyncWorker poll
CREATE INDEX sync_runs_connection_idx ON sync_runs(broker_connection_id, created_at DESC);
CREATE INDEX sync_runs_tenant_idx ON sync_runs(tenant_id, created_at DESC);
```

---

## 3. Provider Interface (`internal/integration/broker/`)

```go
// internal/integration/broker/provider.go

package broker

type Provider interface {
    Name() string

    // Connect authenticates with the broker. Mock: returns fake external_account_id.
    Connect(ctx context.Context, req ConnectRequest) (ConnectResult, error)

    // Disconnect tells the broker to release resources. Idempotent.
    Disconnect(ctx context.Context, externalAccountID string) error

    // FetchDeals returns canonical deals in [from, to). Deterministic for same args.
    // For Mock: deterministic seed = hash(external_account_id + from.UnixNano()).
    FetchDeals(ctx context.Context, req FetchDealsRequest) ([]Deal, error)

    // Ping checks the connection is still live. Used by SyncWorker pre-poll.
    Ping(ctx context.Context, externalAccountID string) error
}

type ConnectRequest struct {
    Login       string  // mock: ignored
    Password    string  // mock: ignored (never stored)
    Server      string  // mock: used in metadata
    Extra       map[string]string  // mock: ignored
}

type ConnectResult struct {
    ExternalAccountID string
    Metadata          map[string]any
}

type FetchDealsRequest struct {
    ExternalAccountID string
    From              time.Time  // inclusive
    To                time.Time  // exclusive
    Limit             int        // 0 = no cap
}

type Deal struct {
    ProviderDealID  string    // UUIDv5(uuid, externalAccountID+openTime+symbol+price)
    Symbol          string
    Direction       string    // "buy" | "sell"
    Volume          int64     // minor units (micro lots, ×100_000)
    OpenPrice       int64     // minor units (×100_000)
    ClosePrice      int64     // minor units (×100_000)
    OpenTime        time.Time
    CloseTime       time.Time
    Commission      int64
    Swap            int64
    Profit          int64
}
```

### 3.1 `MockProvider`

- `Name() == "mock"`
- `Connect()` — returns `uuid.NewV7().String()` as `ExternalAccountID`. Stores metadata `{server, seed}`.
- `Disconnect()` — no-op (returns nil).
- `Ping()` — always nil. (Fail mode wired but unused in Phase 2.)
- `FetchDeals()` — generates 1–3 deals per UTC day in `[from, to)`:
  - Symbol: random pick from `EURUSD, GBPUSD, USDJPY, XAUUSD, US30` (deterministic per seed)
  - OpenTime / CloseTime: spread across UTC trading hours
  - Direction: alternate buy/sell
  - Volume: 0.1–1.0 lot (micro units `10000`–`100000`)
  - Prices: walk ±50 pips from a fixed reference
  - Profit: derived from price walk × volume × tick size
  - Determinism: PRNG seeded by `hash(external_account_id + day_bucket)` so reconnecting the same account produces identical deals across syncs. **This is what lets Phase 2 dedup tests assert "second sync imports 0" with confidence.**

### 3.2 Registry

```go
// internal/integration/broker/registry.go

var Registry = map[string]Provider{
    "mock": NewMockProvider(),
    // "metaapi": NewMetaApiProvider(...),  // Phase 4 — gated by env
}
```

---

## 4. SyncService (`internal/service/sync_service.go`)

Orchestrates: `SyncWorker → SyncService.Run(sync_run_id)` → `Provider.FetchDeals` → `ImportService.WriteDeals` → update `broker_connection.last_sync_at`.

```
POST /v1/brokers/connections/{id}/sync
  → enqueue sync_run (status='queued')
  → worker picks up → SyncService.Run(sync_run_id):
        a1. sync_run = queued→running, started_at=now()
        a2. conn = broker_connections[id]
        a3. err := provider.Ping(external_account_id)
              if err → conn.status='error', sync_run=failed, return
        a4. deals := provider.FetchDeals(external_account_id, conn.last_sync_at, now)
        a5. import := ImportService.WriteDeals(ctx, conn.trading_account_id, deals)
              deals_imported = len(imported)
              deals_skipped  = len(deals) - len(imported)
        a6. conn.last_sync_at = now() (only if deals_imported + deals_skipped > 0)
        a7. sync_run = succeeded
```

### 4.1 Reusing `ImportService.WriteDeals`

Phase 1's `WriteDeals` is the right primitive — it takes `(tenantID, accountID, []parser.Deal)` and applies the dedup `INSERT ... ON CONFLICT (tenant_id, account_id, provider_deal_id) DO NOTHING`. Phase 2 adds a thin adapter `broker.Deal → parser.Deal` so the dedup path is identical whether deals come from a CSV upload or a broker API.

### 4.2 Errors surfaced to FE

| Provider error | Mapped to FE | HTTP |
|---|---|---|
| `Ping` fails | `last_error` field, sync_run.failed | 200 (sync queued) — sync itself failed |
| `FetchDeals` fails (5xx/transient) | sync_run retried up to 1× by worker; after 1 fail, `broker_connection.status='error'` | 200 (sync queued) |
| `FetchDeals` fails (4xx/auth) | `broker_connection.status='error'` immediately, no retry | 200 |
| Tenant ctx missing | 500 (programmer error) | 500 |

---

## 5. SyncWorker (`internal/worker/sync_worker.go`)

- Reuses the existing `worker.Pool` (channel dispatcher, 5min job timeout, 1 retry, Drain(15s)).
- Two trigger paths:
     - **Poll loop** — every 30s, scan `broker_connections WHERE status='connected' AND (last_sync_at IS NULL OR last_sync_at + sync_interval_seconds * interval '1 second' <= now())`. For each due connection, enqueue a `scheduled` sync_run.
     - **Manual** — HTTP handler enqueues a `manual` sync_run for the given connection_id.
- **Single-flight per connection**: a `sync_connection_lock{Type=advisory}` or in-process map keyed by `connection_id` prevents two workers running the same connection simultaneously. (Advisory locks via pg_try_advisory_xact_lock wrapped in tx, OR a process-local `sync.Map[string]struct{}` since Phase 2 has only 1 API process.)

> **Why process-local lock is OK in Phase 2**: single API process. Multi-instance API in Phase 4 (HPA) will switch to pg advisory locks — same interface, swap the implementation.

---

## 6. HTTP Contract

| Method | Path | Body | Returns | Notes |
|---|---|---|---|---|
| `GET` | `/v1/brokers/connections` | — | `[{id, provider, status, last_sync_at, last_error, trading_account_id, ...}]` | list for current tenant |
| `POST` | `/v1/brokers/connections` | `{trading_account_id, provider, login, password, server}` | `{id, status:'pending'}` | create + start connect (sync_enqueued by worker) |
| `GET` | `/v1/brokers/connections/{id}` | — | `{id, ..., last_sync_at, last_error, sync_interval_seconds}` | detail |
| `POST` | `/v1/brokers/connections/{id}/sync` | `{trigger?}` | `{sync_run_id}` | enqueue sync_run immediately |
| `GET` | `/v1/brokers/connections/{id}/sync-runs` | — | `[{sync_run, deals_imported, deals_skipped, status, started_at, finished_at}]` | history (paginated, latest 50) |
| `POST` | `/v1/brokers/connections/{id}/disconnect` | — | `{status:'disconnected'}` | sets status, zeroes external_account_id |
| `DELETE` | `/v1/brokers/connections/{id}` | — | `204` | cascade sync_runs; broker_connections row gone |

> **Plan limit (Phase 2)**: `Free` = 1 broker connection; `pro_99` = 3 (mirrors planned multi-account in Phase 3). Enforcement via `internal/entitlement` already in code.

---

## 7. Frontend

| Page | What |
|---|---|
| `/accounts` (Phase 1) | Add "Connect broker" button → opens `ConnectBrokerDialog` (provider dropdown = "Mock Broker (Phase 2 demo)", login/password/server fields disabled with tooltip "mock provider ignores creds"). On submit → POST `/v1/brokers/connections`. |
| `/accounts` (per row) | Show connection state badge (`connected` / `error` / `disconnected`), last_sync_at relative time, "Sync now" button → POST `/sync`, "Disconnect" button. Click into detail → sync history list (latest 50 sync_runs). |
| `/dashboard` | New `SyncStatusCard` showing all broker connections (count connected, count error, most recent sync). |

FE composables: `useBrokerConnections` (list + connect/disconnect/sync mutations) + `useSyncRuns` (paginated history).

---

## 8. Migration Commands (Phase 2)

```bash
cd apps/api
# same pattern as Phase 1: golang-migrate up/down with explicit +N
migrate -path migrations -database "$DATABASE_URL" up
# new version: 4
```

`infra/.env.example` adds:
```
# Phase 2 — broker sync
SYNC_POLL_INTERVAL_SECONDS=30
SYNC_JOB_TIMEOUT_SECONDS=300
```

---

## 9. Definition of Done — Phase 2

- [ ] Migration 0004 applied; `broker_connections` + `sync_runs` tables exist with indexes
- [ ] Provider interface + MockProvider implemented with deterministic PRNG
- [ ] SyncService reuses ImportService.WriteDeals for dedup (single source of truth)
- [ ] SyncWorker polls every 30s; single-flight per connection; drains on shutdown
- [ ] All 7 HTTP endpoints work with RBAC + tenant scoping
- [ ] `/accounts` page: connect Mock → sync runs appear → trades appear in dashboard
- [ ] `/dashboard` SyncStatusCard shows real connection state
- [ ] Unit tests: MockProvider determinism (same input → same deals), SyncService dedup reuse (second sync imports 0)
- [ ] `go build ./...` + `go test -race -short ./...` + `pnpm lint` + `pnpm build` all green
- [ ] AGENTS.md updated: Phase 2 done, "Adding a broker provider" extension guide

---

## 10. Implementation Order (Phase 2)

1. **Migration 0004** — `broker_connections` + `sync_runs` + indexes
2. **Domain entities** — `internal/domain/broker/{connection,deal}` + `internal/domain/sync/run`
3. **Store repos** — `internal/store/phase2_repos.go` (broker_connections + sync_runs)
4. **Provider interface + MockProvider** — `internal/integration/broker/{provider,registry,mock}.go` + unit tests
5. **SyncService** — `internal/service/sync_service.go` (uses ImportService.WriteDeals)
6. **SyncWorker** — `internal/worker/sync_worker.go` (reuses `worker.Pool`)
7. **HTTP handlers** — `internal/httpapi/phase2/` (7 endpoints) + wire route + RBAC
8. **FE composable** — `useBrokerConnections` + `useSyncRuns`
9. **FE pages** — ConnectBrokerDialog + sync history + SyncStatusCard
10. **Tests** — unit (mock determinism + sync dedup) + pnpm build/lint
11. **AGENTS.md + .env.example** updates

---

## 11. Out of Scope (Phase 2)

- MetaApi SDK adapter — interface is ready, real adapter is one file in Phase 4
- pg advisory locks — Phase 2 uses process-local map; Phase 4 multi-instance swap
- AES-GCM credential encryption — `credentials_encrypted` column reserved; Phase 2 mock has NULL
- Webhook-based push from broker — Phase 2 is poll-only
- Multi-account dashboard UI (Phase 3) — Phase 2 just stores the data; the dashboard comes in Phase 3
- Connection re-auth flow (refresh token expiry etc) — Phase 2 has no token concept (mock is immortal)