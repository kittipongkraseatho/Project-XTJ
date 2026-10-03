package phase5http

import (
	"testing"
	"time"

	"github.com/trading-journal/api/internal/domain/api_key"
)

func TestParseExpiry_Supported(t *testing.T) {
	cases := []struct {
		in      string
		wantOk  bool
		minDays int
	}{
		{"30d", true, 30 - 1},
		{"90d", true, 90 - 1},
		{"1y", true, 365 - 2},
	}
	now := time.Now().UTC()
	for _, c := range cases {
		d, err := parseExpiry(c.in)
		if c.wantOk && err != nil {
			t.Errorf("parseExpiry(%q) errored: %v", c.in, err)
			continue
		}
		if !c.wantOk {
			if err == nil {
				t.Errorf("parseExpiry(%q) expected error, got nil", c.in)
			}
			continue
		}
		diff := d.Sub(now)
		if diff < time.Duration(c.minDays)*24*time.Hour {
			t.Errorf("parseExpiry(%q) = %v, expected at least %d days from now", c.in, d, c.minDays)
		}
	}
}

func TestParseExpiry_Unsupported(t *testing.T) {
	for _, in := range []string{"", "never", "7d", "5y", "garbage", "30days"} {
		if _, err := parseExpiry(in); err == nil {
			t.Errorf("parseExpiry(%q) expected error, got nil", in)
		}
	}
}

func TestToScopeList(t *testing.T) {
	in := []string{"read:trades", "read:analytics"}
	got := toScopeList(in)
	if len(got) != 2 {
		t.Fatalf("want 2 scopes, got %d", len(got))
	}
	if got[0] != api_key.ScopeReadTrades || got[1] != api_key.ScopeReadAnalytics {
		t.Errorf("scope mapping wrong: %v", got)
	}
}

func TestToScopeList_Empty(t *testing.T) {
	got := toScopeList(nil)
	if got == nil {
		t.Fatalf("want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Errorf("want 0, got %d", len(got))
	}
}