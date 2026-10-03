# Trading Journal — Phase 0 Design

| | |
|---|---|
| **Status** | ✅ Approved 2026-10-03 (all sections) — ready for implementation plan |
| **Date** | 2026-10-03 |
| **Phase** | 0 — Foundation |
| **Scope** | Repo skeleton · PostgreSQL schema (9 tables) · Auth · Tenant · RBAC · OAuth · Observability · Docker compose · CI · PWA shell |
| **Out of scope (later phases)** | MT5 parser · MetaApi integration · Cloudinary upload · Billing automation · Admin dashboard (real data) |
| **Spec source** | `pasted-text.txt` MASTER PROMPT for Trading Journal SaaS |

---

## 1. Executive Summary

Bootstrap a **greenfield, monorepo, multi-tenant SaaS** for a Trading Journal. Phase 0 produces a production-grade skeleton: register → login → tenant-scoped `/v1/me` flow working end-to-end on a freshly cloned dev environment, with all the foundations in place to add Phase 1 (MT5 upload + parser + analytics) without re-platforming.

**Stack chosen (2026-10-03):**
- Monorepo (apps/api + apps/web + packages/shared + infra)
- Backend: Go + Echo + slog + sqlc + golang-migrate
- Database: PostgreSQL 16
- Cache/queue infra: Redis 7 (rate limit only in Phase 0)
- Frontend: Nuxt 3 + Vue 3 + Vuetify 3 + Pinia + TypeScript
- PWA: `@vite-pwa/nuxt` (Workbox)

---

## 2. Repo Layout

```
trading-journal/
├── apps/
│   ├── api/                                # Go + Echo
│   │   ├── cmd/api/main.go                 # entrypoint
│   │   ├── internal/
│   │   │   ├── config/                     # env loading (envconfig)
│   │   │   ├── server/                     # Echo server, middleware wiring, graceful shutdown
│   │   │   ├── auth/                       # JWT (EdDSA), argon2id, refresh sessions
│   │   │   ├── tenant/                     # tenant context propagation
│   │   │   ├── rbac/                       # role/permission checks
│   │   │   ├── observability/              # slog JSON, request id, error envelope
│   │   │   ├── httpapi/                    # handlers, validators, dto
│   │   │   │   ├── auth/
│   │   │   │   └── me/
│   │   │   ├── domain/                     # entities + typed domain errors
│   │   │   │   ├── user/
│   │   │   │   ├── tenant/
│   │   │   │   ├── membership/
│   │   │   │   ├── plan/
│   │   │   │   └── subscription/
│   │   │   ├── service/                    # use cases (orchestration only)
│   │   │   └── store/                      # sqlc-generated + thin repo wrappers
│   │   ├── migrations/                     # golang-migrate *.up.sql / *.down.sql
│   │   ├── queries/                        # sqlc *.sql (one file per table)
│   │   ├── sqlc.yaml
│   │   ├── tools/tools.go                  # go:generate tooling pin
│   │   ├── go.mod
│   │   ├── go.sum
│   │   └── Dockerfile
│   └── web/                                # Nuxt 3 + Vue 3 + Vuetify 3 + Pinia + TS + PWA
│       ├── app/
│       │   ├── pages/
│       │   │   ├── index.vue               # Landing
│       │   │   ├── login.vue
│       │   │   ├── register.vue
│       │   │   ├── offline.vue             # PWA offline fallback
│       │   │   └── dashboard.vue           # auth-guarded, shows me
│       │   ├── layouts/default.vue         # app shell (dark, responsive)
│       │   ├── components/
│       │   │   ├── AppHeader.vue            # includes <PwaInstallButton/>
│       │   │   ├── PwaInstallButton.vue
│       │   │   └── PwaUpdateToast.vue
│       │   ├── composables/
│       │   │   ├── useApi.ts
│       │   │   ├── useAuth.ts
│       │   │   ├── useTenant.ts
│       │   │   └── usePwaUpdate.ts
│       │   ├── stores/auth.ts              # Pinia auth store
│       │   ├── plugins/vuetify.ts          # dark theme preset
│       │   └── middleware/auth.global.ts
│       ├── public/
│       │   ├── icons/
│       │   │   ├── icon-192.png
│       │   │   ├── icon-512.png
│       │   │   ├── icon-maskable-512.png
│       │   │   └── favicon.svg
│       │   └── offline.html
│       ├── nuxt.config.ts
│       ├── tsconfig.json
│       ├── package.json
│       └── Dockerfile
├── packages/
│   └── shared/                             # shared TS types for FE↔BE contract
│       ├── src/index.ts                    # exported dto, role enums, error codes
│       ├── tsconfig.json
│       └── package.json
├── infra/
│   ├── docker-compose.yml                  # postgres + redis + mailhog
│   └── .env.example                        # all required envs documented
├── docs/
│   └── superpowers/
│       └── specs/2026-10-03-trading-journal-phase0-design.md  # this file
├── .github/workflows/
│   ├── api-ci.yml                          # lint + build + test
│   └── web-ci.yml                          # lint + typecheck + build
├── .gitignore
├── .editorconfig
├── AGENTS.md
├── README.md
└── LICENSE
```

