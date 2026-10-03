# Trading Journal — Phase 5 Design

| | |
|---|---|
| **Status** | ✅ Approved 2026-10-03 (1 fork resolved) — implementation starting |
| **Date** | 2026-10-03 |
| **Phase** | 5 — Public API + Scoped API Keys |
| **Scope** | API-key auth scheme · Scoped permissions · Per-key rate limiting · `/v1/public/*` read-only endpoints · Tenant-bound keys |
| **Out of scope** | AI analysis (LLM trade commentary) · Real MetaApi SDK handshake (Phase 5+) |

---

## 1. Decisions Resolved

| Fork | Decision |
|---|---|
| Phase 5 scope | Public API + scoped API keys. AI analysis deferred. |

---

## 2. New Table (Migration 0006)

### 2.1 `api_keys`

| Column | Type | Notes |
|---|---|---|
| `id` | UUID PK | UUIDv7 |
| `tenant_id` | UUID NOT NULL FK→tenants CASCADE | tenant scope |
| `name` | TEXT NOT NULL | human label ("Zapier", "Mobile app") |
| `prefix` | TEXT NOT NULL | first 8 chars of the key (e.g. `trj_pk_abcd1234`) — used for identification, indexed |
| `secret_hash` | TEXT NOT NULL | bcrypt hash of the secret portion; never stored plaintext |
| `scopes` | TEXT[] NOT NULL DEFAULT `{}` | e.g. `{read:trades, read:analytics}` |
| `last_used_at` | TIMESTAMPTZ | for FE display |
| `expires_at` | TIMESTAMPTZ | NULL = no expiry |
| `is_active` | BOOLEAN NOT NULL DEFAULT TRUE | soft delete via toggle |
| `created_by_user_id` | UUID NOT NULL FK→users | audit |
| `created_at` | TIMESTAMPTZ NOT NULL DEFAULT now() | |
| `revoked_at` | TIMESTAMPTZ | set when admin revokes |

Indexes:
- UNIQUE(`prefix`) — public-facing identifier
- INDEX(`tenant_id`, `is_active`) — FE list query

---

## 3. Key Format

Keys are issued as: `trj_pk_<prefix>.<secret>` — e.g. `trj_pk_abcd1234.XyZ123...`

- `trj_pk_` literal prefix (trading-journal public key)
- `<prefix>` = 8 random URL-safe characters (used for lookup)
- `<secret>` = 32 random URL-safe characters (the part the user must keep secret)

**Generation:** `crypto/rand` for both parts. The `secret_hash` column stores `bcrypt(<secret>, cost=10)`. Verification: look up by `prefix`, then `bcrypt.CompareHashAndPassword(hash, secret)`.

**One-shot display:** The plaintext `trj_pk_…` is shown to the user exactly once at creation, never again.

---

## 4. Auth Middleware

Two routes:
- `/v1/auth/*` (existing) — JWT cookie / refresh token
- `/v1/public/*` (new) — API key only

Middleware `apiKey.Auth`:
1. Read `Authorization: Bearer trj_pk_<prefix>.<secret>` header
2. Split on `.` → look up by `prefix`
3. Verify `secret_hash` matches `secret` via bcrypt
4. Reject if `is_active=false`, `revoked_at != null`, `expires_at < now`
5. Set tenant ctx (same as JWT path)
6. Set scope list on ctx (read:trades etc.)
7. Per-key rate limit (token bucket, 60 req/min default; configurable per key)
8. Increment `last_used_at` async (fire-and-forget)

**Failure modes:**
- Missing header → 401 `API_KEY_REQUIRED`
- Bad prefix/secret → 401 `API_KEY_INVALID`
- Expired / revoked → 401 `API_KEY_EXPIRED`
- Rate limited → 429 `RATE_LIMITED` (per-key + per-IP fallback)

---

## 5. Scope Set (Phase 5)

| Scope | Allowed on |
|---|---|
| `read:accounts` | `GET /v1/public/accounts` |
| `read:trades` | `GET /v1/public/trades` |
| `read:analytics` | `GET /v1/public/analytics/*` (overview, daily-pl, equity-curve, symbol-breakdown, session, ea, day-of-week, hour-of-day) |
| `read:payments` | `GET /v1/public/billing/payments` |
| `write:journal` | (deferred to Phase 5.1) |
| `write:webhooks` | (deferred to Phase 5.2) |

**Read-only phase 5.** Write operations come later when we have a clear use case (e.g. webhook subscriptions, programmatic journal updates).

### 6. Endpoints under `/v1/public/*`

