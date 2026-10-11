# R2 Trading Intelligence Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add deterministic Trading Behavior Intelligence and immutable Strategy Versioning while leaving the existing Public API and outbound Webhook contracts unchanged.

**Architecture:** Read normalized trades through tenant-scoped repositories, calculate a deterministic account-scoped behavior profile, persist findings, and expose it through JWT-authenticated internal product routes. Add Strategy and StrategyVersion domain/store/service layers with schema validation and trade linkage; keep all strategy rules descriptive and non-executable.

**Tech Stack:** Go 1.26+, Echo, pgx, PostgreSQL migrations, existing tenant/RBAC/audit patterns, Nuxt 3/Vue 3/TypeScript/Vuetify, Vitest.

**Spec:** `docs/superpowers/specs/2026-10-11-r2-trading-intelligence-design.md`

## Global Constraints

- Public API key scopes, idempotency behavior, public write endpoints, and outbound Webhook event/delivery behavior are unchanged.
- No AI strategy candidate generation, market-data ingestion, Backtest, OOS, Walk Forward, Paper Trading, Signal Engine, Risk Engine, Execution Engine, or MetaApi order method is added.
- Every database query includes `tenant_id`; account-scoped behavior queries also include `trading_account_id`.
- Use the existing `{error: {code, message, details}}` envelope and existing JWT/RBAC middleware.
- Use UTC timestamps and UUIDv7 IDs; do not log secrets, tokens, raw credentials, or full rule payloads where audit redaction is required.
- Strategy Version rows are append-only; updates create a new version.
- Existing money fields remain unchanged in this slice; do not add a new money representation that worsens the current invariant.
- Code, identifiers, log keys, error codes, and commit messages are English.
- Do not modify `internal/httpapi/phase5/`, `internal/integration/apikey/`, `internal/integration/webhook/`, `internal/worker/webhook_worker.go`, or the existing Webhook page/composable except if a compile-only import change is unavoidable.

## Review Focus

- A profile request for an account belonging to another tenant must return not-found/unauthorized behavior without revealing that the account exists; test in the repository and HTTP tasks.
- A profile with zero or fewer than 10 completed trades must return `insufficient_data` without fabricated percentages; test in the calculator task.
- Trades exactly on detector thresholds must be classified deterministically, including UTC boundary timestamps; test in the detector task.
- Concurrent strategy-version creation must not produce duplicate version numbers; test the repository/service transaction path.
- Unknown rule fields/operators and non-positive risk values must be rejected before persistence; test the rule validator and HTTP task.

---

### Task 1: R2 migration and domain contracts

**Files:**
- Create: `apps/api/migrations/0012_r2_trading_intelligence.up.sql`
- Create: `apps/api/migrations/0012_r2_trading_intelligence.down.sql`
- Create: `apps/api/internal/domain/behavior/behavior.go`
- Create: `apps/api/internal/domain/behavior/behavior_test.go`
- Create: `apps/api/internal/domain/strategy/strategy.go`
- Create: `apps/api/internal/domain/strategy/rules.go`
- Create: `apps/api/internal/domain/strategy/rules_test.go`

**Interfaces:**
- Consumes: Existing `trades`, `trading_accounts`, `tenants`, `users`, and `audit_logs` schemas.
- Produces: `behavior.Profile`, `behavior.Finding`, `behavior.InputTrade`, `strategy.Strategy`, `strategy.Version`, `strategy.RuleSet`, and `strategy.ValidateRuleSet(json.RawMessage) (strategy.RuleSet, error)`.

- [ ] **Step 1: Write failing domain tests**

  Add tests for `ValidateRuleSet`: schema version 1 and all required sections pass; an unknown field, unsupported operator, missing entry/exit condition, or non-positive risk value fails with a typed validation error. Add behavior tests for `ProfileStatus` values, finding codes, and zero-value profile serialization.

