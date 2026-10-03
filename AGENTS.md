# AGENTS.md — Trading Journal Project Context

> Read this **before** doing any non-trivial work. It documents invariants for code, current phase scope, and where things live. Phase-specific agent docs may live alongside this one (e.g. `apps/api/AGENTS.md`, `apps/web/AGENTS.md`).

---

## Project shape

- **Monorepo:** `apps/api` (Go), `apps/web` (Nuxt 3), `packages/shared` (TS), `infra` (docker-compose + env), `docs/` (specs)
- **Language policy:** English for code, identifiers, log keys, error codes, commit messages. Free Thai for chat with the user (this repo's owner prefers Thai).
- **Brand:** "Trading Journal" (working name). Future split possible.

## Current phase

**Phase 0 — Foundation.** Spec: `docs/superpowers/specs/2026-10-03-trading-journal-phase0-design.md`.

**In scope:** repo skeleton · 9 tables (7 base + 2 OAuth) · auth flows (email/password + OAuth Google/Line) · tenant context · RBAC · JWT (EdDSA) · argon2id · refresh session rotation · audit log · rate limit · PWA shell · observability (slog + healthz/readyz/livez) · docker-compose · GitHub Actions · unit + integration tests + 1 Playwright happy path.

**Out of scope (defer to later phases):**
- MT5 HTML/CSV parser · trade data model · Cloudinary signed upload
- MetaApi integration · account connection · sync worker
- Multi-account dashboard · session/EA/magic analysis · PDF reports
- Billing automation · Stripe/Omise
- AI analysis

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
- **CSP:** `'self' + res.cloudinary.com` for images (Phase 1 adds more).
- **PWA:** `@vite-pwa/nuxt`. `autoUpdate`. Icons in `public/icons/`. Manifest aligns with dark palette.
- **i18n:** English-only in Phase 0. Thai copy inline as needed; full i18n in Phase 4.
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
| Spec source (MASTER PROMPT) | `pasted-text.txt` in workspace root or repo root |
| Migrations | `apps/api/migrations/` |
| sqlc queries | `apps/api/queries/` |
| Domain entities | `apps/api/internal/domain/<entity>/` |
| HTTP handlers | `apps/api/internal/httpapi/` |
| Middleware chain | `apps/api/internal/server/` |
| OAuth providers | `apps/api/internal/integration/oauth/` |
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
- **Adding a new tenant-scoped table:** migration with `tenant_id UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE`, query in `queries/<table>.sql`, repo wrapper, service use case. **Every query must include `WHERE tenant_id = $ctxTenantID`.**
- **Adding a new plan:** insert row in `plans` migration (or admin endpoint Phase 0 deferred to Phase 4).
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