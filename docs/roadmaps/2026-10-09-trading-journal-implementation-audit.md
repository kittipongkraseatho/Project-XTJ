# Trading Journal Implementation Audit

วันที่ตรวจ: 9 ตุลาคม 2026 (Asia/Bangkok)

แหล่งอ้างอิงหลัก:

- Target roadmap: [Trading Journal → Autonomous AI Trading](./2026-10-09-trading-journal-ai-trading-roadmap-th.md)
- Source attachment: `C:\Users\kittiphong.k\Downloads\Trading_Journal_AI_Trading_Roadmap_TH.docx`
- Repository instructions: [`AGENTS.md`](../../AGENTS.md)

## 1. Scope boundary

เอกสารแนบเป็น product/architecture target และ safety requirements สำหรับระบบปลายทาง ไม่ใช่หลักฐานว่า phase อนาคตทำเสร็จแล้ว

คำขอของผู้ใช้ในรอบนี้คือ:

1. rebaseline roadmap ให้ตรงกับเอกสารแนบ
2. ไล่ตรวจสิ่งที่ทำมาแล้วทั้งหมด
3. ทำ plan ก่อน implementation
4. รอบนี้ไม่ implement feature ใหม่ของ Strategy, Backtest, Paper, Signal, Risk, Execution หรือ Live Trading

## 2. Status model

| Status | ความหมาย |
|---|---|
| Implemented | มี implementation และมี verification path ใน repository |
| Partial | มี implementation บางส่วน แต่ยังไม่ครบ Definition of Done หรือ release gate |
| Not started | ยังไม่พบ production implementation ของ boundary นั้น |
| Not verified | มี code หรือ path แต่ environment ปัจจุบันยังรัน verification ไม่ได้ |
| Deferred | ถูกระบุไว้นอก scope โดย project context เดิม |

`Implemented` ในตารางนี้หมายถึงมีหลักฐานใน source tree ไม่ได้หมายถึง production sign-off แล้ว หากคำสั่งทดสอบรันไม่ได้จะระบุไว้ใน Verification column

## 3. Legacy delivery audit: Phase 0–10