- [ ] **Step 2: Run domain tests and verify failure**

  Run: `go test ./internal/domain/behavior ./internal/domain/strategy`

  Expected: FAIL because the new domain packages and validator do not exist.

- [ ] **Step 3: Implement domain contracts**

  Define `behavior.InputTrade` with IDs, symbol, direction, volume, net P/L, outcome, open/close UTC timestamps, and magic. Define `behavior.Profile` with status, sample count, window, algorithm version, summary metrics, and findings. Define `strategy.Strategy`, `strategy.Version`, `strategy.RuleSet`, `strategy.Condition`, and the exact supported operators `eq`, `neq`, `gt`, `gte`, `lt`, `lte`, `in`. Make validation reject unknown JSON fields and enforce positive `stop_distance`, `target_distance`, and `max_volume`.

  Add migration 0012 with `strategies`, `strategy_versions`, `trade_behavior_profiles`, and `trade_behavior_findings`; add nullable `strategy_version_id` to `trades`. Enforce tenant FKs, unique strategy name per tenant, unique `(strategy_id, version_no)`, profile uniqueness on `(tenant_id, trading_account_id, window_start, window_end, algorithm_version)`, and indexes for account/window and strategy lookups.

- [ ] **Step 4: Run domain tests and migration syntax checks**

  Run: `go test ./internal/domain/behavior ./internal/domain/strategy`

  Expected: PASS. Run: `rg -n "CREATE TABLE|ALTER TABLE|CREATE UNIQUE INDEX|strategy_version_id" apps/api/migrations/0012_r2_trading_intelligence.up.sql` and confirm every new table has `tenant_id` and the down migration reverses the up migration in dependency order.

- [ ] **Step 5: Commit**

  ```bash
  git add apps/api/migrations/0012_r2_trading_intelligence.* apps/api/internal/domain/behavior apps/api/internal/domain/strategy
  git commit -m "feat: add R2 trading intelligence domain"
  ```

### Task 2: Deterministic behavior calculator and detectors

**Files:**
- Create: `apps/api/internal/service/behavior/calculator.go`
- Create: `apps/api/internal/service/behavior/calculator_test.go`
- Create: `apps/api/internal/service/behavior/detectors.go`
- Create: `apps/api/internal/service/behavior/detectors_test.go`

**Interfaces:**
- Consumes: `behavior.InputTrade` and `behavior.Profile` from Task 1.
- Produces: `behavior.NewCalculator() *Calculator`, `(*Calculator).Calculate(accountID uuid.UUID, from, to time.Time, trades []behavior.InputTrade) behavior.Profile`, and deterministic detector implementations for the five codes in the spec.

- [ ] **Step 1: Write failing calculator tests**

  Cover empty input, 9 trades, exactly 10 trades, win rate, expectancy, average holding time, UTC window filtering, and no division-by-zero for zero wins/losses. Assert that insufficient samples return `status = insufficient_data` and no fabricated score.

- [ ] **Step 2: Run calculator tests and verify failure**

  Run: `go test ./internal/service/behavior -run 'TestCalculator' -v`

  Expected: FAIL because `Calculator` does not exist.

- [ ] **Step 3: Implement the calculator**

  Calculate only from completed trades within `[from, to)`, sort a copy by `OpenTime`, compute count/wins/losses/net result/expectancy/holding time/frequency/concentration, and set algorithm version `r2-v1`. Use UTC comparisons and return an empty findings slice rather than nil.

- [ ] **Step 4: Write failing detector regression tests**

  Add stable fixtures for exact boundaries: 20 trades in 24 hours, a second trade exactly 30 minutes after a loss, volume exactly 150%, three consecutive losses, 70% symbol concentration, and a UTC midnight boundary. Assert finding code, severity, count, and evidence trade IDs/timestamps.

- [ ] **Step 5: Run detector tests and verify failure**

  Run: `go test ./internal/service/behavior -run 'TestDetector' -v`

  Expected: FAIL because detector implementations do not exist.