## 3. Backend Architecture

### 3.1 Layering

```
httpapi.Handler
   ↓ calls
service.AuthService.Register(ctx, input) → (user, tenant, tokens, error)
   ↓ orchestrates
domain.User, domain.Tenant, domain.Plan
   ↓ persisted via
store.UserStore.Create(ctx, ...)   ← generated by sqlc
   ↓
PostgreSQL
```

### 3.2 Middleware chain order

```
RequestID (UUIDv7 per request)
  → Recovery (panic → 500, structured log)
  → CORS (configurable allowlist)
  → RateLimit (Redis token bucket, per IP; per user_id when authed)
  → AccessLog (slog JSON: request_id, method, path, status, latency, user_id, tenant_id)
  → Auth (route-group scope; populates user/tenant into ctx)
  → RBAC (per-route permission requirement)
```

### 3.3 Tenant isolation invariant

Every domain entity carries `tenant_id` (except `plans` which are global). The `store` layer reads `tenant_id` from `context.Context` via a typed key — handlers MUST populate this context. Any query that bypasses the tenant context is a CI-lint error (checked via `golangci-lint` custom linter in Phase 1, or via `go vet` rules).

### 3.4 API style

- REST + JSON
- Versioned `/v1/` prefix
- Uniform error envelope: `{"error": {"code":"STRING_CODE","message":"...","details":{}}}`
- Money: stored in `numeric(18,8)` + minor units (cents/satoshi) in JSON. No floats.
- Timestamps: stored as UTC `timestamptz` in DB, displayed in Bangkok (`Asia/Bangkok`) in FE.

## 4. Frontend Architecture

### 4.1 Stack details

- **State:** Pinia stores per domain (`auth`, `tenant`)
- **HTTP:** `$fetch` with `useApi` composable that injects `Authorization: Bearer <accessToken>`, maps HTTP errors → typed `ApiError` with `code`, auto-refreshes on 401 once then surfaces login
- **Forms:** VeeValidate 4 + Zod schemas (shared with `packages/shared` where possible)
- **Styling:** Vuetify 3 with custom dark theme palette:
  - `background: #0B0F19`
  - `surface: #121826`
  - `primary: #3B82F6` (also PWA `theme_color`)
- **Routing:** file-based via Nuxt; `auth.global.ts` redirects unauth users to `/login`

### 4.2 PWA configuration

Module: `@vite-pwa/nuxt`. Workbox-powered.

```ts
pwa: {
  registerType: 'autoUpdate',
  manifest: {
    name: 'Trading Journal',
    short_name: 'TJ',
    description: 'Multi-tenant trading journal SaaS',
    theme_color: '#3B82F6',
    background_color: '#0B0F19',
    display: 'standalone',
    orientation: 'portrait',
    start_url: '/',
    scope: '/',
    icons: [
      { src: '/icons/icon-192.png', sizes: '192x192', type: 'image/png' },
      { src: '/icons/icon-512.png', sizes: '512x512', type: 'image/png' },
      { src: '/icons/icon-maskable-512.png', sizes: '512x512', type: 'image/png', purpose: 'maskable' }
    ]
  },
  workbox: {
    navigateFallback: '/',
    cleanupOutdatedCaches: true,
    runtimeCaching: [
      { urlPattern: ({ request }) => ['document','script','style','image','font'].includes(request.destination),
        handler: 'StaleWhileRevalidate', options: { cacheName: 'tj-shell-v1' } },
      { urlPattern: /^\/v1\//,
        handler: 'NetworkFirst',
        options: { cacheName: 'tj-api-v1', networkTimeoutSeconds: 5,
                   expiration: { maxEntries: 50, maxAgeSeconds: 300 } } }
    ]
  },
  client: { installPrompt: true, periodicSyncForUpdates: 3600 }
}
```

### 4.3 Phase 0 PWA scope

| Capability | Phase 0 |
|---|---|
| Installable (manifest + icons + HTTPS / localhost) | ✅ |
| App shell offline (landing/login/register static) | ✅ |
| Service Worker auto-update | ✅ |
| Custom install button in header | ✅ |
| Offline fallback page | ✅ |
| Offline trade-journal drafting | ❌ (Phase 4) |
| Push notifications | ❌ (Phase 4) |
| Background sync | ❌ (Phase 4) |

