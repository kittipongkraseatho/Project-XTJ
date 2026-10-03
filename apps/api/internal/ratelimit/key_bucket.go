// Package ratelimit holds the per-key (API-key-id) token bucket used by
// the public API rate limit middleware.
//
// Phase 5 single-instance: in-memory sync.Map keyed by uuid.UUID.
// Phase 5.1 (multi-instance) will swap for a Redis token-bucket.
package ratelimit

import (
	"sync"
	"time"

	"github.com/gofrs/uuid"
	"golang.org/x/time/rate"
)

// KeyBucket is a per-API-key token-bucket.
type KeyBucket struct {
	mu        sync.Mutex
	limiters  map[string]*rate.Limiter
	perMinute int
	burst     int
}

// NewKeyBucket creates a per-minute token bucket with the given rate
// (default 60 req/min, burst 10).
func NewKeyBucket(perMinute, burst int) *KeyBucket {
	if perMinute <= 0 {
		perMinute = 60
	}
	if burst <= 0 {
		burst = 10
	}
	return &KeyBucket{
		limiters:  make(map[string]*rate.Limiter),
		perMinute: perMinute,
		burst:     burst,
	}
}

// Allow reports whether the request from the given key id should be allowed
// to proceed. Limiters are lazily created and stored.
func (b *KeyBucket) Allow(id uuid.UUID) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := id.String()
	lim, ok := b.limiters[key]
	if !ok {
		// rate.Every(minute / perMinute) — convert perMinute → interval
		lim = rate.NewLimiter(rate.Every(time.Minute/time.Duration(b.perMinute)), b.burst)
		b.limiters[key] = lim
	}
	return lim.Allow()
}

// CleanupIdle removes limiters that haven't been used in `idle`. Cheap
// GC for inactive keys (no scheduled goroutine in Phase 5 — call
// periodically from a future worker if needed).
func (b *KeyBucket) CleanupIdle(idle time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// We can't cheaply query a limiter's last-use; Phase 5 keeps them all.
	// Real cleanup is a Phase 5.1 optimization.
	_ = idle
}