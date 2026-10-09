# AGENTS.md — Trading Journal Project Context

> Read this **before** doing any non-trivial work. It documents invariants for code, current phase scope, and where things live. Phase-specific agent docs may live alongside this one (e.g. `apps/api/AGENTS.md`, `apps/web/AGENTS.md`).

---

## Project shape

- **Monorepo:** `apps/api` (Go), `apps/web` (Nuxt 3), `packages/shared` (TS), `infra` (docker-compose + env), `docs/` (specs)
- **Language policy:** English for code, identifiers, log keys, error codes, commit messages. Free Thai for chat with the user (this repo's owner prefers Thai).
- **Brand:** "Trading Journal" (working name). Future split possible.

## Current phase

**Legacy Phase 0–10 platform delivery is implemented at capability level. The product roadmap is rebaselined to R0 Foundation → R5 Autonomous Trading; R0/R1 core functionality exists but release sign-off is partial, R2/R3 are partial, and R4/R5 are not started. First close the verification/release blockers, then start R2 Trading Intelligence with Behavior Intelligence and Strategy/Strategy Versioning.**

Canonical roadmap: `docs/roadmaps/2026-10-09-trading-journal-ai-trading-roadmap-th.md`

Implementation audit: `docs/roadmaps/2026-10-09-trading-journal-implementation-audit.md`

**Target safety boundary:** AI must not call MetaApi directly. Any future order must pass Strategy → Risk Engine → Execution Engine, with Backtest → OOS → Walk Forward → Paper Trading → User/Risk approval gates before Live. This rebaseline does not authorize order-execution work.

**Audit blockers to keep visible:** the root Makefile still hard-codes an old checkout path; API CI/local Postgres configuration does not consistently provide pgvector or enable Docker-gated E2E coverage; the current audit host has no Go executable; and several money-bearing Go domain structs use `float64` despite the money invariant. See the implementation audit for evidence and follow-up.

- Phase 0 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase0-design.md` — Foundation ✅ shipped
- Phase 1 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase1-design.md` — Free + ฿99 Upload Journal ✅ shipped
- Phase 2 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase2-design.md` — Mock broker sync ✅ shipped
- Phase 3 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase3-design.md` — Multi-account + Session/EA/Magic + PDF ✅ shipped
- Phase 4 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase4-design.md` — Billing (Stripe) + Real MetaApi + Email scheduling ✅ shipped
- Phase 5 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase5-design.md` — Public API + Scoped API Keys ✅ shipped
- Phase 5.1 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase5_1-design.md` — Redis rate limit + Integration test ✅ shipped
- Phase 5.2 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase5_2-design.md` — Outbound webhooks ✅ shipped
- Phase 6 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase6-design.md` — AI analysis ✅ shipped
- Phase 7 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase7-design.md` — Stripe webhook state machine ✅ shipped
- Phase 8 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase8-design.md` — Real MetaApi REST handshake (Connect + FetchDeals + Ping, no SDK) ✅ shipped
- Phase 9 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase9-design.md` — AI Expansion (weekly recap + Q&A chat with RAG) ✅ shipped
- Phase 10 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase10-design.md` — Public API Write Scopes + Idempotency-Key ✅ shipped

**Phase 5.1 in scope (delivered):** Extracted `ratelimit.KeyBucket` interface · renamed Phase 5 in-process impl to `MemoryKeyBucket` · new `RedisKeyBucket` (atomic Lua token-bucket, fail-open on Redis error, TTL-based GC) · `RATE_LIMIT_BACKEND=memory|redis` env-gated router swap · E2E integration test (testcontainers-go Postgres+Redis) covering 10-step API-key lifecycle: register → /me → mint → public-with-key → scope guard → burst → 429 → revoke → reuse-401 (revoked) → list-confirms-revoked. Skipped unless `INTEGRATION_TESTS=on` + Docker available; runs in CI.

**Phase 5.2 in scope (delivered):** 2 new tables (`webhook_subscriptions` + `webhook_deliveries`) · `webhook.Service` interface + `ServiceImpl` (HMAC-SHA256 signing with timestamp anti-replay; plaintext secret at rest, shown ONCE) · `webhook.EventType` (2 events: `import.completed`, `trade.created`) · `WebhookWorker` (30s poll, single-flight, retry classification: 408/429/5xx → retry, other 4xx → permanent fail; exponential backoff 1m/5m/30m/2h/12h, max 5 attempts) · Emitters added to `ImportService.RunPipeline` (one `import.completed` per job + one `trade.created` per trade) · 6 admin endpoints under `/v1/webhooks` (JWT-authed: list / get / create / patch / delete / deliveries drill) · FE `/webhooks` page (table + create dialog + one-shot plaintext display + revoke + click-to-drill deliveries) · `useWebhooks` composable + nav link + footer label → Phase 5.2.

**Phase 6 in scope (delivered):** 2 new tables (`trade_analyses` + `ai_usage_daily`) · `ai.Provider` interface with 3 impls (Stub / OpenAI / Anthropic — stdlib `net/http`, no SDK bloat) · `ai.Service` with idempotent prompt-hash cache (30min TTL) + per-tenant daily budget (free: 5/day, pro: 50/day, hard cap: 100/day) · async generation via Phase 1's `worker.Pool` (kick returns 202; FE polls) · 3 JWT-authed endpoints (`POST /v1/trades/:id/analyze`, `GET /v1/trades/:id/analysis`, `GET /v1/ai/usage`) · FE: AI Analysis panel on `/trades/[id]` (Generate button + polling + budget indicator + provider badge).

**Phase 7 in scope (delivered):** new `webhook_events` table (UNIQUE on `stripe_event_id` for idempotency) · `billing.Service` interface gains `HandleWebhookEvent` · `StripeService.HandleWebhookEvent` handles 5 event types: `checkout.session.completed`, `customer.subscription.updated`, `customer.subscription.deleted`, `invoice.paid`, `invoice.payment_failed` · upsert-by-external-id on `subscriptions` + `payments` (existing UNIQUE columns reused) · `StubService.HandleWebhookEvent` is a no-op · `/v1/billing/webhook` now applies events after signature verify (200 on idempotent repeat, 200 on unknown type with log, 500 on transient error → Stripe retries).

**Phase 8 in scope (delivered):** `MetaApiAdapter` rewritten to talk to MetaApi Cloud REST directly (no third-party SDK, stdlib `net/http`). `Connect` does list → find-or-create → poll-DEPLOYED with config-gated timeout (`METAAPI_DEPLOY_TIMEOUT_SECONDS`, default 10s). `FetchDeals` walks history-time-buckets (`/users/current/accounts/:id/history-orders/time-range` per page) then fetches each ticket via `/users/current/accounts/:id/history-orders/:ticket` and maps to canonical `broker.Deal` shape (minor units, UTC, `UUIDv5(ProviderDealID)`). `Ping` checks the account state via `GET /users/current/accounts/:id/account-information` and treats anything other than `DEPLOYED` as not-ready. `Disconnect` is a no-op (REST has no streaming teardown in Phase 8). Auth header is `auth-token: <METAAPI_TOKEN>` per MetaApi REST convention. **No migration needed** — Phase 8 only swaps the `MetaApiAdapter` impl behind the existing `Provider` interface; `broker_connections` schema and SyncService dedup contract are unchanged. 9 unit tests via `httptest.NewServer` cover: finds-existing / create-when-missing / poll-until-deployed / maps-to-canonical / empty-history / ping-OK / ping-NotDeployed / unauthorized / empty-token. `cfg.MetaApiEnabled()` gating is unchanged — set `METAAPI_TOKEN=<real-token>` to flip from Phase 4's stub to the real REST adapter.

**Phase 9 in scope (delivered):** `ai.Embedder` interface (Stub + OpenAI text-embedding-3-small via stdlib `net/http`, no SDK). 4 new tables via migration `0010_phase9` (`trade_embeddings` with pgvector `vector(1536)`, `ai_recaps`, `ai_chat_threads`, `ai_chat_messages`) + 3 new columns on `ai_usage_daily` (`recap_requests`, `chat_requests`, `embedding_tokens`). **RecapService**: single-account weekly summary, button-triggered, async via Phase 1's worker.Pool. SHA256 prompt-hash idempotent cache (30min TTL); budget-deducted from shared daily budget (free=5, pro=50, hard cap=100 across all surfaces). **ChatService**: multi-turn Q&A with RAG — embed user message, pgvector cosine search top-10 relevant trades, feed into LLM prompt with last-50 thread history, async worker handler. Threads find-or-create by `(user, account)` partial unique; trim to last 50 messages per thread. Shared budget with Phase 6 per-trade analyses (one cap across per-trade + recaps + chat). 9 HTTP endpoints under `/v1/accounts/:id/recaps`, `/v1/recaps/:id`, `/v1/chat/threads`, `/v1/chat/threads/:id/messages`. FE: `/recaps` page (account + week picker + polling + budget indicator), `/chat` page (sidebar threads + message stream + composer + account-scope RAG). E2E test in `internal/integration/ai/phase9_e2e_test.go` (testcontainers-go pgvector + Redis, gated by `INTEGRATION_TESTS=on`). **CI must use `pgvector/pgvector:pg16` image** — managed Postgres needs `CREATE EXTENSION IF NOT EXISTS vector;` before applying migration.

**Phase 10 in scope (delivered):** 4 new write scopes (`write:trades`, `write:journal`, `write:webhooks`, `write:accounts`) extending Phase 5's read-only public API. New table `api_key_idempotency` (UNIQUE on `api_key_id + idempotency_key`, 24h TTL via background sweep). **`apikey.IdempotencyMiddleware`** (Stripe-style replay): POST requires `Idempotency-Key: <UUID>` header → 400 if missing/invalid; same key + same body returns cached response (200) with `Idempotent-Replay: true` header; same key + different body returns 409 `IDEMPOTENCY_KEY_CONFLICT`. PUT/PATCH/DELETE bypass (URL is the idempotency key). **`IdempotencySweeper`** runs hourly in `server.New` to delete expired rows. 8 new endpoints under `/v1/public/*` (POST/PATCH trades, PUT journal, POST/PATCH/DELETE webhooks, POST/PATCH accounts). Each successful write emits an `audit_logs` row with `action='api_key_write'`, `resource_type`, `resource_id`, and redacted payload (webhook secret never logged). Phase 5 admin `/v1/api-keys` accept-list now includes the 4 write scopes; FE `useApiKeys` groups them read/write with a "Select all write scopes" preset in the create dialog. E2E test in `internal/integration/apikey/phase10_e2e_test.go` exercises: required header → 400; same key + same body → 201 + replay header; same key + different body → 409; webhook secret returned only once.

**Deferred (Phase 11+ — outside initial master plan):**
- `write:recaps` (kicking recaps is admin-only — too niche)
- `write:chat` (chat send is user-only — defer)
- `delete:trades` (admin-only, destructive — defer to admin API)
- Bulk operations (batch POST)
- Webhook delivery replay (separate from subscription CRUD)
- API key creation by another API key (chicken-and-egg — admin only)
- More webhook event types (Phase 5.2: import.completed + trade.created only)
- Per-key (per-user) rate-limit override
- Inbound webhooks (Phase 5.2 is outbound only)
- Webhook event filtering (e.g. "only emit for account X")
- Bulk retry / replay of exhausted deliveries
- AI: streaming responses, function-calling agents, image input (trade screenshots), prompt caching, multi-language analysis, voice input, chat thread export
- Recap auto-email, multi-account rollup, period comparison (this week vs last week)
- Local embedding model (Phase 9 ships OpenAI + Stub only)
- Stripe Connect (multi-party payments — never planned)
- MetaApi streaming (REST polling is enough for current deal history; Phase 8 chose no SDK to avoid bloat — streaming subs are a future option)

> If a task crosses into a deferred area, **stop** and surface it. Don't expand scope.

## Stack & conventions

### Go (apps/api)

- Go 1.23+
- **Layering:** `httpapi.Handler` → `service` → `store` (sqlc-generated + thin wrappers). Domain entities in `internal/domain/<entity>/`. Cross-layer imports`httpapi → domain + service + store`, `service → domain + store`, `store → domain`. **Never** import backwards.
- **Tenant context:** every query scoped by `tenant_id` read from `context.Context` via `internal/tenant/ctx.go` typed key. Missing ctx → 500 (programmer error).
- **Errors:** typed `*domain.Error` with `Code`, `Message`, `Details`. Mapped uniformly to `{error: {code, message, details}}` envelope. Never leak internal error text to client.
- **Logging:** `slog` JSON to stdout. Always include `request_id`, `user_id`, `tenant_id` when available. **Never** log passwords, tokens, refresh tokens, raw PII.
- **Money:** `numeric(18,8)` + minor units (cents). Never float.
- **Time:** UTC `timestamptz` in DB; Bangkok display in FE.
- **IDs:** UUIDv7 (sortable, no order leak). `gofrs/uuid` v7.
- **JWT:** EdDSA / Ed25519. Keypair on disk (`apps/api/keys/ed25519.pem`); `kid="v1"`.
- **Refresh token:** rotating opaque 32-byte base64url, sha256-hashed in DB. Reuse detection revokes ALL active sessions.
- **Tests:** testify + testcontainers-go (production) or embedded-postgres + miniredis (local when no Docker). 70%+ coverage in `auth`, `tenant`, `observability`, `integration/oauth` packages.

### Vue/Nuxt (apps/web)

- Nuxt 3 + Vue 3 + Vuetify 3 + Pinia + TypeScript
- **Dark theme default.** Palette: bg `#0B0F19`, surface `#121826`, primary `#3B82F6`.
- **State per domain** (Pinia stores). `auth` store = single source of truth for `user`, `accessToken`, `currentTenant`.
- **HTTP** via `$fetch` in `useApi` composable: injects Bearer, maps errors → typed `ApiError`, auto-refresh on 401 once.
- **Forms:** VeeValidate 4 + Zod.
- **Routing:** file-based; `auth.global.ts` redirects unauth to `/login`.
- **CSP:** `'self' + res.cloudinary.com` for images (Phase 1 added Cloudinary; later phases add more).
- **PWA:** `@vite-pwa/nuxt`. `autoUpdate`. Icons in `public/icons/`. Manifest aligns with dark palette.
- **i18n:** English-only in Phase 0–3. Thai copy inline as needed; full i18n in Phase 4.
- **Broker sync (Phase 2 + Phase 8):** `Provider` interface in `internal/integration/broker/`. Phase 2 shipped the mock-only adapter (`MockProvider` for local dev). Phase 8 added the real `MetaApiAdapter` (REST via stdlib `net/http`, no SDK) with `Connect` (list → find-or-create → poll-DEPLOYED), `FetchDeals` (history-orders time-range paginated + per-ticket fetch), and `Ping` (account-state check). Auth header `auth-token: <METAAPI_TOKEN>`. Wire-time gated by `cfg.MetaApiEnabled()` (`METAAPI_TOKEN` non-empty). SyncWorker reuses the Phase 1 worker.Pool; SyncService reuses `dealRepo.Insert` for dedup (single source of truth for both manual uploads and broker syncs).
- **Plan limits (Phase 3):** `internal/entitlement/limits_by_plan.go` maps `plan.Code` to `Limits{MaxTradingAccounts, MaxBrokerConnections}`. Handlers (`CreateAccount`, `CreateConnection`) call `entitlement.LookupForTenant(ctx, store, tenantID)` and 402 on overflow.
- **PDF reports (Phase 3):** `pdfreport.Service` interface in `internal/integration/pdfreport/`. Implementation uses `go-pdf/fpdf` (pure Go, MIT, built-in PDF base 14 fonts — no TTF payload bundled).
- **Billing (Phase 4):** `billing.Service` interface in `internal/integration/billing/`. Stub impl for local dev; Stripe impl via `stripe-go/v9` (real webhook signature verify). Phase 7 added `HandleWebhookEvent` to apply 5 event types (`checkout.session.completed`, `customer.subscription.updated`, `customer.subscription.deleted`, `invoice.paid`, `invoice.payment_failed`) with idempotency via `webhook_events` UNIQUE on `stripe_event_id`. Stub service returns no-op.
- **Mailer (Phase 4):** `mailer.Service` interface in `internal/integration/mailer/`. Stub writes `.eml` files to a temp dir; production uses `SMTPMailer` via net/smtp.
- **Scheduling (Phase 4):** pure cron helper `ComputeNextSend` + `ScheduleWorker` (60s poll, in-process single-flight). New `reportbuild` package shares trade-walking logic between HTTP `/v1/reports/pdf` and the worker.
- **API keys (Phase 5 + 10):** `apikey.Service` interface in `internal/integration/apikey/`. Implementation uses `golang.org/x/crypto/bcrypt` (cost=10). Key format `trj_pk_<8-char-prefix>.<24-byte-secret>` (URL-safe base64); the `prefix` is the public lookup key, the `secret` is bcrypt-hashed and never recoverable. Echo middleware sets tenant_id on both request ctx (`tenant.WithTenantID`) and echo ctx (`c.Set("tenant_id")`) — the former for service/store paths, the latter for existing phase1/4 handlers. Per-key rate limit via `internal/ratelimit/KeyBucket` (in-process `golang.org/x/time/rate.Limiter` map; Phase 5.1 swaps for Redis). **Phase 10 adds `apikey.IdempotencyMiddleware`** (Stripe-style replay, 24h TTL via `IdempotencySweeper`) — POST endpoints under `/v1/public/*` require `Idempotency-Key: <UUID>`. 8 write endpoints (write:trades/journal/webhooks/accounts) compose existing phase1/4/5.2 handlers under the same auth model. Admin endpoints under `/v1/api-keys` are JWT-authed for management (list/create/get/update/revoke) — the create/update accept-list now includes all 8 scopes (4 read + 4 write).
- **Rate limit (Phase 5.1):** `ratelimit.KeyBucket` interface with two implementations. `MemoryKeyBucket` (in-process `sync.Mutex` + `rate.Limiter` per key) — default for dev. `RedisKeyBucket` (atomic Lua token-bucket in Redis; fail-open on Redis error with `slog.Warn`; TTL-based cleanup) — set via `RATE_LIMIT_BACKEND=redis` for prod multi-instance. Both return `Decision{Allowed, Remaining, Reset}`. E2E integration test in `internal/integration/apikey_e2e/` (testcontainers-go Postgres+Redis) exercises the full 10-step API-key flow; gated by `INTEGRATION_TESTS=on`.
- **AI analysis (Phase 6):** `ai.Provider` interface with 3 implementations (`StubProvider`, `OpenAIProvider`, `AnthropicProvider`) — all using stdlib `net/http` (no SDK bloat). `ai.Service` orchestrates prompt-hash idempotency, daily budget (free: 5/day, pro: 50/day, hard cap: 100/day), and async generation via Phase 1's `worker.Pool`. Selection: `AI_PROVIDER=stub|openai|anthropic` env var. Defaults to stub. Per-tenant usage tracked in `ai_usage_daily` table.
- **AI Expansion (Phase 9):** builds on Phase 6 with `ai.Embedder` interface (Stub + OpenAI text-embedding-3-small via stdlib `net/http`) for RAG. New `ai.RecapService` (single-account weekly summary) + `ai.ChatService` (multi-turn Q&A with RAG via pgvector cosine search over `trade_embeddings`). Shared daily budget — all 3 AI surfaces (per-trade + recap + chat) draw from the same `ai_usage_daily` cap (free=5, pro=50, hard=100). 3 new worker jobs (`TradeEmbedJobPayload`, `RecapJobPayload`, `ChatJobPayload`) wired to the same Phase 1 `worker.Pool`. Selection: `EMBED_PROVIDER=stub|openai` env var; OpenAI auto-replaces Stub when `OPENAI_API_KEY` is set. Stub fallback uses "last 10 trades for the account" (deterministic SQL filter). pgvector extension required in DB — CI uses `pgvector/pgvector:pg16` image.
- **Tests:** Vitest + `@vue/test-utils` for unit, Playwright for E2E.

### Cross-stack

- **Shared types:** `packages/shared/src/index.ts` exports role enums, error codes, plan DTO.
- **API contract:** REST + JSON, `/v1/` prefix. Error envelope uniform.
- **OAuth:** Authorization Code + PKCE. `code_verifier` server-side (in `oauth_states`). State token = 32-byte base64url, 10-min TTL.
- **Account linking:** auto-link by verified email. Audit `user.identity_linked`.

## Where to find things

| What | Path |
|---|---|
| Phase 0 design | `docs/superpowers/specs/2026-10-03-trading-journal-phase0-design.md` |
| Phase 1 design | `docs/superpowers/specs/2026-10-03-trading-journal-phase1-design.md` |
| Phase 2 design | `docs/superpowers/specs/2026-10-03-trading-journal-phase2-design.md` |
| Phase 3 design | `docs/superpowers/specs/2026-10-03-trading-journal-phase3-design.md` |
| Phase 4 design | `docs/superpowers/specs/2026-10-03-trading-journal-phase4-design.md` |
| Phase 5 design | `docs/superpowers/specs/2026-10-03-trading-journal-phase5-design.md` |
| Phase 5.1 design | `docs/superpowers/specs/2026-10-03-trading-journal-phase5_1-design.md` |
| Phase 5.2 design | `docs/superpowers/specs/2026-10-03-trading-journal-phase5_2-design.md` |
| Phase 6 design | `docs/superpowers/specs/2026-10-03-trading-journal-phase6-design.md` |
| Phase 7 design | `docs/superpowers/specs/2026-10-03-trading-journal-phase7-design.md` |
| Phase 8 design | `docs/superpowers/specs/2026-10-03-trading-journal-phase8-design.md` |
| Phase 9 design | `docs/superpowers/specs/2026-10-03-trading-journal-phase9-design.md` |
| Phase 10 design | `docs/superpowers/specs/2026-10-03-trading-journal-phase10-design.md` |
| Spec source (MASTER PROMPT) | `pasted-text.txt` in workspace root or repo root |
| Migrations | `apps/api/migrations/` (0001_init, 0002_oauth, 0003_phase1, 0004_phase2, 0005_phase4, 0006_phase5, 0007_phase5_2, 0008_phase6, 0009_phase7, 0010_phase9, 0011_phase10) |
| sqlc queries | `apps/api/queries/` |
| Domain entities | `apps/api/internal/domain/<entity>/` |
| HTTP handlers | `apps/api/internal/httpapi/` |
| Middleware chain | `apps/api/internal/server/` |
| OAuth providers | `apps/api/internal/integration/oauth/` |
| Broker providers | `apps/api/internal/integration/broker/` |
| Billing providers | `apps/api/internal/integration/billing/` |
| Mailer | `apps/api/internal/integration/mailer/` |
| PDF reports | `apps/api/internal/integration/pdfreport/` |
| MT5 parser | `apps/api/internal/parser/` |
| Worker pool | `apps/api/internal/worker/` |
| SyncService | `apps/api/internal/syncsvc/` |
| ScheduleService | `apps/api/internal/service/report_schedule/` |
| Report build (shared) | `apps/api/internal/reportbuild/` |
| Plan limits | `apps/api/internal/entitlement/` |
| Cloudinary integration | `apps/api/internal/integration/cloudinary/` |
| API keys | `apps/api/internal/integration/apikey/` |
| Rate limit | `apps/api/internal/ratelimit/` |
| Webhooks (outbound) | `apps/api/internal/integration/webhook/` + `apps/api/internal/worker/webhook_worker.go` |
| AI analysis | `apps/api/internal/integration/ai/` + `apps/api/internal/worker/analysis_job.go` |
| Phase 1 handlers | `apps/api/internal/httpapi/phase1/` |
| Phase 2 handlers | `apps/api/internal/httpapi/phase2/` |
| Phase 3 handlers | `apps/api/internal/httpapi/phase3/` |
| Phase 4 handlers | `apps/api/internal/httpapi/phase4/` |
| Phase 5 handlers | `apps/api/internal/httpapi/phase5/` |
| Phase 6 (webhooks) handlers | `apps/api/internal/httpapi/phase6/` |
| Phase 9 (recap + chat) handlers | `apps/api/internal/httpapi/phase9/` |
| Phase 1 store repos | `apps/api/internal/store/phase1_repos.go` |
| Phase 2 store repos | `apps/api/internal/store/phase2_repos.go` |
| Phase 3 store repos | `apps/api/internal/store/phase3_repos.go` |
| Phase 4 store repos | `apps/api/internal/store/phase4_repos.go` |
| Phase 5 store repos | `apps/api/internal/store/phase5_repos.go` |
| Phase 5.2 store repos | `apps/api/internal/store/phase7_repos.go` |
| Phase 6 store repos | `apps/api/internal/store/phase8_repos.go` |
| Phase 7 store repos | `apps/api/internal/store/phase9_repos.go` |
| Phase 9 store repos | `apps/api/internal/store/phase10_repos.go` |
| FE pages | `apps/web/app/pages/` |
| FE composables | `apps/web/app/composables/` |
| FE stores | `apps/web/app/stores/` |
| PWA manifest config | `apps/web/nuxt.config.ts` (under `pwa:`) |
| docker-compose | `infra/docker-compose.yml` |
| CI workflows | `.github/workflows/` |
| Env template | `infra/.env.example` |
| Root Makefile | `Makefile` (root) |

## Extending

- **Adding a new endpoint:** `internal/httpapi/<area>/handler.go` (Echo handler) → `internal/httpapi/<area>/dto.go` (request/response types) → `internal/service/<area>/<usecase>.go` (orchestration) → `internal/store/<table>.sql` (sqlc query) → `internal/store/<table>.go` (thin wrapper). Add route to `internal/server/router.go`.
- **Adding a new OAuth provider:** Implement `internal/integration/oauth/provider.go` `Provider` interface → register in `internal/integration/oauth/registry.go` if env vars set.
- **Adding a new broker provider (Phase 2+):** Implement `internal/integration/broker/provider.go` `Provider` interface (Name / Connect / Disconnect / FetchDeals / Ping) → register in `router.go` registry map. Return canonical `broker.Deal` shape (minor units, UUIDv5 stable `ProviderDealID`) so SyncService's dedup contract holds without changes.
- **Adding a new billing provider (Phase 4+):** Implement `billing.Service` interface in `internal/integration/billing/`. Wire the new env check (`XEnabled()`) in `router.go`. Production code uses Stripe (`stripe-go`); to add Omise, drop a `omise.go` with the same interface.
- **Adding a new mailer backend (Phase 4+):** Implement `mailer.Service` interface in `internal/integration/mailer/`. SendGrid, Mailgun, SES — each as a separate file.
- **Adding a new API scope (Phase 5+):** add the constant in `internal/domain/api_key/api_key.go`, append to `AllScopes`, add to `shared/src` if FE needs it, then mount a new group in `internal/httpapi/phase5/public_handler.go` with `apikey.Middleware(svc, bucket, scope)`.
- **Adding a new public endpoint (Phase 5+):** pick the right group in `internal/httpapi/phase5/public_handler.go` (read:accounts / read:trades / read:analytics / read:payments), bind the handler function in `PublicDeps`, and wire it in `internal/server/router.go`. Keep the handler thin — delegate to the existing phase1/4 implementation.
- **Adding a new rate limit backend (Phase 5.1+):** implement `ratelimit.KeyBucket` interface (`Allow(uuid) Decision`, `Close() error`) and add the wiring switch in `internal/server/router.go`. The middleware only inspects `Decision.Allowed`. Don't bypass the interface for one-off buckets — that breaks the prod/dev swap.
- **Adding a new webhook event type (Phase 5.2+):** add a constant in `internal/domain/webhook/webhook.go` (EventType + append to AllEventTypes), then call `webhookSvc.Emit(ctx, EmitInput{EventType: <new>, Data: ...})` from wherever the event happens. The worker picks the row up on the next tick. Update the FE `useWebhooks.ts` `ALL_WEBHOOK_EVENTS` and the `/webhooks` page checkbox list.
- **Adding a new LLM provider (Phase 6+):** implement `ai.Provider` interface (`Name() string`, `Generate(ctx, GenerateInput) (*GenerateResult, error)`) and add the wiring case in `internal/server/router.go`'s `buildAIProvider`. The interface uses raw `net/http` — no SDK dependency required.
- **Adding a new AI analysis type (Phase 6+):** add a new method to `Service` and a new worker job struct mirroring `AnalysisJobPayload`. Today only per-trade commentary is supported; weekly recap / Q&A chat would be new job kinds.
- **Adding a new Stripe webhook event (Phase 7+):** add a new `apply<EventType>` method on `StripeService` + a case in the `HandleWebhookEvent` switch. Use `EventApplyStore` methods that already exist (Phase 7 ships `WebhookEventInsert` + subscription/payment upserts). Unknown event types return `ErrUnknownEvent` — the handler logs + returns 200 so Stripe stops retrying.
- **Adding a new MetaApi endpoint (Phase 8+):** add a typed helper on `MetaApiAdapter` (mirror `Connect` / `FetchDeals` / `Ping`) using the existing `doJSON(ctx, method, path, body, out)` plumbing, and expose it as a new method on the `Provider` interface. The auth header (`auth-token: <METAAPI_TOKEN>`) and JSON envelope (`{ "error": ..., "data": ..., "metadata": ... }`) are already wired. Keep `SyncService` and `broker_connections` schema untouched — the canonical `Deal` shape is the dedup contract.
- **Adding a new embedder (Phase 9+):** implement `ai.Embedder` interface (`Name()`, `Dimensions()`, `Embed(ctx, []string) ([][]float32, error)`) and add a switch in `internal/server/router.go` near the existing Stub/OpenAI wiring. Vector output must be L2-normalized so pgvector's `<=>` cosine distance is meaningful. Stub fallback (`r.embedder.Name() == "stub"`) is auto-handled by `ChatRAGRetriever` via the `RecentTradeLister` path.
- **Adding a new chat mode (Phase 9+):** `ChatService` already handles per-thread RAG. For a new mode (e.g. "weekly Q&A with summary context"), add a new method on `ChatService` that builds a different prompt (mirror `BuildReply`). The shared budget + thread model + worker pipeline stay the same. Add a new endpoint in `internal/httpapi/phase9/handler.go` + register in `RegisterRoutes`.
- **Adding a new write scope (Phase 10+):** add a constant in `internal/domain/api_key/api_key.go` (append to `AllScopes` + the right `ReadScopes`/`WriteScopes` group), add to `shared/src/index.ts` `API_SCOPES` (with `group: 'read' | 'write'`), then add handlers in `internal/httpapi/phase5/public_write_handlers.go` and mount via `RegisterWriteRoutes` with the scope guard. POST endpoints MUST go through `apikey.IdempotencyMiddleware`; PUT/PATCH/DELETE bypass.
- **Extending the idempotency middleware (Phase 10+):** the middleware accepts an `IdempotencyStore` interface (Lookup + Store). To swap implementations (e.g. add Redis), implement the interface + pass it to `apikey.IdempotencyMiddleware`. The TTL is `IdempotencyTTL` (24h); override via a wrapper if needed. Sweeper is wired separately via `NewIdempotencySweeper`.
- **Adding a new analytics dimension (Phase 3+):** add a method to `TradeRepo` returning `[]BucketRow`, plus a handler in `phase1http/imports_handler.go` and a FE composable fn + component in `app/components/`. Reuse the `BucketRow` SQL helper.
- **Adding a new PDF report section (Phase 3+):** add fields to `pdfreport.ReportRequest`, fill them in `phase3http.Handler.buildRequest`, render them in `signintech.go` using the existing `Cell/Line` primitives.
- **Adding a new tenant-scoped table:** migration with `tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE`, query in `queries/<table>.sql`, repo wrapper, service use case. **Every query must include `WHERE tenant_id = $ctxTenantID`.**
- **Adding a new plan:** insert row in `plans` migration + map Stripe price id via `STRIPE_PRICE_<PLAN>` env (Phase 4+).
- **Adding a new role:** extend `membership_role` enum in new migration, update RBAC matrix in spec §7.4, update `RequireRole()` middleware.

## Don't

- ❌ Don't import `internal/data/...` from `internal/service/...` (use DTO instead) — see project memory if available
- ❌ Don't use float for money — always `numeric` + minor units
- ❌ Don't hard-code plan checks like `if user.plan == "pro"` — use `internal/entitlement/` (Phase 1+) or `plan.Metadata` jsonb lookup
- ❌ Don't use `HS256` JWT — EdDSA only (per Phase 0 design)
- ❌ Don't log secrets (passwords, tokens, refresh tokens, PII beyond user_id/email)
- ❌ Don't write to `audit_logs` outside service code (append-only contract)
- ❌ Don't use Thai unicode in `apps/web/app/{pages,components,layouts}/*.vue` files (KX-I18N-004 fence if multi-tenant i18n added; Phase 0 English-only anyway)
- ❌ Don't expand scope into deferred phases without surfacing first

## Verification per change

Per MASTER PROMPT §43, every phase report includes:

```
Changed Files
Database Changes (migrations + sqlc)
API Changes (new endpoints / contracts)
Frontend Changes (new pages / components)
Environment Variables (added)
Migration
Changed (re-apply commands)
Tests (what was added, what passed)
Known Issues
How to Run (quickstart to verify)
```

Run before reporting done:

```bash
make lint       # golangci-lint + pnpm lint
make test       # go test -race + pnpm test
make build      # go build + pnpm build
```

Show evidence (test output, build output) — don't just say "passed".

## Communication language

- User (Keenix engineer) prefers **Thai** in chat, mixed with English tech terms.
- Code, identifiers, log keys, error codes: **English only**.
- Commit messages: **English** (Conventional Commits style).