### 4.4 Icons (Phase 0 deliverable)

Generated via SVG monogram "TJ" + `sharp` render. Visual mockup reviewed before commit.

## 5. Tech Decisions (Defaults)

| Decision | Default | Rationale |
|---|---|---|
| JWT algorithm | EdDSA / Ed25519 | short keys, fast verify, no HS256 secret-sharing problem |
| Password hashing | argon2id | OWASP-recommended 2026, GPU-resistant |
| Refresh token | Rotating opaque token + sha256 hash in DB | detect reuse, no JWT-rotation footgun |
| Access token TTL | 15 min | balance UX vs blast radius |
| Refresh token TTL | 7 days (sliding) | standard SaaS pattern |
| CI | GitHub Actions | standard; flexible to GitLab in Phase 4 |
| Email (dev) | MailHog in docker-compose | inspectable in UI |
| Email (prod) | Resend (config-driven) | simple API, Thai-friendly |
| Queue | Deferred to Phase 1 | Phase 0 has no background jobs |
| API style | REST + JSON, `/v1/` | spec §7-8 fixed |
| Error envelope | `{"error": {"code","message","details"}}` | uniform across layers |
| Money type | `numeric(18,8)` + minor units | no floats for money |
| Timestamps | UTC `timestamptz` in DB, Bangkok display in FE | Bangkok user base, no DST bugs |
| Logging | slog JSON to stdout | 12-factor |

---

## 6. Data Model (Section 2 — ✅ approved 2026-10-03)

### 6.1 ER overview

```
plans (global, no tenant_id)
  ↓ referenced by
subscriptions >── tenants ──< memberships >── users
                                          │
                                          ├── refresh_sessions (1:N)
                                          │
                          (actor_user_id FK) <──── audit_logs (append-only, immutable)
                          (actor_tenant_id FK)
```

### 6.2 Tables (7)

| Table | Tenant-scoped? | Soft delete? | Phase 0 use |
|---|---|---|---|
| `plans` | global (no `tenant_id`) | no (active flag) | seeded at migration |
| `tenants` | yes | yes (`deleted_at`) | workspace container |
| `users` | no (cross-tenant identity) | yes | identity |
| `memberships` | yes (FK) | no (status enum) | user ↔ tenant ↔ role |
| `subscriptions` | yes (FK) | no (status enum) | tenant → plan |
| `refresh_sessions` | yes (FK) | no (status enum) | rotating tokens |
| `audit_logs` | partial (FK, nullable) | append-only | event log |

### 6.3 Key DDL choices

- **UUIDv7** for all public IDs (`users`, `tenants`, `memberships`, `subscriptions`, `refresh_sessions`, `plans`) — sortable, no order-leak.
- **`audit_logs.id` = `BIGSERIAL`** — internal-use, lighter index, naturally time-ordered.
- **`citext`** extension for `users.email` — case-insensitive uniqueness.
- **`pgcrypto`** extension for `gen_random_uuid()` in seed.
- **`INET`** for IP columns (indexable, IPv6-ready).
- **`JSONB`** for `metadata` on every table — extensible without migrations.
- **`JSONB metadata`** on `plans` stores entitlements (Phase 1+ reads via `EntitlementService`).
- **One active subscription per tenant** enforced via partial unique index:
  ```sql
  CREATE UNIQUE INDEX uniq_subscriptions_tenant_active
    ON subscriptions(tenant_id) WHERE status IN ('active','trialing');
  ```
- **Refresh session rotation chain** via `refresh_sessions.replaced_by_session_id` (OWASP reuse-detection).
- **No `ON DELETE CASCADE`** on `audit_logs.actor_*` — preserve history.

### 6.4 Seed data (migration 0001)

```sql
INSERT INTO plans (id, code, name, description, price_cents, currency, interval, is_active) VALUES
  (gen_random_uuid(), 'free',            'Free',            'ทดลองใช้ฟรี',            0,     'THB', 'monthly', TRUE),
  (gen_random_uuid(), 'upload_journal',  'Upload Journal',  'อัปโหลด MT5 history',    9900,  'THB', 'monthly', TRUE),
  (gen_random_uuid(), 'starter',         'Starter',         'MT5 Auto Sync',          19900, 'THB', 'monthly', TRUE),
  (gen_random_uuid(), 'pro',             'Pro',             'Multi-account + Advanced', 39900,'THB','monthly', TRUE)
ON CONFLICT (code) DO NOTHING;
```

### 6.5 sqlc query files