- [ ] **Step 6: Implement detectors and run the focused suite**

  Implement detectors behind `type Detector interface { Code() string; Detect([]behavior.InputTrade) behavior.Finding }`. Use the exact thresholds from the spec, never mutate input, and make evidence deterministic by sorting IDs/timestamps. Run: `go test ./internal/service/behavior -v`.

  Expected: PASS.

- [ ] **Step 7: Commit**

  ```bash
  git add apps/api/internal/service/behavior
  git commit -m "feat: add deterministic trading behavior detectors"
  ```

### Task 3: Tenant-scoped repositories and Store wiring

**Files:**
- Create: `apps/api/internal/store/phase11_repos.go`
- Create: `apps/api/internal/store/phase11_repos_test.go`
- Modify: `apps/api/internal/store/store.go`
- Modify: `apps/api/internal/store/phase1_repos.go`

**Interfaces:**
- Consumes: `store.DBTX`, Task 1 domain types, and existing `TradeRepo` query conventions.
- Produces: `BehaviorProfileRepo`, `BehaviorFindingRepo`, `StrategyRepo`, and `StrategyVersionRepo` constructors and methods on `store.Store`.

- [ ] **Step 1: Write failing repository tests**

  Add pgx/DBTX fake tests for account-scoped trade loading, profile upsert/read, finding replacement, strategy create/list, version insert, and trade-version link. Assert every SQL statement contains tenant predicates and account predicates where required.

- [ ] **Step 2: Run repository tests and verify failure**

  Run: `go test ./internal/store -run 'Test(Behavior|Strategy|TradeStrategy)' -v`

  Expected: FAIL because the repositories and Store fields do not exist.

- [ ] **Step 3: Implement repository methods**

  Add `ListCompletedByAccountWindow(ctx, tenantID, accountID uuid.UUID, from, to time.Time) ([]behavior.InputTrade, error)`, profile `Upsert/GetLatest`, finding `ReplaceForProfile`, strategy `Create/ListByTenant/GetByIDByTenant`, version `Create/ListByStrategy/GetByIDByTenant`, and `TradeRepo.LinkStrategyVersion(ctx, tenantID, tradeID, versionID uuid.UUID) error`. Map `pgx.ErrNoRows` to the existing `store.ErrNotFound`.

  Insert a version inside a transaction that locks the strategy row before selecting the next `version_no`; reject a missing or cross-tenant strategy/version as `ErrNotFound`.

- [ ] **Step 4: Run repository tests and static checks**

  Run: `go test ./internal/store -run 'Test(Behavior|Strategy|TradeStrategy)' -v` and `go vet ./internal/store`.

  Expected: PASS.

- [ ] **Step 5: Commit**

  ```bash
  git add apps/api/internal/store/phase11_repos.go apps/api/internal/store/phase11_repos_test.go apps/api/internal/store/store.go apps/api/internal/store/phase1_repos.go
  git commit -m "feat: add R2 tenant-scoped repositories"
  ```

### Task 4: Behavior and Strategy application services

**Files:**
- Create: `apps/api/internal/service/behavior/service.go`
- Create: `apps/api/internal/service/behavior/service_test.go`
- Create: `apps/api/internal/service/strategy/service.go`
- Create: `apps/api/internal/service/strategy/service_test.go`
- Modify: `apps/api/internal/store/audit_log_repo.go` only if a redaction helper is required by the existing audit contract

**Interfaces:**
- Consumes: Task 2 calculator/detectors, Task 3 repositories, `tenant.TenantIDFromContext`, `auth.UserIDFromContext`, and `store.AuditLogRepo`.
- Produces: `behavior.Service.Calculate(ctx context.Context, tenantID, accountID uuid.UUID, from, to time.Time) (*behavior.Profile, error)`; `strategy.Service.Create`, `List`, `CreateVersion`, `ListVersions`, and `LinkTrade` methods with explicit tenant/user IDs.

