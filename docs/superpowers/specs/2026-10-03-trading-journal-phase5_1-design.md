# Trading Journal — Phase 5.1 Design

| | |
|---|---|
| **Status** | ✅ Approved 2026-10-03 — implementation starting |
| **Date** | 2026-10-03 |
| **Phase** | 5.1 — Redis Rate Limit + Integration Test |
| **Scope** | Production-grade rate limiter (Redis-backed) · Multi-instance correctness · End-to-end integration test (testcontainers-go) |
| **Out of scope** | AI analysis · Real MetaApi SDK · Stripe webhook state machine · write:journal / write:webhooks scopes |

---

## 1. Decisions Resolved

| Fork | Decision |
|---|---|
| Rate limit backend in prod | **Redis** (via Lua script for atomicity) — env-gated, `memory` still available for local dev |
| Failure mode on Redis down | **Fail open** with `slog.Warn` (industry standard; prefer availability over strict rate limit) |
| Algorithm | **Token bucket** (same as Phase 5 in-process — keeps wire behavior consistent) |
| Integration test framework | **testcontainers-go** (already in deps; CI `api-ci.yml` has Postgres+Redis service containers) |

---

## 2. Rate Limiter

### 2.1 `KeyBucket` interface

```go
// internal/ratelimit/limiter.go
package ratelimit

import "github.com/gofrs/uuid"

type Decision struct {
    Allowed   bool
    Remaining int
    Reset     time.Duration  // until bucket fully refills
}

type KeyBucket interface {
    Allow(id uuid.UUID) Decision
    Close() error
}
```

Both `MemoryKeyBucket` (renamed from `KeyBucket` in Phase 5) and `RedisKeyBucket` implement this.

### 2.2 MemoryKeyBucket (rename only)

`internal/ratelimit/key_bucket.go` keeps the Phase 5 logic; the struct is renamed `MemoryKeyBucket` and satisfies the new interface.

### 2.3 RedisKeyBucket

`internal/ratelimit/redis_bucket.go` — uses `github.com/redis/go-redis/v9` (already a dep).

**Key layout:**
```
trj:rl:<api-key-uuid>  →  { ts: ms-since-epoch, tokens: float64 }
```

**Lua script** (atomic refill + check):

```lua
-- KEYS[1] = bucket key
-- ARGV[1] = capacity (burst)
-- ARGV[2] = refill rate per second
-- ARGV[3] = now (ms since epoch)
-- ARGV[4] = cost (always 1 for now)
-- ARGV[5] = TTL seconds (idle GC)

local capacity   = tonumber(ARGV[1])
local rate       = tonumber(ARGV[2])
local now        = tonumber(ARGV[3])
local cost       = tonumber(ARGV[4])
local ttl        = tonumber(ARGV[5])

local data = redis.call('HMGET', KEYS[1], 'ts', 'tokens')
local ts    = tonumber(data[1]) or now
local tokens = tonumber(data[2]) or capacity

-- Refill since last call
local delta_ms  = math.max(0, now - ts)
local refilled  = math.min(capacity, tokens + (delta_ms / 1000) * rate)

local allowed = 0
if refilled >= cost then
    refilled = refilled - cost
    allowed = 1
end

redis.call('HMSET', KEYS[1], 'ts', now, 'tokens', refilled)
redis.call('EXPIRE', KEYS[1], ttl)

return { allowed, math.floor(refilled), capacity }
```

Returns `{ allowed, remaining, capacity }` as integers (Lua `floor`).

**Fail-open behavior:** if `redis.Eval` returns a network error, log `slog.Warn` and return `Decision{Allowed: true}` — better to risk a brief abuse window than to take the whole public API down.

**TTL:** 1 hour after last access (the Lua sets `EXPIRE` each call). The constant is in the script via `ARGV[5]` so we can tune without changing Go code.

### 2.4 Key prefix

`RATE_LIMIT_KEY_PREFIX` env, default `trj:rl:`. Lets us namespace in a shared Redis.