```
apps/api/queries/
├── plans.sql             -- PlanStore.Get, ListActive
├── tenants.sql           -- TenantStore.Create, GetByID, GetBySlug
├── users.sql             -- UserStore.Create, GetByEmail, GetByID
├── memberships.sql       -- MembershipStore.Create, GetByUserAndTenant
├── subscriptions.sql     -- SubscriptionStore.Create, GetActiveByTenant
└── refresh_sessions.sql  -- RefreshStore.Create, GetByHash, Revoke, Rotate
```

`audit_logs` writes via direct `tx.Exec` (insert-only hot path, no type-safe query needed).

### 6.6 Coverage map vs MASTER PROMPT §9

| §9 table | Phase 0 | Phase |
|---|---|---|
| users / tenants / memberships | ✅ | 0 |
| plans / subscriptions | ✅ | 0 |
| audit_logs | ✅ | 0 |
| **(added)** refresh_sessions | ✅ | 0 (rotating-token pattern) |
| subscription_events | ⏭️ | 4 (billing automation) |
| trading_accounts / account_connections | ⏭️ | 1 / 2 |
| imported_files / import_jobs / orders / deals / positions / trades | ⏭️ | 1 |
| trade_tags / journal_entries / trade_images / daily_metrics / equity_snapshots | ⏭️ | 1 |
| sync_cursors / sync_events | ⏭️ | 2 |
| usage_counters | ⏭️ | 4 |

---

## 7. Security Model (Section 3 — ✅ approved 2026-10-03)

### 7.1 Auth flows (email/password)

**Register** — single tx: insert user + tenant + membership (owner) + subscription (free) → issue EdDSA access (15 min) + opaque refresh (32 bytes base64url, sha256 hashed in DB) → audit_logs `user.registered` + `tenant.created` → 201 with `{user, tenant, plan, tokens}`.

**Login** — find by email, **constant-time dummy hash on miss** (no timing leak) → pick primary membership → issue tokens → audit_logs `user.login`.

**Refresh (rotating)** — sha256 lookup → if revoked → **revoke ALL active sessions for that user + audit `user.refresh_reuse_detected`**; else mark old `revoked`+`replaced_by=new.id` and insert new session.

**Logout** — read `sid` from JWT → revoke matching refresh_sessions row → audit_logs `user.logout` → 204.

**Me** — resolve user/tenant/role from JWT → fetch user + tenant + active subscription.plan → 200.

### 7.2 JWT structure (EdDSA / Ed25519)

```json
Header:  { "alg": "EdDSA", "kid": "v1", "typ": "JWT" }
Payload: {
  sub: "<user_uuid_v7>",  tid: "<tenant_uuid_v7>",
  sid: "<session_uuid_v7>", rol: "owner",
  iat, exp, iss: "trading-journal", aud: "trading-journal-web"
}
```

EdDSA keypair generated on first boot → `apps/api/keys/ed25519.{priv,pub}.pem`. `kid` supports rotation.

### 7.3 Tenant context propagation

```go
type ctxKey int
const tenantKey ctxKey = iota
func WithTenantID(ctx, id)   // set
func TenantIDFromContext(ctx) (uuid, bool) // get
```

Handler reads `tid` from JWT → `WithTenantID(ctx, tid)` → store layer reads via ctx → every query MUST include `WHERE tenant_id = $ctx`. Missing ctx → `ErrTenantMissingFromContext` (programmer error, 500).

### 7.4 RBAC matrix (Phase 0)

| Resource / Action | owner | admin | member | viewer |
|---|:-:|:-:|:-:|:-:|
| Read own profile | ✅ | ✅ | ✅ | ✅ |
| Update own profile | ✅ | ✅ | ✅ | ✅ |
| Read tenant | ✅ | ✅ | ✅ | ✅ |
| Update tenant settings | ✅ | — | — | — |
| Invite users | ✅ | — | — | — |
| Manage subscription | ✅ | — | — | — |
| Read audit logs | ✅ | — | — | — |

Phase 0 = single owner per tenant (multi-user is Phase 1+, matrix ready). Routes use `RequireRole("owner")` decorators.

### 7.5 Error mapping

Envelope:
```json
{ "error": { "code": "VALIDATION_FAILED", "message": "...", "details": {} } }
```

