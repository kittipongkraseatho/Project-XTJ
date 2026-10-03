// Package ratelimit holds the per-key (API-key-id) token bucket used by
// the public API rate limit middleware.
//
// Phase 5 shipped an in-process implementation (`MemoryKeyBucket`).
// Phase 5.1 adds `RedisKeyBucket` for production multi-instance correctness.
//
// Both implementations satisfy `KeyBucket` — choose at wire time via the
// `RATE_LIMIT_BACKEND` env var (default `memory` for dev; `redis` for prod).
package ratelimit

import (
	"time"

	"github.com/gofrs/uuid"
)

// Decision is what Allow returns.
//
//   - Allowed:   true if the request may proceed
//   - Remaining: tokens left in the bucket after this call (best-effort;
//                the middleware only inspects Allowed)
//   - Reset:     how long until the bucket fully refills (best-effort)
type Decision struct {
	Allowed   bool
	Remaining int
	Reset     time.Duration
}

// KeyBucket is the per-API-key rate limiter contract.
type KeyBucket interface {
	// Allow reports whether the request from the given key id may proceed.
	Allow(id uuid.UUID) Decision
	// Close releases resources (Redis connection, etc.). For in-process
	// implementations this is a no-op.
	Close() error
}