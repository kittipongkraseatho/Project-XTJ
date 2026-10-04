package billing_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/payment"
	"github.com/trading-journal/api/internal/domain/subscription"
	"github.com/trading-journal/api/internal/integration/billing"
)

// ── Fake EventApplyStore ─────────────────────────────────────────────

type fakeStore struct {
	mu           sync.Mutex
	events          map[string]bool          // stripe_event_id → already seen
	subsByExternal map[string]*subscription.Subscription // external_sub_id → row
	subsByID       map[uuid.UUID]*subscription.Subscription
	paymentsByInv  map[string]*payment.Payment // external_invoice_id → row
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		events:          map[string]bool{},
		subsByExternal: map[string]*subscription.Subscription{},
		subsByID:       map[uuid.UUID]*subscription.Subscription{},
		paymentsByInv:  map[string]*payment.Payment{},
	}
}

func (f *fakeStore) WebhookEventInsert(_ context.Context, id, _ string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.events[id] {
		return false, nil // already seen
	}
	f.events[id] = true
	return true, nil
}

func (f *fakeStore) SubscriptionUpsertByExternalID(_ context.Context, s *subscription.Subscription) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if existing, ok := f.subsByExternal[*s.ExternalSubscriptionID]; ok {
		// ON CONFLICT DO UPDATE — keep external id, take everything else from new row.
		existing.Status = s.Status
		existing.CurrentPeriodStart = s.CurrentPeriodStart
		existing.CurrentPeriodEnd = s.CurrentPeriodEnd
		existing.CancelAt = s.CancelAt
		existing.CanceledAt = s.CanceledAt
		existing.Metadata = s.Metadata
		return nil
	}
	f.subsByExternal[*s.ExternalSubscriptionID] = s
	f.subsByID[s.ID] = s
	return nil
}

func (f *fakeStore) SubscriptionGetByExternalID(_ context.Context, externalID string) (*subscription.Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if s, ok := f.subsByExternal[externalID]; ok {
		return s, nil
	}
	return nil, errors.New("not found")
}

func (f *fakeStore) PaymentUpsertByExternalInvoice(_ context.Context, p *payment.Payment) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paymentsByInv[p.ExternalInvoiceID] = p
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ── Tests ───────────────────────────────────────────────────────────

// mkEvent builds a verified *WebhookEvent for one of the 5 supported types.
func mkEvent(t *testing.T, eventID, eventType, customerID, subID, invoiceID string, amount int64) *billing.WebhookEvent {
	t.Helper()
	now := time.Now().UTC().Unix()
	obj := map[string]any{
		"id":                  invoiceID,
		"customer":            customerID,
		"subscription":        subID,
		"amount_paid":         amount,
		"currency":            "thb",
		"status":              "paid",
		"current_period_start": now,
		"current_period_end":   now + 30*86400,
		"cancel_at_period_end": false,
		"items": []map[string]any{
			{"price": map[string]any{"lookup_key": "pro"}},
		},
	}
	raw := map[string]any{
		"id":   eventID,
		"type": eventType,
		"data": map[string]any{"object": obj},
	}
	body, _ := json.Marshal(raw)
	ev, err := (&billing.StubService{}).VerifyWebhookSignature(body, "")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	// StubService.VerifyWebhookSignature only sets SubscriptionID etc.
	// when data.object exists. Augment for the tests that need them.
	ev.SubscriptionID = subID
	ev.CustomerID = customerID
	ev.InvoiceID = invoiceID
	ev.Amount = amount
	ev.Currency = "thb"
	return ev
}

// newStripeServiceWithFake wires a real StripeService against the fake
// store. The SDK client is unused for the HandleWebhookEvent path.
func newStripeServiceWithFake(t *testing.T, fake *fakeStore) *billing.StripeService {
	t.Helper()
	// NewStripeService requires a non-empty SecretKey. We pass a fake key
	// because the webhook-apply path doesn't call the SDK.
	s, err := billing.NewStripeService(billing.Config{
		SecretKey: "sk_test_dummy_for_unit_tests",
		Events:    fake,
	})
	if err != nil {
		t.Fatalf("NewStripeService: %v", err)
	}
	return s
}