| Domain error | HTTP | code |
|---|---|---|
| `ErrValidation` | 422 | `VALIDATION_FAILED` |
| `ErrUserNotFound` | 404 | `USER_NOT_FOUND` |
| `ErrEmailTaken` | 409 | `EMAIL_ALREADY_EXISTS` |
| `ErrInvalidCredentials` | 401 | `INVALID_CREDENTIALS` (generic) |
| `ErrPasswordTooWeak` | 422 | `PASSWORD_TOO_WEAK` |
| `ErrRefreshTokenInvalid` | 401 | `REFRESH_TOKEN_INVALID` |
| `ErrRefreshTokenExpired` | 401 | `REFRESH_TOKEN_EXPIRED` |
| `ErrRefreshTokenReuse` | 401 | `REFRESH_TOKEN_REUSE_DETECTED` |
| `ErrTenantNotFound` | 404 | `TENANT_NOT_FOUND` |
| `ErrMembershipNotFound` | 403 | `MEMBERSHIP_NOT_FOUND` |
| `ErrInsufficientRole` | 403 | `INSUFFICIENT_ROLE` |
| `ErrTenantMissingFromContext` | 500 | `INTERNAL_ERROR` (loud log) |
| `ErrRateLimited` | 429 | `RATE_LIMITED` (+ `Retry-After`) |
| `ErrUnauthorized` | 401 | `UNAUTHORIZED` |
| (other) | 500 | `INTERNAL_ERROR` |

### 7.6 Rate limiting (Redis token bucket, sliding 60s window)

| Scope | Key | Limit |
|---|---|---|
| `/v1/auth/*` per IP | `ratelimit:auth-ip:{ip}` | 60 / min |
| Authed per user | `ratelimit:user:{user_id}` | 600 / min |
| Authed per tenant | `ratelimit:tenant:{tenant_id}` | 6000 / min |

Headers: `X-RateLimit-Limit`, `X-RateLimit-Remaining`, `X-RateLimit-Reset`.

### 7.7 Audit logging contract

`audit_logs` is **append-only at service layer** (no UPDATE/DELETE allowed from app code). Every protected mutation writes ≥1 audit row in the same tx (rollback-safe).

Phase 0 actions: `user.registered`, `tenant.created`, `user.login`, `user.login_failed`, `user.refresh`, `user.refresh_reuse_detected`, `user.logout`, `user.identity_linked` (OAuth flow), `oauth.callback_completed`.

### 7.8 Security headers

```
Strict-Transport-Security: max-age=31536000; includeSubDomains
X-Content-Type-Options: nosniff
X-Frame-Options: DENY
Referrer-Policy: strict-origin-when-cross-origin
Content-Security-Policy: default-src 'self'; img-src 'self' https://res.cloudinary.com data:; ...
```

---

## 7A. OAuth Provider Login (Section 3.5 — ✅ approved 2026-10-03)

### 7A.1 Providers (Phase 0)

Google + Line (Thailand-focused, broadest reach). GitHub defer to Phase 1+.

### 7A.2 Schema additions (migration 0002_oauth)

```sql
CREATE TYPE identity_provider AS ENUM ('google','line','github');

CREATE TABLE user_identities (
  id            UUID PRIMARY KEY,
  user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  provider      identity_provider NOT NULL,
  provider_sub  TEXT NOT NULL,
  email         CITEXT,
  email_verified BOOLEAN NOT NULL DEFAULT FALSE,
  display_name  TEXT,
  avatar_url    TEXT,
  raw_profile   JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_used_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (provider, provider_sub)
);

CREATE TABLE oauth_states (
  state_token   TEXT PRIMARY KEY,
  flow          TEXT NOT NULL,        -- 'login' | 'link'
  redirect_uri  TEXT,
  code_verifier TEXT,                  -- PKCE verifier (server-side storage)
  ip            INET,
  user_agent    TEXT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at    TIMESTAMPTZ NOT NULL,  -- 10 min
  consumed_at   TIMESTAMPTZ
);
```

### 7A.3 Provider abstraction

```go
type Provider interface {
    Name() identity_provider
    AuthorizeURL(state, codeChallenge, redirectURI string) string
    ExchangeCode(ctx, code, codeVerifier, redirectURI string) (TokenResponse, error)
    FetchUserInfo(ctx, accessToken string) (UserInfo, error)
}
```

Implementations: `GoogleProvider`, `LineProvider`. Registered via DI; missing env → not exposed.

### 7A.4 Flow (Authorization Code + PKCE, server-side `code_verifier`)

```
FE: click "Continue with Google"
  → GET /v1/auth/oauth/{provider}/start
  BE: generate state + code_verifier → INSERT oauth_states (10min TTL) → 302 to provider authorize URL
Provider: user authorizes → 302 /v1/auth/oauth/{provider}/callback?code&state
  BE: lookup oauth_states → exchange code → fetch userinfo → upsert user_identities
       (link by verified email OR create-new-with-tx-email-unverified)
  BE: issue our JWT + refresh → mark state consumed
  BE: 302 /auth/oauth-complete?access=...&refresh=...
FE: store tokens via Pinia → redirect /dashboard
```