| Legacy phase | สิ่งที่พบ | หลักฐานหลัก | สถานะ |
|---|---|---|---|
| Phase 0 Foundation | Auth, tenant, RBAC, JWT/refresh session, OAuth, migrations, config, health, observability, worker, CI | `apps/api/internal/auth/`, `internal/tenant/`, `internal/rbac/`, `internal/server/`, `migrations/0001_init.up.sql`, `0002_oauth.up.sql`, `.github/workflows/` | Implemented; local verification not available |
| Phase 1 Upload Journal | MT5 HTML/CSV parser, import jobs, normalized order/deal/position/trade, journal, tags, images, daily metrics, dashboard/trades/import/journal pages | `migrations/0003_phase1.up.sql`, `internal/parser/`, `internal/service/import_service.go`, `httpapi/phase1/`, `apps/web/app/pages/`, `internal/integration/cloudinary/` | Implemented; correctness gate needs executed tests |
| Phase 2 Broker sync | Provider abstraction, mock provider, broker connections, sync runs, SyncService/SyncWorker, manual sync routes | `migrations/0004_phase2.up.sql`, `internal/integration/broker/`, `internal/syncsvc/`, `internal/worker/sync_worker.go`, `httpapi/phase2/` | Implemented |
| Phase 3 Multi-account/Reports | Multi-account limits, session/EA/Magic analytics, PDF reports, report UI | `internal/entitlement/`, `httpapi/phase3/`, `internal/integration/pdfreport/`, `apps/web/app/components/` | Implemented |
| Phase 4 Billing/Email | Stripe checkout/portal, subscription/payment tables, Stripe mailer, schedules, PDF email delivery | `migrations/0005_phase4.up.sql`, `internal/integration/billing/`, `internal/integration/mailer/`, `service/report_schedule/`, `httpapi/phase4/` | Implemented; external-provider paths need environment verification |
| Phase 5 Public API | Scoped API keys, public read API, admin key management, per-key rate limit | `migrations/0006_phase5.up.sql`, `internal/integration/apikey/`, `httpapi/phase5/public_handler.go`, `apps/web/app/pages/api-keys.vue` | Implemented |
| Phase 5.1 Rate limit hardening | KeyBucket interface, MemoryKeyBucket, RedisKeyBucket, env-gated backend, lifecycle E2E | `internal/ratelimit/`, `internal/integration/apikey_e2e/`, `infra/.env.example` | Implemented; integration path is Docker-gated |
| Phase 5.2 Outbound webhooks | Subscriptions/deliveries, HMAC timestamp signing, retry worker, two event types, admin UI | `migrations/0007_phase5_2.up.sql`, `internal/integration/webhook/`, `internal/worker/webhook_worker.go`, `httpapi/phase6/webhooks_handler.go`, `apps/web/app/pages/webhooks.vue` | Implemented |
| Phase 6 AI analysis | Provider abstraction (Stub/OpenAI/Anthropic), prompt-hash cache, shared daily budget, async analysis, trade panel | `migrations/0008_phase6.up.sql`, `internal/integration/ai/service.go`, `internal/worker/analysis_job.go`, `httpapi/phase6/ai_handler.go`, `apps/web/app/composables/useAnalysis.ts` | Implemented; external provider execution not locally verified |
| Phase 7 Stripe state machine | Idempotent webhook event storage and five event handlers | `migrations/0009_phase7.up.sql`, `internal/integration/billing/stripe.go`, `stripe_events_test.go`, `httpapi/phase4/billing_handler.go` | Implemented |
| Phase 8 Real MetaApi REST | Connect list/find-or-create/poll, FetchDeals history mapping, Ping, no SDK | `internal/integration/broker/metaapi.go`, `metaapi_test.go`, config and router wiring | Implemented; no order execution by design |
| Phase 9 AI expansion | pgvector embeddings, weekly recap, RAG chat, thread/message storage, shared budget, async jobs, recap/chat pages | `migrations/0010_phase9.up.sql`, `internal/integration/ai/{embed_service,recap_service,chat_service,retriever}.go`, `httpapi/phase9/`, `apps/web/app/pages/{recaps,chat}.vue` | Implemented; pgvector E2E is not run in current host |
| Phase 10 Public API writes | Four write scopes, idempotency table/middleware/sweeper, eight write endpoints, redacted audit logging, UI scope groups | `migrations/0011_phase10.up.sql`, `internal/integration/apikey/idempotency_middleware.go`, `httpapi/phase5/public_write*.go`, `server/idempotency_sweeper.go`, phase10 E2E | Implemented; integration E2E is Docker-gated |

## 4. New release baseline: R0–R5

| Release | Required outcome | Current status | Evidence/gap |
|---|---|---|---|
| R0 Foundation | Auth, tenant, DB, API, worker, security, CI/CD | **Partial*** | Core modules and migrations exist, but Go/Docker verification and CI config consistency block release sign-off |
| R1 Journal MVP | Account, import, journal, Cloudinary, dashboard | **Partial*** | Legacy Phase 1/3 surfaces cover the scope, but import/P&L/tenant/Cloudinary gates and frontend checks are not signed off |
| R2 Trading Intelligence | Analytics + behavior/profile + strategy/versioning | **Partial** | Analytics exists; behavior profile, behavior detection, strategies and strategy versions are absent |
| R3 AI Research | AI analysis + regime + Backtest/OOS/WFA | **Partial** | AI analysis/recap/chat exist; market regime, candidate strategy generation and validation loop are absent |
| R4 AI Trading Beta | Paper + signal + semi-auto + risk engine | **Not started** | No paper order, signal, user approval, hard risk boundary or execution preview path |
| R5 Autonomous Trading | Full execution, fail-safe, AI journal, continuous improvement | **Not started** | MetaApi adapter only handles account connection/history; no order lifecycle or live loop |

