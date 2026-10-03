package ratelimit

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/gofrs/uuid"
	"github.com/redis/go-redis/v9"
)

// Lua script — atomic token-bucket refill + check.
//
// KEYS[1] = bucket key (e.g. "trj:rl:<api-key-uuid>")
// ARGV[1] = capacity (burst)
// ARGV[2] = refill rate per second
// ARGV[3] = now (ms since epoch)
// ARGV[4] = cost (1 for now)
// ARGV[5] = idle TTL seconds
//
// Returns: { allowed (0|1), remaining_tokens (int), capacity (int) }
const luaTokenBucket = `
local capacity = tonumber(ARGV[1])
local rate     = tonumber(ARGV[2])
local now      = tonumber(ARGV[3])
local cost     = tonumber(ARGV[4])
local ttl      = tonumber(ARGV[5])

local data = redis.call('HMGET', KEYS[1], 'ts', 'tokens')
local ts    = tonumber(data[1]) or now
local tokens = tonumber(data[2]) or capacity

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
`

// RedisKeyBucket is a production-grade per-key token-bucket backed by Redis.
//
// Multi-instance correctness: every replica reads/writes the same bucket
// state via a single atomic Lua script, so the effective limit is exactly
// `perMinute` regardless of replica count.
//
// Failure mode: on Redis error the bucket fails OPEN — better to risk a
// brief abuse window than to take the whole public API down. The error
// is logged at WARN level.
type RedisKeyBucket struct {
	rdb       *redis.Client
	keyPrefix string
	capacity  int
	refillSec float64 // tokens per second
	ttlSec    int
	script    *redis.Script
	logger    *slog.Logger
}

// NewRedisKeyBucket creates a Redis-backed bucket.
//
//   - perMinute: tokens per minute (each request costs 1 token)
//   - burst:     capacity (= max instantaneous burst)
//   - ttlSec:    idle GC (the Lua EXPIRE on every call)
//   - keyPrefix: per-tenant isolation in multi-tenant Redis
//   - logger:    fail-open error sink (use slog.Default() if nil)
func NewRedisKeyBucket(rdb *redis.Client, keyPrefix string, perMinute, burst, ttlSec int, logger *slog.Logger) *RedisKeyBucket {
	if perMinute <= 0 {
		perMinute = 60
	}
	if burst <= 0 {
		burst = 10
	}
	if ttlSec <= 0 {
		ttlSec = 3600
	}
	if keyPrefix == "" {
		keyPrefix = "trj:rl:"
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &RedisKeyBucket{
		rdb:       rdb,
		keyPrefix: keyPrefix,
		capacity:  burst,
		refillSec: float64(perMinute) / 60.0,
		ttlSec:    ttlSec,
		script:    redis.NewScript(luaTokenBucket),
		logger:    logger,
	}
}

// Allow runs the Lua script and returns a Decision. On Redis error it
// returns Allowed=true (fail open) and logs at WARN.
func (b *RedisKeyBucket) Allow(id uuid.UUID) Decision {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	key := b.keyPrefix + id.String()
	now := time.Now().UnixMilli()
	res, err := b.script.Run(ctx, b.rdb, []string{key},
		b.capacity,
		b.refillSec,
		now,
		1, // cost
		b.ttlSec,
	).Result()

	if err != nil {
		// Fail open — surface via log, don't 5xx the public API.
		if !errors.Is(err, context.DeadlineExceeded) {
			b.logger.Warn("rate limit redis error — failing open",
				slog.String("err", err.Error()),
				slog.String("key", key))
		}
		return Decision{Allowed: true, Remaining: b.capacity}
	}

	arr, ok := res.([]any)
	if !ok || len(arr) < 3 {
		b.logger.Warn("rate limit redis unexpected reply — failing open",
			slog.Any("reply", res))
		return Decision{Allowed: true, Remaining: b.capacity}
	}
	allowedI, _ := arr[0].(int64)
	remainingI, _ := arr[1].(int64)
	// capacityI, _ := arr[2].(int64) // not needed in Decision

	return Decision{
		Allowed:   allowedI == 1,
		Remaining: int(remainingI),
		Reset:     time.Second, // best-effort
	}
}

// Close is a no-op for now; the redis.Client is owned by the caller
// (the Echo server) and closed on app shutdown.
func (b *RedisKeyBucket) Close() error { return nil }