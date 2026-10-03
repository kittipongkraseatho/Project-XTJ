package ratelimit_test

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gofrs/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/trading-journal/api/internal/ratelimit"
)

func newTestBucket(t *testing.T, perMinute, burst int) (*ratelimit.RedisKeyBucket, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	// Discard slog output during tests.
	bucket := ratelimit.NewRedisKeyBucket(rdb, "test:rl:", perMinute, burst, 60,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() {
		_ = bucket.Close()
		_ = rdb.Close()
		mr.Close()
	})
	return bucket, mr, rdb
}

func TestRedisKeyBucket_FirstCallAllowed(t *testing.T) {
	bucket, _, _ := newTestBucket(t, 60, 10)
	id := uuid.Must(uuid.NewV7())
	d := bucket.Allow(id)
	if !d.Allowed {
		t.Fatalf("first call must be allowed")
	}
	if d.Remaining < 0 {
		t.Errorf("remaining must be >= 0, got %d", d.Remaining)
	}
}

func TestRedisKeyBucket_BurstExhaustion(t *testing.T) {
	// capacity = 3, very low rate so the bucket doesn't refill during test.
	bucket, _, _ := newTestBucket(t, 1, 3)
	id := uuid.Must(uuid.NewV7())

	for i := 1; i <= 3; i++ {
		d := bucket.Allow(id)
		if !d.Allowed {
			t.Fatalf("call %d: want allowed, got denied (remaining=%d)", i, d.Remaining)
		}
	}
	d := bucket.Allow(id)
	if d.Allowed {
		t.Fatalf("4th call: want denied (burst exhausted)")
	}
}

func TestRedisKeyBucket_PerKeyIsolation(t *testing.T) {
	bucket, _, _ := newTestBucket(t, 1, 2)
	a := uuid.Must(uuid.NewV7())
	b := uuid.Must(uuid.NewV7())
	// Burn out key A.
	for i := 0; i < 2; i++ {
		_ = bucket.Allow(a)
	}
	if d := bucket.Allow(a); d.Allowed {
		t.Fatalf("key A should be exhausted")
	}
	// Key B is independent.
	if d := bucket.Allow(b); !d.Allowed {
		t.Fatalf("key B should be allowed (independent bucket)")
	}
}

func TestRedisKeyBucket_Refill(t *testing.T) {
	bucket, _, _ := newTestBucket(t, 60, 2)
	id := uuid.Must(uuid.NewV7())
	_ = bucket.Allow(id)
	_ = bucket.Allow(id)
	if d := bucket.Allow(id); d.Allowed {
		t.Fatalf("3rd immediate call: want denied")
	}
	// Wait real time so the Lua refill math (driven by the `now` arg we
	// pass in) advances. 60/min = 1 token/sec → 2s sleep refills 2.
	time.Sleep(2100 * time.Millisecond)
	if d := bucket.Allow(id); !d.Allowed {
		t.Fatalf("after 2s refill: want allowed (got remaining=%d)", d.Remaining)
	}
}

func TestRedisKeyBucket_FailOpenOnRedisDown(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	bucket := ratelimit.NewRedisKeyBucket(rdb, "test:rl:", 60, 5, 60,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { _ = bucket.Close() })

	id := uuid.Must(uuid.NewV7())
	if d := bucket.Allow(id); !d.Allowed {
		t.Fatalf("baseline call: want Allowed")
	}
	// Kill the server.
	mr.Close()
	d := bucket.Allow(id)
	if !d.Allowed {
		t.Fatalf("on Redis down: want Allowed (fail open), got denied")
	}
}

func TestRedisKeyBucket_KeyPrefix(t *testing.T) {
	bucket, mr, rdb := newTestBucket(t, 60, 10)
	id := uuid.Must(uuid.NewV7())
	_ = bucket.Allow(id)
	// The key should be `test:rl:<uuid>`.
	want := "test:rl:" + id.String()
	if !mr.Exists(want) {
		t.Fatalf("key %q not found in miniredis", want)
	}
	_ = rdb
}