`*` Code presence is supported by repository evidence, but release readiness is incomplete. Capability-level implementation and release-level sign-off are intentionally separate.

## 5. New Phase 0–16 matrix

| New phase | Target capability | Status | Evidence / missing gate | Next dependency |
|---:|---|---|---|---|
| 0 | Foundation & Architecture | Partial | Foundation exists; stale entry docs, CI/database mismatch and unavailable local Go verification | Clean baseline and verification contract |
| 1 | SaaS Core | Partial | Auth/plans/subscriptions/accounts/admin exist; generic usage counters and complete release verification are not evidenced | R0/R1 release sign-off |
| 2 | MT5 Import Engine | Partial | Orders/deals/positions/trades, parser, normalize/dedup and import jobs exist; money representation and real-fixture correctness gates remain | Canonical money/data correctness |
| 3 | Journal + Cloudinary | Implemented | Journal/tags/images and Cloudinary abstraction/routes exist; execute tenant/security/image-limit verification | R1 gate |
| 4 | Analytics Engine | Partial | Daily metrics and symbol/session/EA/Magic/equity surfaces exist; complete risk dashboard/regression sign-off is not evidenced | R2 analytics gate |
| 5 | Trading Behavior Intelligence | Not started | No behavior profile/detection/best-worst setup/risk pattern domain or service found | Phase 4 normalized metrics |
| 6 | Strategy Builder & Versioning | Not started | No strategies/strategy_versions model, builder, version traceability or trade linkage found | Phase 5 behavior features |
| 7 | AI Strategy Intelligence | Not started | Existing AI is commentary/recap/chat; no candidate strategy/pattern/optimization pipeline | Phase 6 executable strategy model |
| 8 | Market Intelligence | Not started | No market snapshots, OHLC/indicator/regime pipeline found | Phase 6–7 strategy context |
| 9 | Backtest & Validation | Not started | No backtest/OOS/WFA/stress/performance gate implementation found | Phases 6–8 |
| 10 | Paper Trading | Not started | No virtual execution/paper orders/virtual P&L/risk limits found | Phase 9 validation gates |
| 11 | AI Signal | Not started | No signal engine/confidence/signal history/decision record loop found | Phase 10 paper evidence |
| 12 | Semi-Auto | Not started | No approval/preview/test execution flow found | Phase 11 signals + Phase 13 risk |
| 13 | Risk & Execution Engine | Not started | No hard risk limits/position sizing/emergency stop/order state engine found | Phase 12 safety contract |
| 14 | Autonomous Live Trading | Not started | MetaApi provider has no order lifecycle; no live permissions/fail-safe loop | Phase 13 + broker execution adapter |
| 15 | AI Trade Journal & Continuous Improvement | Not started | No auto trade journal/degradation/candidate-validation feedback loop found | Phase 14 audited live results |
| 16 | Scale / Multi-account / Global Intelligence | Partial | Multi-account SaaS foundation exists; AI per-account/shared strategies and aggregate learning are absent | Phase 15 stable loop |

## 6. Cross-cutting capability inventory

| Area | Found | Interpretation |
|---|---|---|
| Tenant isolation | Typed tenant context and tenant-scoped SQL patterns | Foundation capability; must remain a release gate |
| Money | Database numeric columns exist, but several Go domain structs use `float64` | Invariant violation requiring future hardening before financial validation claims |
| Time/IDs | UTC/timestamptz and UUID usage are present | Preserve in future domain additions |
| MetaApi | REST handshake, history fetch and Ping | `TradingDataProvider`-like history integration; not `ExecutionProvider` |
| AI | LLM providers, embeddings, recap, chat, shared budget | Research/support platform; no trading decision authority |
| Audit | Existing audit log and API-key write audit path | Extend for decision/risk/execution records only after those boundaries exist |
| Async work | In-process worker pool and phase-specific workers | Useful vertical-slice foundation; future live execution needs durable/safe state semantics |
| Public API | Read and write scopes, idempotency, rate limits | Cross-cutting integration surface; does not imply trading execution permission |

