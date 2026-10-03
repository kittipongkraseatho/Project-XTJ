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