### 7A.5 Account linking policy

| Scenario | Action |
|---|---|
| `email_verified=true` + email matches existing user | **Auto-link** identity → existing user |
| `email_verified=false` | Create new user (`pending_verification` status — routes allowed, verification deferred to Phase 4) |
| Existing user already has identity for this provider | Sign in directly (no new row) |
| Email conflict but existing user unverified | Create separate user |

Auto-link logs `user.identity_linked` to audit_logs.

### 7A.6 Env vars

```env
OAUTH_GOOGLE_CLIENT_ID / OAUTH_GOOGLE_CLIENT_SECRET / OAUTH_GOOGLE_REDIRECT_URI
OAUTH_LINE_CLIENT_ID   / OAUTH_LINE_CLIENT_SECRET   / OAUTH_LINE_REDIRECT_URI
```

Missing env → provider not registered → button hidden on FE.

### 7A.7 FE

- `OAuthButtonGroup.vue` on `/login` and `/register` (below email/password form)
- `/auth/oauth-complete` route receives tokens via URL → feeds into Pinia
- `useOAuth.ts` composable: `start(provider) → window.location.href = /v1/auth/oauth/{provider}/start`

---

## 8. Observability + Local Dev + CI (Section 4 — ✅ approved 2026-10-03)

### 8.1 Observability

| Layer | Phase 0 | Later |
|---|---|---|
| **Logs** | `slog` JSON to stdout · always includes `request_id` + `user_id`/`tenant_id` when authed · never logs secrets (password, tokens, refresh) | — |
| **Health** | `/healthz` (process alive), `/livez` (k8s liveness), `/readyz` (Postgres + Redis up) | — |
| **Metrics** | structured logs only | Prometheus + Grafana (Phase 4) |
| **Error tracking** | slog `error` with stack | Sentry (Phase 4) |
| **Tracing** | request_id via middleware | OpenTelemetry (Phase 4) |

### 8.2 Local dev (docker-compose)

```yaml
# infra/docker-compose.yml
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_DB: tj
      POSTGRES_USER: tj
      POSTGRES_PASSWORD: devpass
    ports: ["5432:5432"]
    volumes: [pgdata:/var/lib/postgresql/data]
    healthcheck: { test: ["CMD-SHELL", "pg_isready -U tj"], interval: 5s }
  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]
    healthcheck: { test: ["CMD", "redis-cli", "ping"], interval: 5s }
  api:
    build: ../apps/api
    env_file: ../infra/.env
    depends_on:
      postgres: { condition: service_healthy }
      redis:    { condition: service_healthy }
    ports: ["8080:8080"]
    volumes: [../apps/api:/app]   # hot reload via `air`
    command: ["air", "-c", ".air.toml"]
  web:
    build: ../apps/web
    env_file: ../infra/.env
    depends_on: [api]
    ports: ["3000:3000"]
    volumes: [apps/web:/app]    # hot reload via Nuxt dev server
    command: ["pnpm", "dev"]
volumes:
  pgdata: {}
```

**Hot reload stack:**
- BE → `air-verse/air` watches `apps/api/**.go`, rebuilds + restarts on change
- FE → Nuxt dev server with HMR (native)

`infra/.env.example` (committed; `.env` git-ignored):
```env
DATABASE_URL=postgres://tj:devpass@postgres:5432/tj?sslmode=disable
REDIS_URL=redis://redis:6379/0
API_PORT=8080
WEB_BASE_URL=http://localhost:3000
CORS_ALLOWED_ORIGINS=http://localhost:3000
LOG_LEVEL=info
JWT_ED25519_PRIVATE_KEY_PATH=/app/keys/ed25519.pem
OAUTH_GOOGLE_CLIENT_ID=
OAUTH_GOOGLE_CLIENT_SECRET=
OAUTH_GOOGLE_REDIRECT_URI=http://localhost:8080/v1/auth/oauth/google/callback
OAUTH_LINE_CLIENT_ID=
OAUTH_LINE_CLIENT_SECRET=
OAUTH_LINE_REDIRECT_URI=http://localhost:8080/v1/auth/oauth/line/callback
```

### 8.3 CI (GitHub Actions)

`.github/workflows/api-ci.yml` (on: pull_request) — golangci-lint + `sqlc generate` diff check + `go build` + `go test -race -coverprofile` (Postgres + Redis as service containers). `web-ci.yml` — pnpm lint + typecheck + build + test:unit (Vitest).

---

## 9. Testing + Definition of Done (Section 4 — ✅ approved 2026-10-03)