## 7. Findings and remediation queue

| ID | Priority | Finding | Evidence | Recommended follow-up |
|---|---|---|---|---|
| F-001 | Important | `README.md` still says “Phase 0 — Foundation (in progress)” and shows obsolete four-phase roadmap | `README.md` status and `## Phase roadmap` | Task in this rebaseline: update links/status |
| F-002 | Important | Root `Makefile` hard-codes `D:\bot\Trading Journal` instead of resolving the current repository | `Makefile` `ROOT :=` | **Adjusted in this pass**: uses `$(CURDIR)` and quotes paths with spaces; `make help` passed |
| F-003 | Important | API CI uses `postgres:16-alpine` and does not set `INTEGRATION_TESTS=on`; Phase 9 E2E requires `pgvector/pgvector:pg16`, while Phase 10 integration paths are Docker-gated | `.github/workflows/api-ci.yml`, `phase9_e2e_test.go`, `phase10_e2e_test.go` | **Adjusted in this pass**: CI/local/E2E Postgres paths use pgvector, `INTEGRATION_TESTS='on'`, and tagged integration compilation passes; GitHub execution remains pending |
| F-004 | Important | Local host did not have `go` on PATH, so the first API verification attempt was blocked | Initial `go test ./...` returned command-not-found | **Adjusted in this pass**: Go 1.26 portable toolchain was used against `go.mod`; API tests, tagged integration compile, build, vet, and golangci-lint now pass. Local `-race` and Docker-backed execution remain environment-gated |
| F-005 | Important | Local compose uses `postgres:16-alpine`, but migration `0010_phase9` requires pgvector | `infra/docker-compose.yml`, `migrations/0010_phase9.up.sql` | **Adjusted in this pass**: local Postgres now uses `pgvector/pgvector:pg16`; Docker migration verification remains pending |
| F-006 | Important | Money-bearing domain structs contain `float64`, contrary to project invariant | `internal/domain/deal/deal.go`, `trade/trade.go`, `position/position.go`, `daily_metric/daily_metric.go` | **Partially adjusted**: MetaApi → canonical integer conversion now rounds instead of truncating; a domain/store/API money representation migration is still required before financial backtest/live claims |
| F-007 | Minor | Frontend test inventory currently has no `*.test.ts`/`*.spec.ts` files; dependency installation was interrupted during local verification | `apps/web/package.json`, test file inventory | **Adjusted in this pass**: added a shared API-scope contract test; 1 test passes |
| F-008 | Important | MetaApi integration is historical/account-state only; provider interface has no order open/modify/close methods | `internal/integration/broker/provider.go`, `metaapi.go` | Do not label R4/R5 complete; design ExecutionProvider behind Risk Engine |
| F-009 | Important | No Strategy, Strategy Version, Market Snapshot, Decision Record, Paper Order or Risk Event production model was found | `migrations/`, `internal/domain/`, `internal/store/` inventory | Start with R2 Behavior → Strategy vertical slice after roadmap approval |
| F-010 | Important | Frontend lint uses ESLint 9 but no `eslint.config.js|mjs|cjs` exists | `apps/web/package.json`, direct `node_modules/.bin/eslint .` | **Adjusted in this pass**: added ESLint flat config plus Vue/TypeScript parser dependencies; lint exits 0 with no errors |
| F-011 | Important | Frontend typecheck currently reports unresolved Nuxt aliases/shared package types plus existing component/type errors | Direct `node_modules/.bin/nuxt typecheck` | **Adjusted in this pass**: aligned `srcDir` with `app/`, source-linked shared DTO alias, fixed analytics contracts and component errors; typecheck passes |

## 8. Verification record

