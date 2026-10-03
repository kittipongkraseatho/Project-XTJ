package config

import (
	"testing"
)

func TestConfig_PublicAPIRateDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("REDIS_URL", "redis://x")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if c.APIPublicRatePerMinute != 60 {
		t.Errorf("want 60, got %d", c.APIPublicRatePerMinute)
	}
	if c.APIPublicRateBurst != 10 {
		t.Errorf("want 10, got %d", c.APIPublicRateBurst)
	}
}

func TestConfig_PublicAPIRateOverrides(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("REDIS_URL", "redis://x")
	t.Setenv("API_PUBLIC_RATE_PER_MINUTE", "120")
	t.Setenv("API_PUBLIC_RATE_BURST", "20")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if c.APIPublicRatePerMinute != 120 {
		t.Errorf("want 120, got %d", c.APIPublicRatePerMinute)
	}
	if c.APIPublicRateBurst != 20 {
		t.Errorf("want 20, got %d", c.APIPublicRateBurst)
	}
}

func TestConfig_RateLimitBackendDefaults(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("REDIS_URL", "redis://x")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if c.RateLimitBackend != "memory" {
		t.Errorf("want memory default, got %q", c.RateLimitBackend)
	}
	if c.RateLimitRedisEnabled() {
		t.Errorf("memory backend must NOT enable redis")
	}
	if c.RateLimitKeyPrefix != "trj:rl:" {
		t.Errorf("want default prefix 'trj:rl:', got %q", c.RateLimitKeyPrefix)
	}
	if c.RateLimitTTLSeconds != 3600 {
		t.Errorf("want 3600 default TTL, got %d", c.RateLimitTTLSeconds)
	}
}

func TestConfig_RateLimitBackendRedisOverride(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://x")
	t.Setenv("REDIS_URL", "redis://x")
	t.Setenv("RATE_LIMIT_BACKEND", "redis")
	t.Setenv("RATE_LIMIT_KEY_PREFIX", "tenant:rl:")
	t.Setenv("RATE_LIMIT_TTL_SECONDS", "1800")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	if !c.RateLimitRedisEnabled() {
		t.Errorf("redis override must enable redis")
	}
	if c.RateLimitKeyPrefix != "tenant:rl:" {
		t.Errorf("want 'tenant:rl:', got %q", c.RateLimitKeyPrefix)
	}
	if c.RateLimitTTLSeconds != 1800 {
		t.Errorf("want 1800, got %d", c.RateLimitTTLSeconds)
	}
}