### 9.1 Testing strategy

| Layer | Tools | Scope |
|---|---|---|
| **BE unit** | `go test` + testify | `auth`, `tenant`, `observability`, `integration/oauth` ≥ 70% |
| **BE integration** | `testcontainers-go` (Postgres + Redis per test) | every `httpapi` route + tenant isolation + rate limit + OAuth flow (mocked provider) |
| **FE unit** | Vitest + `@vue/test-utils` | composables, stores, components — auth state machine focus |
| **E2E** | Playwright (TS-native) | 1 happy path: register → dashboard → logout → login → dashboard |

**Must-pass unit specs:**
- `auth/password_test.go` — hash/verify, wrong password rejected
- `auth/jwt_test.go` — sign/verify, expiry, wrong kid rejected
- `tenant/ctx_test.go` — With/From roundtrip
- `observability/errors_test.go` — every domain error → expected `(HTTP, code)`
- `integration/oauth/google_test.go` — `AuthorizeURL` format, mocked exchange + userinfo
- `integration/oauth/line_test.go` — same

**Must-pass integration specs:**
- `httpapi/auth_register_test.go`
- `httpapi/auth_login_test.go` (correct + wrong creds)
- `httpapi/auth_refresh_test.go` (rotate + reuse detection revokes all)
- `httpapi/me_test.go`
- `httpapi/tenant_isolation_test.go` (user A → 404 on tenant B data)
- `httpapi/rate_limit_test.go` (61st auth req → 429)
- `integration/oauth/google_callback_test.go` (mocked provider)

### 9.2 Definition of Done — Phase 0

**Functional:**
- [ ] 7 base tables + 2 OAuth tables via `make migrate-up`
- [ ] 4 plan seeds visible in DB
- [ ] `POST /v1/auth/register` → 201 + 4 audit rows + 4 entity rows in tx
- [ ] `POST /v1/auth/login` → 200; bad creds → 401 (no info leak)
- [ ] `POST /v1/auth/refresh` rotates; reuse → 401 + all sessions revoked
- [ ] `POST /v1/auth/logout` → 204
- [ ] `GET /v1/me` returns user + tenant + plan
- [ ] OAuth login with Google + Line end-to-end (real test accounts)
- [ ] Tenant isolation: user A cannot see tenant B data
- [ ] Rate limit: 61st `/v1/auth/*` req returns 429
- [ ] Audit logs for all Phase 0 actions
- [ ] `/healthz`, `/readyz`, `/livez` respond correctly
- [ ] PWA installable + offline fallback + install button

**Non-functional:**
- [ ] `docker-compose up` → http://localhost:3000 working
- [ ] GitHub Actions api-ci + web-ci green on PR
- [ ] Vuetify dark theme applied
- [ ] All endpoints return uniform `{error:{code,message,details}}` envelope
- [ ] No secrets in logs (CI grep check)

**Coverage:**
- [ ] BE service + auth + tenant + oauth ≥ 70%
- [ ] All domain errors have mapping test
- [ ] README.md quickstart (clone → `make up` → register)
- [ ] AGENTS.md describing repo + extension guide
- [ ] Spec doc committed

---

## 10. Implementation Order (Phase 0)

1. **Skeleton** — monorepo dirs · root README · AGENTS.md · .gitignore · .editorconfig · .env.example · docker-compose · Makefile
2. **Postgres + Redis** in compose + migration runner script + sqlc.yaml
3. **Migration 0001** — 7 tables + plan seeds
4. **BE config + server** — envconfig · slog · Echo · graceful shutdown · health endpoints
5. **BE auth foundation** — argon2id · JWT (EdDSA) · refresh sessions
6. **BE domain + store** — User/Tenant/Membership/Subscription + sqlc queries + repos
7. **BE auth service + handlers** — Register/Login/Refresh/Logout/Me + error mapping + audit
8. **BE middleware** — RequestID → Recovery → CORS → RateLimit → AccessLog → Auth → RBAC → Headers
9. **Migration 0002 + OAuth** — `user_identities` + `oauth_states` + provider interface + Google/Line impls + flow handlers
10. **BE tests** — unit + testcontainers integration
11. **FE skeleton** — Nuxt + Vuetify + Pinia + dark theme + layout
12. **FE auth** — composables (`useApi`/`useAuth`) + stores + pages (Landing/Login/Register/Dashboard) + middleware
13. **FE OAuth** — `OAuthButtonGroup` + `oauth-complete` route + `useOAuth`
14. **FE PWA** — `@vite-pwa/nuxt` config + SVG icons + offline page + install button + update toast
15. **CI** — api-ci.yml + web-ci.yml
16. **E2E** — Playwright auth happy path
17. **AGENTS.md + README** — finalize