---

## 3. Config

```go
// apps/api/internal/config/config.go
RateLimitBackend    string `envconfig:"RATE_LIMIT_BACKEND" default:"memory"` // memory | redis
RateLimitKeyPrefix  string `envconfig:"RATE_LIMIT_KEY_PREFIX" default:"trj:rl:"`
RateLimitTTLSeconds int    `envconfig:"RATE_LIMIT_TTL_SECONDS" default:"3600"`
```

Router wires the bucket type:
```go
var bucket ratelimit.KeyBucket
switch cfg.RateLimitBackend {
case "redis":
    bucket = ratelimit.NewRedisKeyBucket(rdb, cfg.RateLimitKeyPrefix,
        cfg.APIPublicRatePerMinute, cfg.APIPublicRateBurst, cfg.RateLimitTTLSeconds, logger)
default:
    bucket = ratelimit.NewMemoryKeyBucket(cfg.APIPublicRatePerMinute, cfg.APIPublicRateBurst)
}
```

---

## 4. Integration Test

### 4.1 `internal/integration/apikey/flow_test.go`

Uses `testcontainers-go`:
- `postgres:16-alpine` — runs migrations (0001–0006)
- `redis:7-alpine`

Spins up a real Echo server via `echo.New()` + `server.RegisterRoutes(...)` against the test DB, then drives:

1. `POST /v1/auth/register` → JWT
2. `GET /v1/me` → confirms user + tenant
3. `POST /v1/api-keys` → mints a key, returns plaintext
4. `GET /v1/public/accounts` (no key) → 401
5. `GET /v1/public/accounts` (with `Authorization: Bearer <plaintext>`) → 200, list empty
6. `POST /v1/api-keys` again with a key having only `read:accounts`; call `read:trades` → 403
7. Burn the burst by calling 11 times in a tight loop; the 11th call → 429 + `Retry-After`
8. `DELETE /v1/api-keys/:id` → 204
9. Re-use the plaintext key → 401 (revoked)
10. `GET /v1/api-keys` → confirm revoked status

**Isolation:** each test gets a fresh DB schema (random suffix) and Redis index (random prefix) — no cross-test bleed.

**Skip rules:** if `TESTCONTAINERS=off` env, skip with `t.Skip`. Lets CI control; local dev with no Docker can still run other tests.

---

## 5. Definition of Done — Phase 5.1

- [ ] `KeyBucket` interface extracted; `MemoryKeyBucket` renamed
- [ ] `RedisKeyBucket` impl with atomic Lua script; `Decision` returned (allowed/remaining/reset)
- [ ] Fail-open on Redis error with `slog.Warn`
- [ ] `RATE_LIMIT_BACKEND` env; default `memory`; `redis` activates Redis impl
- [ ] Integration test (testcontainers-go) exercises 10-step flow above; all assertions pass
- [ ] `go build ./...` + `go test -race -short ./...` + integration test all green
- [ ] AGENTS.md: Phase 5.1 complete; "Rate limit" section updated; "Adding a new rate limit backend" guide
- [ ] .env.example: `RATE_LIMIT_BACKEND=memory`, `RATE_LIMIT_KEY_PREFIX=trj:rl:`, `RATE_LIMIT_TTL_SECONDS=3600`

---

## 6. Implementation Order

1. **Design doc** (this file)
2. **`KeyBucket` interface + rename** `KeyBucket` → `MemoryKeyBucket`
3. **`RedisKeyBucket`** + Lua script + miniredis unit test
4. **Config + router** env-gated swap
5. **Integration test** (testcontainers-go)
6. **AGENTS.md + .env.example** + final test run

---

## 7. Out of Scope (Phase 5.1)

- AI analysis
- Real MetaApi SDK
- Stripe webhook state machine
- write:journal / write:webhooks scopes
- Webhook delivery (still Phase 5.2)
- Per-key (per-user) rate limit override (deferred; current implementation uses global 60/min)