| Check | Result |
|---|---|
| Source roadmap extraction | Completed with a standard-library DOCX XML reader because `pandoc` is unavailable |
| Repository status before docs work | Clean apart from the plan/spec artifacts created for this request |
| `go test -short ./...` | **Passed** — Go 1.26 portable toolchain, `CGO_ENABLED=0`; all API packages and Docker-gated E2E package passed/skipped as designed |
| `go test -tags=integration -run '^$' ./...` | **Passed** — all integration-tagged packages compile, including Phase 9/10 E2E sources |
| `go build ./...` | **Passed** — Go 1.26 |
| `go vet ./...` | **Passed** — Go 1.26 |
| `golangci-lint run --timeout 5m ./...` | **Passed** — golangci-lint v1.64.8 under Go 1.26 |
| MetaApi broker mapping regression test | **Passed** — fractional price/money values round to canonical integer units without truncation |
| `make lint` | **Passed** — API golangci-lint + frontend ESLint, exit 0 |
| `make build` | **Passed** — API binary + Nuxt/Nitro/PWA production output, exit 0 |
| `make test` | **Not run to completion locally** — repository target forces `-race`, but this Windows host has `CGO_ENABLED=0` and no C compiler; non-race API tests pass |
| `make help` verification | **Passed** — repository-local root resolution works from the current path, including spaces |
| Frontend dependency install | **Prepared** — lockfile updated for lint parsers; CI now installs from `apps/web` with `--frozen-lockfile --ignore-scripts` and explicitly runs `nuxt prepare` |
| Frontend lint | **Passed** — ESLint 9 flat config exits 0 with no errors |
| Frontend typecheck | **Passed** — `pnpm typecheck` exits 0 |
| Frontend unit tests | **Passed** — 1 Vitest contract test passes |
| Frontend production build | **Passed** — `pnpm build` exits 0 and generates client/server/PWA output; chunk-size and Node deprecation warnings remain |
| Docker/pgvector E2E | Not run locally; Docker engine is not running. CI configuration and test source now use the required pgvector image |
| Source DOCX preservation | Original path remains untouched; hash validation is part of the revised DOCX task |

## 9. Deferred items

The following remain deferred from the existing project context and are not silently pulled into this roadmap audit: `write:recaps`, `write:chat`, destructive `delete:trades` public scope, bulk operations, webhook replay/filtering/inbound delivery, per-key rate overrides, AI streaming/function calling/image/voice/export, recap email/multi-account rollups, local embedding model, Stripe Connect and MetaApi streaming.

## 10. Recommended next WBS slice

### Gate 0 progress after the adjustment pass

The repository-path, pgvector, CI integration-test, Go compile/lint, frontend alias/typecheck, ESLint, and baseline-unit-test items are now adjusted in the working tree. The remaining Gate 0 blockers are Docker-backed API/pgvector execution and money representation hardening. Those are intentionally not marked complete without a real database run and financial regression suite.

After this rebaseline is accepted, the work should proceed in two gates:

### Gate 0 — Release hardening before new product work

1. Make the repository quickstart portable by removing the hard-coded root path from `Makefile`.
2. Repair frontend dependency/workspace setup, ESLint flat config, Nuxt aliases/shared package type generation, and baseline unit tests.
3. Make local/CI Postgres use pgvector where Phase 9 migrations/E2E require it; add an explicit Docker-gated integration job.
4. Run API race tests and Docker-backed pgvector integration in CI/local Docker; fix money representation before financial validation work.

### Gate 1 — R2 / Phase 5–6 product slice

1. Define behavior metrics and a tenant/account-scoped `TradingBehaviorProfile` read model.
2. Add deterministic behavior detectors with regression fixtures from normalized trades.
3. Define `Strategy` and immutable `StrategyVersion` with explicit rule schema.
4. Link journal/trades to a strategy version without changing live execution.
5. Add regression and tenant-isolation gates before starting AI Strategy Candidate generation.

No MetaApi order execution should be added until the later Backtest → OOS → Walk Forward → Paper → Risk → Semi-Auto gates are independently accepted.