- [ ] **Step 1: Write failing service tests**

  Test behavior calculation orchestration, insufficient data persistence, findings replacement, cross-tenant account rejection, strategy creation, version numbering, invalid rules, and trade linkage audit payload redaction.

- [ ] **Step 2: Run service tests and verify failure**

  Run: `go test ./internal/service/behavior ./internal/service/strategy -v`

  Expected: FAIL because the application services do not exist.

- [ ] **Step 3: Implement behavior service**

  Load completed trades through the repository, calculate with Task 2, upsert the profile, replace findings in one transaction, and return the persisted profile. Use a typed domain error for invalid windows (`from >= to`) and map missing accounts to not-found behavior.

- [ ] **Step 4: Implement strategy service**

  Validate names/status/rules, create strategy metadata, validate rules before version insertion, create versions transactionally, and link a trade only after confirming both trade and version belong to the same tenant. Write audit actions `behavior_profile_calculated`, `strategy_created`, `strategy_version_created`, and `trade_strategy_version_linked`; never include full rule payloads in audit details.

- [ ] **Step 5: Run focused and race-independent service tests**

  Run: `go test ./internal/service/behavior ./internal/service/strategy -v`.

  Expected: PASS.

- [ ] **Step 6: Commit**

  ```bash
  git add apps/api/internal/service/behavior apps/api/internal/service/strategy apps/api/internal/store/audit_log_repo.go
  git commit -m "feat: add R2 behavior and strategy services"
  ```

### Task 5: Internal authenticated R2 HTTP routes

**Files:**
- Create: `apps/api/internal/httpapi/r2/dto.go`
- Create: `apps/api/internal/httpapi/r2/handler.go`
- Create: `apps/api/internal/httpapi/r2/handler_test.go`
- Modify: `apps/api/internal/server/router.go`

**Interfaces:**
- Consumes: Task 4 service methods and existing `auth`, `tenant`, `observability`, and Echo route patterns.
- Produces: JWT-authenticated routes `POST/GET /v1/accounts/:account_id/behavior-profile`, `GET/POST /v1/strategies`, `GET/POST /v1/strategies/:id/versions`, and `PUT /v1/trades/:id/strategy-version`.

- [ ] **Step 1: Write failing HTTP contract tests**

  Add tests for valid profile calculation, missing/invalid dates, account ownership, strategy rule validation, RBAC denial, version creation, and trade linkage. Assert status codes and the standard error envelope.

- [ ] **Step 2: Run HTTP tests and verify failure**

  Run: `go test ./internal/httpapi/r2 -v`

  Expected: FAIL because the package and routes do not exist.

- [ ] **Step 3: Implement DTOs and thin handlers**

  Parse UUIDs and RFC3339 UTC dates, bind JSON bodies, call services, and map typed errors to 400/401/403/404/409/422/500 using existing observability helpers. Do not add API-key middleware or touch `phase5` public routes.

- [ ] **Step 4: Wire routes in `server.RegisterRoutes`**

  Construct the R2 services after `store.New(pool)`, mount the handler under the existing `authed` group, and keep Webhook initialization and route registration unchanged.

- [ ] **Step 5: Run HTTP and route verification**

  Run: `go test ./internal/httpapi/r2 ./internal/server -v` and `go vet ./internal/httpapi/r2 ./internal/server`.

  Expected: PASS.

- [ ] **Step 6: Commit**

  ```bash
  git add apps/api/internal/httpapi/r2 apps/api/internal/server/router.go
  git commit -m "feat: expose R2 trading intelligence routes"
  ```

### Task 6: Frontend behavior and strategy surfaces

**Files:**
- Create: `apps/web/app/composables/useBehavior.ts`
- Create: `apps/web/app/composables/useStrategies.ts`
- Create: `apps/web/app/pages/behavior.vue`
- Create: `apps/web/app/pages/strategies.vue`
- Create: `apps/web/tests/behavior.test.ts`
- Create: `apps/web/tests/strategies.test.ts`
- Modify: `apps/web/app/layouts/default.vue` only to add internal R2 navigation entries