func TestStripeService_CheckoutCompleted_CreatesSubscription(t *testing.T) {
	fake := newFakeStore()
	svc := newStripeServiceWithFake(t, fake)

	tenantID := uuid.Must(uuid.NewV7())
	body, _ := json.Marshal(map[string]any{
		"id":   "evt_1",
		"type": "checkout.session.completed",
		"data": map[string]any{
			"object": map[string]any{
				"id":                 "cs_test_1",
				"customer":           "cus_1",
				"subscription":       "sub_1",
				"client_reference_id": tenantID.String(),
				"metadata": map[string]any{
					"tenant_id": tenantID.String(),
					"plan_code": "pro",
				},
			},
		},
	})
	ev, err := (&billing.StubService{}).VerifyWebhookSignature(body, "")
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	ev.SubscriptionID = "sub_1"

	if err := svc.HandleWebhookEvent(context.Background(), ev); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}
	if _, ok := fake.subsByExternal["sub_1"]; !ok {
		t.Fatalf("subscription not created")
	}
}

func TestStripeService_SubscriptionUpdated_UpdatesExisting(t *testing.T) {
	fake := newFakeStore()
	svc := newStripeServiceWithFake(t, fake)

	// Seed an existing row.
	provider := "stripe"
	seedID := uuid.Must(uuid.NewV7())
	fake.subsByExternal["sub_2"] = &subscription.Subscription{
		ID:                     seedID,
		TenantID:               uuid.Must(uuid.NewV7()),
		Status:                 subscription.StatusActive,
		ExternalProvider:       &provider,
		ExternalSubscriptionID: strPtr("sub_2"),
	}

	ev := mkEvent(t, "evt_2", "customer.subscription.updated", "cus_2", "sub_2", "", 0)
	if err := svc.HandleWebhookEvent(context.Background(), ev); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}
	got := fake.subsByExternal["sub_2"]
	if got.Metadata["plan_code"] != "pro" {
		t.Errorf("plan_code not propagated, got %v", got.Metadata)
	}
}

func TestStripeService_SubscriptionDeleted_MarksCanceled(t *testing.T) {
	fake := newFakeStore()
	svc := newStripeServiceWithFake(t, fake)

	provider := "stripe"
	fake.subsByExternal["sub_3"] = &subscription.Subscription{
		ID:                     uuid.Must(uuid.NewV7()),
		TenantID:               uuid.Must(uuid.NewV7()),
		Status:                 subscription.StatusActive,
		ExternalProvider:       &provider,
		ExternalSubscriptionID: strPtr("sub_3"),
	}

	ev := mkEvent(t, "evt_3", "customer.subscription.deleted", "cus_3", "sub_3", "", 0)
	if err := svc.HandleWebhookEvent(context.Background(), ev); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}
	got := fake.subsByExternal["sub_3"]
	if got.Status != subscription.StatusCanceled {
		t.Errorf("want canceled, got %s", got.Status)
	}
	if got.CanceledAt == nil {
		t.Errorf("want canceled_at set")
	}
}

func TestStripeService_InvoicePaid_InsertsPayment(t *testing.T) {
	fake := newFakeStore()
	svc := newStripeServiceWithFake(t, fake)

	provider := "stripe"
	fake.subsByExternal["sub_4"] = &subscription.Subscription{
		ID:                     uuid.Must(uuid.NewV7()),
		TenantID:               uuid.Must(uuid.NewV7()),
		Status:                 subscription.StatusActive,
		ExternalProvider:       &provider,
		ExternalSubscriptionID: strPtr("sub_4"),
	}

	ev := mkEvent(t, "evt_4", "invoice.paid", "cus_4", "sub_4", "in_4", 19900)
	if err := svc.HandleWebhookEvent(context.Background(), ev); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}
	got, ok := fake.paymentsByInv["in_4"]
	if !ok {
		t.Fatalf("payment not inserted")
	}
	if got.Status != payment.StatusPaid {
		t.Errorf("want paid, got %s", got.Status)
	}
	if got.AmountCents != 19900 {
		t.Errorf("amount: want 19900, got %d", got.AmountCents)
	}
}

