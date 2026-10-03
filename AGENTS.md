# AGENTS.md — Trading Journal Project Context

> Read this **before** doing any non-trivial work. It documents invariants for code, current phase scope, and where things live. Phase-specific agent docs may live alongside this one (e.g. `apps/api/AGENTS.md`, `apps/web/AGENTS.md`).

---

## Project shape

- **Monorepo:** `apps/api` (Go), `apps/web` (Nuxt 3), `packages/shared` (TS), `infra` (docker-compose + env), `docs/` (specs)
- **Language policy:** English for code, identifiers, log keys, error codes, commit messages. Free Thai for chat with the user (this repo's owner prefers Thai).
- **Brand:** "Trading Journal" (working name). Future split possible.

## Current phase

**Phase 5.1 complete. All planned phases (0–5) shipped + Phase 5.1 production hardening shipped.**

- Phase 0 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase0-design.md` — Foundation ✅ shipped
- Phase 1 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase1-design.md` — Free + ฿99 Upload Journal ✅ shipped
- Phase 2 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase2-design.md` — Mock broker sync ✅ shipped
- Phase 3 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase3-design.md` — Multi-account + Session/EA/Magic + PDF ✅ shipped
- Phase 4 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase4-design.md` — Billing (Stripe) + Real MetaApi + Email scheduling ✅ shipped
- Phase 5 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase5-design.md` — Public API + Scoped API Keys ✅ shipped
- Phase 5.1 spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase5_1-design.md` — Redis rate limit + Integration test ✅ shipped

**Phase 5.1 in scope (delivered):** Extracted `ratelimit.KeyBucket` interface · renamed Phase 5 in-process impl to `MemoryKeyBucket` · new `RedisKeyBucket` (atomic Lua token-bucket, fail-open on Redis error, TTL-based GC) · `RATE_LIMIT_BACKEND=memory|redis` env-gated router swap · E2E integration test (testcontainers-go Postgres+Redis) covering 10-step API-key lifecycle: register → /me → mint → public-with-key → scope guard → burst → 429 → revoke → reuse-401 (revoked) → list-confirms-revoked. Skipped unless `INTEGRATION_TESTS=on` + Docker available; runs in CI.

**Deferred (Phase 6+ — outside initial master plan):**
- AI analysis (LLM trade commentary)
- Real MetaApi SDK handshake (provider returns ErrProviderNotConfigured until METAAPI_TOKEN is set)
- Stripe webhook → subscription state-machine application (verification-only in Phase 5)
- `write:journal` / `write:webhooks` scopes (Phase 5 is read-only)
- Webhook delivery / inbound webhooks (Phase 5.2)
- Per-key (per-user) rate-limit override

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
- **Broker sync (Phase 2):** `Provider` interface in `internal/integration/broker/`. Mock implementation only — MetaApi SDK deferred. SyncWorker reuses the Phase 1 worker.Pool; SyncService reuses `dealRepo.Insert` for dedup (single source of truth for both manual uploads and broker syncs).
- **Plan limits (Phase 3):** `internal/entitlement/limits_by_plan.go` maps `plan.Code` to `Limits{MaxTradingAccounts, MaxBrokerConnections}`. Handlers (`CreateAccount`, `CreateConnection`) call `entitlement.LookupForTenant(ctx, store, tenantID)` and 402 on overflow.
- **PDF reports (Phase 3):** `pdfreport.Service` interface in `internal/integration/pdfreport/`. Implementation uses `go-pdf/fpdf` (pure Go, MIT, built-in PDF base 14 fonts — no TTF payload bundled).
- **Billing (Phase 4):** `billing.Service` interface in `internal/integration/billing/`. Stub impl for local dev; Stripe impl via `stripe-go/v9` (real webhook signature verify, Phase 5 future work to fully wire checkout session-state application).
- **Mailer (Phase 4):** `mailer.Service` interface in `internal/integration/mailer/`. Stub writes `.eml` files to a temp dir; production uses `SMTPMailer` via net/smtp.
- **Scheduling (Phase 4):** pure cron helper `ComputeNextSend` + `ScheduleWorker` (60s poll, in-process single-flight). New `reportbuild` package shares trade-walking logic between HTTP `/v1/reports/pdf` and the worker.
- **API keys (Phase 5):** `apikey.Service` interface in `internal/integration/apikey/`. Implementation uses `golang.org/x/crypto/bcrypt` (cost=10). Key format `trj_pk_<8-char-prefix>.<24-byte-secret>` (URL-safe base64); the `prefix` is the public lookup key, the `secret` is bcrypt-hashed and never recoverable. Echo middleware sets tenant_id on both request ctx (`tenant.WithTenantID`) and echo ctx (`c.Set("tenant_id")`) — the former for service/store paths, the latter for existing phase1/4 handlers. Per-key rate limit via `internal/ratelimit/KeyBucket` (in-process `golang.org/x/time/rate.Limiter` map; Phase 5.1 swaps for Redis). Public API endpoints under `/v1/public/*` are thin wrappers around existing phase1/4 handlers; admin endpoints under `/v1/api-keys` are JWT-authed for management (list/create/get/update/revoke).
- **Rate limit (Phase 5.1):** `ratelimit.KeyBucket` interface with two implementations. `MemoryKeyBucket` (in-process `sync.Mutex` + `rate.Limiter` per key) — default for dev. `RedisKeyBucket` (atomic Lua token-bucket in Redis; fail-open on Redis error with `slog.Warn`; TTL-based cleanup) — set via `RATE_LIMIT_BACKEND=redis` for prod multi-instance. Both return `Decision{Allowed, Remaining, Reset}`. E2E integration test in `internal/integration/apikey_e2e/` (testcontainers-go Postgres+Redis) exercises the full 10-step API-key flow; gated by `INTEGRATION_TESTS=on`.
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
| Spec source (MASTER PROMPT) | `pasted-text.txt` in workspace root or repo root |
| Migrations | `apps/api/migrations/` (0001_init, 0002_oauth, 0003_phase1, 0004_phase2, 0005_phase4, 0006_phase5) |
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
| Phase 1 handlers | `apps/api/internal/httpapi/phase1/` |
| Phase 2 handlers | `apps/api/internal/httpapi/phase2/` |
| Phase 3 handlers | `apps/api/internal/httpapi/phase3/` |
| Phase 4 handlers | `apps/api/internal/httpapi/phase4/` |
| Phase 5 handlers | `apps/api/internal/httpapi/phase5/` |
| Phase 1 store repos | `apps/api/internal/store/phase1_repos.go` |
| Phase 2 store repos | `apps/api/internal/store/phase2_repos.go` |
| Phase 3 store repos | `apps/api/internal/store/phase3_repos.go` |
| Phase 4 store repos | `apps/api/internal/store/phase4_repos.go` |
| Phase 5 store repos | `apps/api/internal/store/phase5_repos.go` |
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