| Method | Path | Scope |
|---|---|---|
| `GET` | `/v1/public/accounts` | `read:accounts` |
| `GET` | `/v1/public/accounts/:id` | `read:accounts` |
| `GET` | `/v1/public/trades` | `read:trades` |
| `GET` | `/v1/public/trades/:id` | `read:trades` |
| `GET` | `/v1/public/analytics/overview` | `read:analytics` |
| `GET` | `/v1/public/analytics/daily-pl` | `read:analytics` |
| `GET` | `/v1/public/analytics/equity-curve` | `read:analytics` |
| `GET` | `/v1/public/analytics/symbol-breakdown` | `read:analytics` |
| `GET` | `/v1/public/analytics/session` | `read:analytics` |
| `GET` | `/v1/public/analytics/ea` | `read:analytics` |
| `GET` | `/v1/public/analytics/day-of-week` | `read:analytics` |
| `GET` | `/v1/public/analytics/hour-of-day` | `read:analytics` |
| `GET` | `/v1/public/billing/payments` | `read:payments` |

**Query params** (consistent across the surface):
- `account_id` (optional, scoped to tenant)
- `from` / `to` (date range)
- `limit` / `offset` (pagination)

### 7. Admin endpoints (JWT auth, not API key)

```
POST   /api/admin/keys              — generate new key
GET    /api/admin/keys              — list tenant's keys
DELETE /api/admin/keys/:id         — revoke (soft-delete)
PATCH  /api/admin/keys/:id         — update name / scopes / expiry
```

Same shape as the Phase 4 schedule admin routes. Future: a separate `/v1/api-keys` group is fine.

---

## 6. Per-key Rate Limiting

`internal/ratelimit/key_bucket.go` — in-memory token bucket keyed by API key ID. Default `60 req/min` per key (configurable per key in the future). Phase 5 single-instance; Phase 5.1 multi-instance uses Redis (already in deps).

Implementation:
- Lazy refill (compute available tokens from elapsed time)
- `golang.org/x/time/rate.Limiter` per key
- `sync.Map[uuid.UUID]*rate.Limiter`
- Per-IP bucket → 10 req/min for unknown keys (defense against brute force)

---

## 7. Frontend

### 7.1 `/api-keys` page (new)

- Table: name · prefix (`trj_pk_abcd1234.****`) · scopes (chips) · last used · expires · active
- "Create API Key" button → opens dialog
  - name input
  - scopes multi-checkbox (read:accounts, read:trades, read:analytics, read:payments)
  - expiry radio (30d / 90d / 1y / never)
  - submit → POST returns full key, shown ONCE in a copy-to-clipboard box
- Per-row "Revoke" button → DELETE

### 7.2 Sidebar / nav

Add "API Keys" link under "Settings" or as a top-level item (Phase 5 — top-level, it's the kind of thing power users want to find quickly).

---

## 8. Definition of Done — Phase 5

- [ ] Migration 0006 applied; `api_keys` table exists
- [ ] `apikey.Service` interface + impl: generate (returns plaintext ONCE), list, revoke, update, verify-from-secret
- [ ] All 13 public endpoints work behind `apiKey.Auth` middleware
- [ ] Each endpoint checks its scope (`read:trades` etc.); 403 if missing
- [ ] Per-key rate limit (60 req/min) + per-IP fallback (10 req/min)
- [ ] FE `/api-keys` page: list, create (with one-shot plaintext display), revoke, scope editor
- [ ] Unit tests: token generation (uniqueness + prefix shape), bcrypt verify, scope guard, rate limit math
- [ ] `go build ./...` + `go test -race -short ./...` + `pnpm build` all green
- [ ] AGENTS.md updated: Phase 5 done, "Adding a new API scope" + "Adding a new public endpoint" guides

---

## 9. Implementation Order (Phase 5)

1. **Design doc** (this file)
2. **Migration 0006** + domain + store
3. **`apikey.Service`** — generate/list/revoke/verify + tests
4. **Auth middleware** `apiKey.Auth` + scope guard helper + rate limit
5. **Public endpoints** — thin wrappers around existing handlers, scope checks
6. **FE `/api-keys` page**
7. **Tests** + AGENTS.md + .env.example updates

---

## 10. Out of Scope (Phase 5)

- Webhook delivery (Phase 5.2 — separate keys for inbound webhooks)
- Programmatic journal updates (`write:journal` scope)
- Stripe webhook → subscription state machine (Phase 5+)
- Real MetaApi SDK handshake (Phase 5+)
- AI analysis / LLM features (Phase 5+)
- Multi-instance rate limiting via Redis (Phase 5.1)