func TestStripeService_InvoiceFailed_MarksPastDueAndInsertsFailedPayment(t *testing.T) {
	fake := newFakeStore()
	svc := newStripeServiceWithFake(t, fake)

	provider := "stripe"
	fake.subsByExternal["sub_5"] = &subscription.Subscription{
		ID:                     uuid.Must(uuid.NewV7()),
		TenantID:               uuid.Must(uuid.NewV7()),
		Status:                 subscription.StatusActive,
		ExternalProvider:       &provider,
		ExternalSubscriptionID: strPtr("sub_5"),
	}

	ev := mkEvent(t, "evt_5", "invoice.payment_failed", "cus_5", "sub_5", "in_5", 19900)
	if err := svc.HandleWebhookEvent(context.Background(), ev); err != nil {
		t.Fatalf("HandleWebhookEvent: %v", err)
	}
	if got := fake.subsByExternal["sub_5"]; got.Status != subscription.StatusPastDue {
		t.Errorf("want past_due, got %s", got.Status)
	}
	if got, ok := fake.paymentsByInv["in_5"]; !ok {
		t.Fatalf("payment not inserted")
	} else if got.Status != payment.StatusFailed {
		t.Errorf("want failed, got %s", got.Status)
	}
}

func TestStripeService_UnknownEvent_ReturnsErrUnknownEvent(t *testing.T) {
	fake := newFakeStore()
	svc := newStripeServiceWithFake(t, fake)

	ev := mkEvent(t, "evt_99", "weird.unknown.event", "cus_99", "sub_99", "", 0)
	err := svc.HandleWebhookEvent(context.Background(), ev)
	var unknown billing.ErrUnknownEvent
	if !errors.As(err, &unknown) {
		t.Fatalf("want ErrUnknownEvent, got %v", err)
	}
}

func TestStripeService_Idempotency_SecondCallIsNoop(t *testing.T) {
	fake := newFakeStore()
	svc := newStripeServiceWithFake(t, fake)

	tenantID := uuid.Must(uuid.NewV7())
	body, _ := json.Marshal(map[string]any{
		"id":   "evt_idem",
		"type": "checkout.session.completed",
		"data": map[string]any{
			"object": map[string]any{
				"id":                 "cs_test_idem",
				"customer":           "cus_idem",
				"subscription":       "sub_idem",
				"client_reference_id": tenantID.String(),
				"metadata":           map[string]any{"tenant_id": tenantID.String(), "plan_code": "pro"},
			},
		},
	})
	ev, _ := (&billing.StubService{}).VerifyWebhookSignature(body, "")
	ev.SubscriptionID = "sub_idem"

	// First call — inserts the event row + subscription row.
	if err := svc.HandleWebhookEvent(context.Background(), ev); err != nil {
		t.Fatalf("first call: %v", err)
	}
	first := fake.subsByExternal["sub_idem"]
	if first == nil {
		t.Fatalf("first call didn't create row")
	}

	// Second call with same event id — must be a no-op (no error,
	// existing row preserved).
	if err := svc.HandleWebhookEvent(context.Background(), ev); err != nil {
		t.Fatalf("second call: %v", err)
	}
	// Row must still be the same instance (no overwrite).
	if fake.subsByExternal["sub_idem"] != first {
		t.Errorf("second call clobbered the row")
	}
}

func TestStubService_HandleWebhookEvent_Noop(t *testing.T) {
	svc := &billing.StubService{}
	ev := mkEvent(t, "evt_x", "checkout.session.completed", "cus_x", "sub_x", "", 0)
	if err := svc.HandleWebhookEvent(context.Background(), ev); err != nil {
		t.Fatalf("stub HandleWebhookEvent should no-op: %v", err)
	}
}

// strPtr helper.
func strPtr(s string) *string { return &s }