---

## 11. Open Questions Log

Resolved (2026-10-03):
- Repo layout → monorepo ✅
- Backend framework → Echo (lean) ✅
- DB access stack → sqlc + golang-migrate ✅
- PWA module → @vite-pwa/nuxt ✅
- PWA register mode → autoUpdate ✅
- API caching → NetworkFirst + 5min TTL ✅
- Icon generation → SVG + sharp ✅
- Section 2: 7 tables ✅
- Section 2: UUIDv7 for public IDs, BIGSERIAL for audit_logs ✅
- Section 2: plan entitlements in metadata jsonb ✅
- Section 2: refresh_sessions added beyond MASTER §9 ✅
- Section 3: argon2id params ✅
- Section 3: refresh token reuse → auto-revoke all ✅
- Section 3: flat role-only RBAC ✅
- Section 3: EdDSA keypair on disk ✅
- Section 3: CSP `default-src 'self' + Cloudinary img-src` ✅
- Section 3.5: Providers = Google + Line ✅
- Section 3.5: account linking by verified email ✅
- Section 3.5: PKCE `code_verifier` server-side ✅
- Section 3.5: email/password + OAuth coexist ✅
- Section 3.5: unverified-email users allowed on routes (verify Phase 4) ✅
- Section 4: hot reload via air + Nuxt HMR ✅
- Section 4: GitHub Actions + testcontainers-go ✅
- Section 4: 70% coverage threshold ✅
- Section 4: Playwright E2E ✅
- Section 4: 17-step implementation order ✅

**Status: All Section 0–4 questions resolved. Spec ready for review.**

---

## 12. Spec Self-Review (2026-10-03)

**1. Placeholder scan:** ✅ no TBD/TODO remaining.
**2. Internal consistency:**
  - Section 7.6 rate limits reference Redis — consistent with §4 docker-compose (Redis present) ✅
  - Section 7.5 error envelope uniform across all routes — consistent with §3.4 error mapping ✅
  - Section 7A.6 env vars match §8.2 docker-compose env_file ✅
  - Section 7A.4 flow references `/v1/auth/oauth/{provider}/callback` — matches §7.5 routing convention ✅
  - Section 9.2 DoD items reference §7 flows and §8 endpoints — no contradictions ✅
  - Section 10 step 9 (OAuth) references §7A — consistent ✅
**3. Scope check:** Phase 0 is a single, bounded vertical slice (auth + tenant + RBAC + observability + PWA + OAuth + dev infra + CI). Implementation is ~30–50 files, achievable in one implementation plan ✅
**4. Ambiguity check:**
  - "tenant" vs "workspace" terminology — spec uses "tenant" consistently (no drift)
  - "owner" role semantics — every Phase 0 user is the sole owner of their tenant (documented in §7.4)
  - OAuth user unverified-email policy — explicitly documented (§7A.5)
  - Refresh reuse detection — explicitly revokes all sessions (§7.1 Refresh + §7.7 audit)
  - **Minor:** the spec doc itself is the Phase 0 design — Phase 1–4 designs will get their own spec docs (`docs/superpowers/specs/2026-MM-DD-trading-journal-phase{N}-design.md`)
  - **Minor:** JWT key rotation cadence "yearly" (§7.2) is forward-looking; Phase 0 ships single key + `kid="v1"`. Rotation ops task is Phase 4.

---

## 13. Spec Coverage Map (Appendix A — Phase 0 ↔ MASTER PROMPT §3-43)

| Master Prompt section | Phase 0 coverage |
|---|---|
| §7 Tech stack (FE Nuxt/Vuetify/Pinia/TS) | ✅ repo skeleton, dark theme, layout, auth pages |
| §8 Backend Go layout (cmd/internal) | ✅ exact folder structure |
| §9 PostgreSQL core tables (subset) | ✅ users, tenants, memberships, plans, subscriptions, audit_logs |
| §15 Cloudinary folder convention | ⏭️ Phase 1 (no Cloudinary in Phase 0) |
| §36 Security (Auth, RBAC, tenant isolation, rate limit) | ✅ auth + tenant + RBAC + rate limit foundations |
| §37 Observability (structured logs, health/readiness/liveness) | ✅ slog JSON + /healthz, /readyz, /livez |
| §40 Vertical-slice development rule | ✅ Phase 0 = first vertical slice |
| §41 MVP DoD (subset reachable in Phase 0) | items 1-3: ✅ register, login, choose Free |

Items deferred to Phase 1+: trade parsing, analytics, journal, screenshots, MetaApi sync, billing automation, Pro features.