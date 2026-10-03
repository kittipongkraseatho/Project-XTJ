package webhook_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/webhook"
	whsvc "github.com/trading-journal/api/internal/integration/webhook"
)

// ── HMAC ─────────────────────────────────────────────────────────────

func TestSignAndVerifyPayload(t *testing.T) {
	body := []byte(`{"hello":"world"}`)
	sig := whsvc.SignPayload("secret", 1700000000, body)
	if !strings.HasPrefix(sig, "sha256=") {
		t.Fatalf("want sha256= prefix, got %q", sig)
	}
	if !whsvc.VerifyPayload("secret", 1700000000, body, sig) {
		t.Fatalf("verify failed for matching secret/ts/body")
	}
	if whsvc.VerifyPayload("wrong-secret", 1700000000, body, sig) {
		t.Fatalf("verify must reject wrong secret")
	}
	if whsvc.VerifyPayload("secret", 1700000001, body, sig) {
		t.Fatalf("verify must reject wrong ts")
	}
}

func TestSignPayload_DifferentInputsDiffer(t *testing.T) {
	body := []byte(`{"a":1}`)
	s1 := whsvc.SignPayload("k", 100, body)
	s2 := whsvc.SignPayload("k", 101, body)
	if s1 == s2 {
		t.Fatalf("different ts must produce different sig")
	}
}

// ── Retry classification ─────────────────────────────────────────────────

func TestIsRetryableResponse(t *testing.T) {
	cases := []struct {
		code int
		want bool
	}{
		{200, false}, {201, false}, {204, false},
		{400, false}, {401, false}, {403, false}, {404, false},
		{408, true},  // timeout
		{429, true},  // too many requests
		{500, true}, {502, true}, {503, true}, {504, true},
	}
	for _, c := range cases {
		if got := webhook.IsRetryableResponse(c.code); got != c.want {
			t.Errorf("IsRetryableResponse(%d) = %v, want %v", c.code, got, c.want)
		}
	}
}

// ── Backoff schedule ───────────────────────────────────────────────────

func TestNextBackoff(t *testing.T) {
	want := []time.Duration{
		1 * time.Minute,  // after attempt 1
		5 * time.Minute,  // after attempt 2
		30 * time.Minute, // after attempt 3
		2 * time.Hour,    // after attempt 4
		12 * time.Hour,   // after attempt 5
	}
	for i, w := range want {
		got, ok := webhook.NextBackoff(i + 1)
		if !ok {
			t.Errorf("attempt %d: want ok=true", i+1)
			continue
		}
		if got != w {
			t.Errorf("attempt %d: got %v, want %v", i+1, got, w)
		}
	}
	if _, ok := webhook.NextBackoff(6); ok {
		t.Errorf("attempt 6 must be out of schedule")
	}
	if _, ok := webhook.NextBackoff(0); ok {
		t.Errorf("attempt 0 must be invalid")
	}
}

// ── EventType.IsValid ──────────────────────────────────────────────────

func TestEventTypeIsValid(t *testing.T) {
	for _, e := range webhook.AllEventTypes {
		if !e.IsValid() {
			t.Errorf("%s should be valid", e)
		}
	}
	if webhook.EventType("bogus.event").IsValid() {
		t.Errorf("bogus.event must not be valid")
	}
}

// ── Service validation errors ──────────────────────────────────────────

func TestServiceImpl_Create_InvalidURL(t *testing.T) {
	svc := whsvc.NewServiceImpl(nil) // store not needed for input validation
	_, err := svc.Create(context.Background(), whsvc.CreateInput{
		TenantID:        uuid.Nil,
		CreatedByUserID: uuid.Nil,
		URL:             "",
		EventTypes:      []webhook.EventType{webhook.EventTradeCreated},
	})
	if !errors.Is(err, whsvc.ErrInvalidURL) {
		t.Fatalf("want ErrInvalidURL, got %v", err)
	}
}

func TestServiceImpl_Create_EmptyEvents(t *testing.T) {
	svc := whsvc.NewServiceImpl(nil)
	_, err := svc.Create(context.Background(), whsvc.CreateInput{
		URL:        "https://example.com/hook",
		EventTypes: nil,
	})
	if !errors.Is(err, whsvc.ErrEmptyEvents) {
		t.Fatalf("want ErrEmptyEvents, got %v", err)
	}
}

func TestServiceImpl_Create_InvalidEvent(t *testing.T) {
	svc := whsvc.NewServiceImpl(nil)
	_, err := svc.Create(context.Background(), whsvc.CreateInput{
		URL:        "https://example.com/hook",
		EventTypes: []webhook.EventType{webhook.EventType("nope.nope")},
	})
	if !errors.Is(err, whsvc.ErrInvalidEvent) {
		t.Fatalf("want ErrInvalidEvent, got %v", err)
	}
}

// ── unused stubs (kept for future Emit/DeliverDue tests with real DB) ──

type stubStore struct {
	created       atomic.Int32
	listActiveOut []*webhook.Subscription
}

func (s *stubStore) Create(ctx context.Context, d *webhook.Delivery) error {
	s.created.Add(1)
	return nil
}

type fakeHTTPDoer struct {
	responses []*http.Response
	errors    []error
	calls     atomic.Int32
}

func (f *fakeHTTPDoer) Do(req *http.Request) (*http.Response, error) {
	idx := int(f.calls.Add(1)) - 1
	if idx < len(f.errors) && f.errors[idx] != nil {
		return nil, f.errors[idx]
	}
	if idx < len(f.responses) {
		return f.responses[idx], nil
	}
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok"))}, nil
}