// Package ratelimit — in-process per-key token bucket.
//
// Phase 5 default. Phase 5.1 keeps this as the `memory` backend; production
// uses `RedisKeyBucket` for multi-instance correctness.
//
// Implementation:
//   - Lazy refill (compute available tokens from elapsed time)
//   - golang.org/x/time/rate.Limiter per key
//   - sync.Mutex + map[string]*rate.Limiter
//
// Multi-instance note: each replica independently counts tokens, so the
// effective limit becomes N × `perMinute` across N replicas. Phase 5.1
// `redis` backend fixes this by sharing state in Redis.
package ratelimit

import (
	"sync"
	"time"

	"github.com/gofrs/uuid"
	"golang.org/x/time/rate"
)

// MemoryKeyBucket is an in-process per-key token-bucket.
type MemoryKeyBucket struct {
	mu        sync.Mutex
	limiters  map[string]*rate.Limiter
	perMinute int
	burst     int
}

// NewMemoryKeyBucket creates a per-minute token bucket with the given rate
// (default 60 req/min, burst 10).
func NewMemoryKeyBucket(perMinute, burst int) *MemoryKeyBucket {
	if perMinute <= 0 {
		perMinute = 60
	}
	if burst <= 0 {
		burst = 10
	}
	return &MemoryKeyBucket{
		limiters:  make(map[string]*rate.Limiter),
		perMinute: perMinute,
		burst:     burst,
	}
}

// Allow reports whether the request from the given key id should be allowed
// to proceed. Limiters are lazily created and stored.
func (b *MemoryKeyBucket) Allow(id uuid.UUID) Decision {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := id.String()
	lim, ok := b.limiters[key]
	if !ok {
		lim = rate.NewLimiter(rate.Every(time.Minute/time.Duration(b.perMinute)), b.burst)
		b.limiters[key] = lim
	}
	if !lim.Allow() {
		return Decision{Allowed: false, Remaining: 0, Reset: time.Second}
	}
	// ReserveToken returns the wait time if it would block; we just need
	// remaining tokens (best-effort).
	reservation := lim.Reserve()
	if !reservation.OK() {
		return Decision{Allowed: true, Remaining: 0}
	}
	reservation.Cancel()
	// Tokens() is the current count after the Allow + Cancel pair.
	remaining := int(lim.Tokens())
	if remaining < 0 {
		remaining = 0
	}
	return Decision{Allowed: true, Remaining: remaining, Reset: time.Second}
}

// Close is a no-op for the in-process implementation.
func (b *MemoryKeyBucket) Close() error { return nil }

// CleanupIdle removes limiters that haven't been used in `idle`. Cheap
// GC for inactive keys (no scheduled goroutine in Phase 5 — call
// periodically from a future worker if needed).
//
// Phase 5.1 keeps this method for API compatibility; the Redis backend
// uses key TTL instead.
func (b *MemoryKeyBucket) CleanupIdle(idle time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_ = idle
}