**Interfaces:**
- Consumes: Task 5 JSON contracts and existing `useApi`/auth/store patterns.
- Produces: typed composables for profile/strategy/version calls and dark-theme pages with loading, empty, error, findings, strategy creation, and immutable-version states.

- [ ] **Step 1: Write failing composable/page tests**

  Test that behavior renders `insufficient_data`, findings, loading, and API error states; strategy UI renders a strategy list, creates a version, rejects invalid local input, and displays versions as read-only.

- [ ] **Step 2: Run frontend tests and verify failure**

  Run from `apps/web`: `pnpm vitest run tests/behavior.test.ts tests/strategies.test.ts`

  Expected: FAIL because the composables/pages do not exist.

- [ ] **Step 3: Implement typed composables**

  Add request/response interfaces matching the R2 DTOs and use the existing `$fetch` wrapper so Bearer injection and typed API errors remain centralized.

- [ ] **Step 4: Implement pages and navigation**

  Add account selector/date window/profile cards/findings table and strategy/version list/create forms. Do not change existing API-key or Webhook navigation/page behavior.

- [ ] **Step 5: Run frontend checks**

  Run from `apps/web`: `pnpm vitest run tests/behavior.test.ts tests/strategies.test.ts`, `pnpm lint`, `pnpm typecheck`, and `pnpm build`.

  Expected: all commands pass; existing chunk-size/deprecation warnings may remain warnings.

- [ ] **Step 6: Commit**

  ```bash
  git add apps/web/app/composables/useBehavior.ts apps/web/app/composables/useStrategies.ts apps/web/app/pages/behavior.vue apps/web/app/pages/strategies.vue apps/web/tests/behavior.test.ts apps/web/tests/strategies.test.ts apps/web/app/layouts/default.vue
  git commit -m "feat: add R2 behavior and strategy screens"
  ```

### Task 7: Documentation and release verification

**Files:**
- Modify: `docs/roadmaps/2026-10-09-trading-journal-implementation-audit.md`
- Modify: `docs/roadmaps/2026-10-09-trading-journal-ai-trading-roadmap-th.md`
- Modify: `README.md` only if the new R2 status or quickstart links are stale

**Interfaces:**
- Consumes: completed Tasks 1–6 and their test evidence.
- Produces: evidence-backed R2 status, remaining R3/R4/R5 gates, and a verification record that keeps Public API/Webhook blockers visible.

- [ ] **Step 1: Write documentation assertions**

  Add a small repository check or documented `rg` command that confirms the audit distinguishes R2 behavior/strategy as implemented from R3 validation and R4/R5 execution as not started.

- [ ] **Step 2: Update roadmap/audit evidence**

  Record migration, domain, service, route, frontend, and test paths. Keep API/Webhook verification limitations and Docker/race/tooling blockers explicitly marked as unresolved.

- [ ] **Step 3: Run final verification**

  Run: `go test -short ./...`, `go test -tags=integration -run '^$' ./...`, `go build ./...`, `go vet ./...`, `pnpm lint`, `pnpm typecheck`, and `pnpm test:unit` from their required directories. Run `git diff --check`. Attempt `go test -race ./...`, Docker-backed E2E, and `make lint/test`; record environment failures verbatim rather than treating them as product regressions.

- [ ] **Step 4: Commit documentation**

  ```bash
  git add docs/roadmaps README.md
  git commit -m "docs: record R2 trading intelligence delivery"
  ```

## Final completion contract

- All focused tests from Tasks 1–6 pass.
- Existing non-Docker Go and frontend checks pass.
- Public API and outbound Webhook files/contracts are unchanged.
- No live order or execution path exists in the diff.
- The final report lists changed files, migration, internal API, frontend, tests, known environment blockers, and quickstart commands.
