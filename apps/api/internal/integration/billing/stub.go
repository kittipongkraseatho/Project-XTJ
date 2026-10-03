package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/gofrs/uuid"
)

// StubService is a no-op billing provider used in local dev / tests. It:
//
//   - Generates fake checkout URLs that point at a local "/dev/checkout?..." page
//   - Returns synthetic subscription state (always 'active')
//   - Accepts every webhook signature (use ONLY for tests)
//
// Production code MUST use StripeBillingService. StubService is wired when
// STRIPE_SECRET_KEY is unset.
type StubService struct{}

// NewStubService returns the no-op implementation.
func NewStubService() Service { return &StubService{} }

func (s *StubService) CreateCheckout(_ context.Context, req CheckoutRequest) (CheckoutResult, error) {
	sessionID := "stub_cs_" + uuid.Must(uuid.NewV7()).String()[:12]
	return CheckoutResult{
		CheckoutURL: "/dev/checkout?session=" + sessionID + "&plan=" + req.PlanCode,
		SessionID:   sessionID,
	}, nil
}

func (s *StubService) PortalURL(_ context.Context, req PortalRequest) (string, error) {
	return "/dev/portal?tenant=" + req.TenantID.String() + "&return=" + req.ReturnURL, nil
}

func (s *StubService) CurrentSubscription(_ context.Context, tenantID uuid.UUID) (*Subscription, error) {
	return &Subscription{
		Provider:           "stub",
		ExternalID:         "stub_sub_" + tenantID.String()[:8],
		Status:             "active",
		PlanCode:           "free",
		CurrentPeriodStart: time.Now().Add(-15 * 24 * time.Hour),
		CurrentPeriodEnd:   time.Now().Add(15 * 24 * time.Hour),
		CancelAtPeriodEnd:  false,
		CustomerExternalID: "stub_cus_" + tenantID.String()[:8],
	}, nil
}

func (s *StubService) CancelSubscription(_ context.Context, req CancelRequest) (*Subscription, error) {
	sub, _ := s.CurrentSubscription(context.Background(), req.TenantID)
	sub.CancelAtPeriodEnd = true
	sub.Status = "active"
	return sub, nil
}

// VerifyWebhookSignature accepts any payload and decodes it as a generic
// event. Real Stripe signature verification is the only thing StubService
// deliberately skips — don't use in production.
func (s *StubService) VerifyWebhookSignature(payload []byte, _ string) (*WebhookEvent, error) {
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, ErrInvalidWebhook{Reason: "invalid JSON"}
	}
	ev := &WebhookEvent{
		Type:       asString(raw["type"]),
		ID:         asString(raw["id"]),
		RawPayload: raw,
	}
	if data, ok := raw["data"].(map[string]any); ok {
		if obj, ok := data["object"].(map[string]any); ok {
			ev.SubscriptionID = asString(obj["subscription"])
			ev.CustomerID = asString(obj["customer"])
			ev.InvoiceID = asString(obj["invoice"]) // sometimes "id"
			if ev.InvoiceID == "" {
				ev.InvoiceID = asString(obj["id"])
			}
			ev.Amount = asInt64(obj["amount_paid"])
			ev.Currency = asString(obj["currency"])
			ev.Status = asString(obj["status"])
		}
	}
	return ev, nil
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	}
	return 0
}

// VerifyStubCheckoutHandler is a tiny test-only router handler that confirms
// a stub checkout URL is reachable. Useful for FE e2e tests in stub mode.
func VerifyStubCheckoutHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("session") == "" {
		http.Error(w, "missing session", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"session": q.Get("session"),
		"plan":    q.Get("plan"),
		"stub":    true,
	})
}

// StubCheckoutMiddleware ensures the local /dev/checkout route is reachable.
func StubCheckoutMiddleware() http.Handler {
	return httptest.NewServer(http.HandlerFunc(VerifyStubCheckoutHandler)).Config.Handler
}