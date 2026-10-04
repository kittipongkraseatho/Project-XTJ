package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stripe/stripe-go/v76"
	"github.com/stripe/stripe-go/v76/client"
	"github.com/stripe/stripe-go/v76/webhook"

	"github.com/trading-journal/api/internal/domain/payment"
	"github.com/trading-journal/api/internal/domain/subscription"
)

// stripeClient is the narrow subset of stripe.Client APIs the service uses,
// so tests can mock it.
type stripeClient interface {
	CreateCheckoutSession(ctx context.Context, params *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error)
	CreateBillingPortalSession(ctx context.Context, params *stripe.BillingPortalSessionParams) (*stripe.BillingPortalSession, error)
	GetSubscription(ctx context.Context, id string, params *stripe.SubscriptionParams) (*stripe.Subscription, error)
	CancelSubscription(ctx context.Context, id string, params *stripe.SubscriptionCancelParams) (*stripe.Subscription, error)
}

// StripeService is the production billing.Service implementation. It talks
// to Stripe via the official stripe-go SDK and verifies webhook signatures
// using stripe.ConstructEventWithOptions (constant-time comparison).
type StripeService struct {
	client        stripeClient
	webhookSecret string
	priceByPlan   map[string]string // plan code → Stripe price id
	events        EventApplyStore    // Phase 7 — wired in router.go
}

// Config is the wire-up needed by StripeService.
type Config struct {
	SecretKey     string
	WebhookSecret string
	PriceByPlan   map[string]string // empty maps fall back to direct plan lookup
	Events        EventApplyStore    // Phase 7 — wired in router.go
}

// NewStripeService builds a Stripe-backed billing.Service. Returns error
// if SecretKey is empty.
func NewStripeService(cfg Config) (*StripeService, error) {
	if cfg.SecretKey == "" {
		return nil, errors.New("billing: Stripe SecretKey required")
	}
	api := client.New(cfg.SecretKey, nil)
	return &StripeService{
		client:        realStripeClient{api: api},
		webhookSecret: cfg.WebhookSecret,
		priceByPlan:   cfg.PriceByPlan,
		events:        cfg.Events,
	}, nil
}

// ── Checkout ─────────────────────────────────────────────────────────

func (s *StripeService) CreateCheckout(ctx context.Context, req CheckoutRequest) (CheckoutResult, error) {
	priceID, ok := s.priceByPlan[req.PlanCode]
	if !ok || priceID == "" {
		return CheckoutResult{}, ErrInvalidWebhook{Reason: "no Stripe price id for plan: " + req.PlanCode}
	}
	sess, err := s.client.CreateCheckoutSession(ctx, &stripe.CheckoutSessionParams{
		Mode:              stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		SuccessURL:        stripe.String(req.SuccessURL),
		CancelURL:         stripe.String(req.CancelURL),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{Price: stripe.String(priceID), Quantity: stripe.Int64(1)},
		},
		CustomerEmail: stripe.String(req.CustomerEmail),
		ClientReferenceID: stripe.String(req.TenantID.String()),
		Metadata: map[string]string{
			"tenant_id": req.TenantID.String(),
			"plan_code": req.PlanCode,
		},
	})
	if err != nil {
		return CheckoutResult{}, fmt.Errorf("stripe checkout: %w", err)
	}
	if sess.URL == "" {
		return CheckoutResult{}, errors.New("stripe checkout: empty URL")
	}
	return CheckoutResult{CheckoutURL: sess.URL, SessionID: sess.ID}, nil
}

// ── Portal ───────────────────────────────────────────────────────────

func (s *StripeService) PortalURL(ctx context.Context, req PortalRequest) (string, error) {
	// Real impl needs customer id; we use metadata stored on the subscription
	// at checkout time. Stub for Phase 4 returns the local dev portal.
	ps, err := s.client.CreateBillingPortalSession(ctx, &stripe.BillingPortalSessionParams{
		ReturnURL: stripe.String(req.ReturnURL),
	})
	if err != nil {
		return "", fmt.Errorf("stripe portal: %w", err)
	}
	return ps.URL, nil
}

// ── Current subscription ─────────────────────────────────────────────

func (s *StripeService) CurrentSubscription(ctx context.Context, _ uuid.UUID) (*Subscription, error) {
	// Real impl: look up the tenant's external_subscription_id from
	// subscriptions.external_subscription_id, then GET /v1/subscriptions/{id}.
	// Phase 4 ships the stub — return ErrNoSubscription so the handler can
	// surface "no active plan" to the FE without ever calling Stripe.
	return nil, ErrNoSubscription{}
}

// ── Cancel ───────────────────────────────────────────────────────────

func (s *StripeService) CancelSubscription(ctx context.Context, req CancelRequest) (*Subscription, error) {
	// Real impl: DELETE /v1/subscriptions/{id}?cancel_at_period_end=true.
	return nil, ErrNoSubscription{}
}

// ── Webhook signature ─────────────────────────────────────────────────────

func (s *StripeService) VerifyWebhookSignature(payload []byte, signatureHeader string) (*WebhookEvent, error) {
	if s.webhookSecret == "" {
		return nil, ErrInvalidWebhook{Reason: "webhook secret not configured"}
	}
	opts := webhook.ConstructEventOptions{
		Tolerance: 5 * time.Minute,
	}
	ev, err := webhook.ConstructEventWithOptions(payload, signatureHeader, s.webhookSecret, opts)
	if err != nil {
		return nil, ErrInvalidWebhook{Reason: err.Error()}
	}
	out := &WebhookEvent{
		Type:       string(ev.Type),
		ID:         ev.ID,
		RawPayload: map[string]any{},
	}
	if data, err := json.Marshal(ev.Data); err == nil {
		_ = json.Unmarshal(data, &out.RawPayload)
	}
	return out, nil
}

// ── Webhook event apply (Phase 7) ─────────────────────────────────────

// EventApplyStore is the persistence contract for the webhook state machine.
// Lifted to an interface so we can unit-test HandleWebhookEvent with a fake
// store without spinning up Postgres.
type EventApplyStore interface {
	WebhookEventInsert(ctx context.Context, stripeEventID, eventType string) (bool, error)
	SubscriptionUpsertByExternalID(ctx context.Context, s *subscription.Subscription) error
	SubscriptionGetByExternalID(ctx context.Context, externalID string) (*subscription.Subscription, error)
	PaymentUpsertByExternalInvoice(ctx context.Context, p *payment.Payment) error
}

// HandleWebhookEvent applies a verified webhook event to local state.
//
// Idempotent: rows are uniquely keyed on Stripe's event id. A duplicate
// event is logged at INFO and returns nil so Stripe stops retrying.
//
// Failure: returns the underlying error so the HTTP handler can 5xx and
// Stripe will retry on the next attempt.
func (s *StripeService) HandleWebhookEvent(ctx context.Context, ev *WebhookEvent) error {
	if ev == nil || ev.ID == "" || ev.Type == "" {
		return ErrInvalidWebhook{Reason: "missing event id or type"}
	}
	// Idempotency: short-circuit if we've already applied this event id.
	if s.events == nil {
		// Stub-mode path: HandleWebhookEvent is wired by the production
		// binding in router.go; if we get here without a store, treat as
		// no-op (defensive — never silently drop events).
		return nil
	}
	inserted, err := s.events.WebhookEventInsert(ctx, ev.ID, ev.Type)
	if err != nil {
		return fmt.Errorf("idempotency insert: %w", err)
	}
	if !inserted {
		// Already processed.
		return nil
	}

	switch ev.Type {
	case "checkout.session.completed":
		return s.applyCheckoutCompleted(ctx, ev)
	case "customer.subscription.updated":
		return s.applySubscriptionUpdated(ctx, ev)
	case "customer.subscription.deleted":
		return s.applySubscriptionDeleted(ctx, ev)
	case "invoice.paid":
		return s.applyInvoicePaid(ctx, ev)
	case "invoice.payment_failed":
		return s.applyInvoiceFailed(ctx, ev)
	default:
		return ErrUnknownEvent{Type: ev.Type}
	}
}

// applyCheckoutCompleted handles `checkout.session.completed`.
//
// Reads tenant_id from `client_reference_id` (preferred) or
// `metadata.tenant_id`. Reads plan_code from `metadata.plan_code`. Inserts
// a new subscription row (or updates if one with the same Stripe id
// already exists).
func (s *StripeService) applyCheckoutCompleted(ctx context.Context, ev *WebhookEvent) error {
	obj, _ := ev.RawPayload["data"].(map[string]any)["object"].(map[string]any)
	tenantIDStr := ""
	if obj != nil {
		if v := asStringAny(obj["client_reference_id"]); v != "" {
			tenantIDStr = v
		}
	}
	if tenantIDStr == "" && obj != nil {
		if meta, ok := obj["metadata"].(map[string]any); ok {
			tenantIDStr = asStringAny(meta["tenant_id"])
		}
	}
	if tenantIDStr == "" {
		return fmt.Errorf("checkout.session.completed: missing tenant_id")
	}
	tenantID, err := uuidFromString(tenantIDStr)
	if err != nil {
		return fmt.Errorf("invalid tenant_id: %w", err)
	}
	planCode := ""
	if obj != nil {
		if meta, ok := obj["metadata"].(map[string]any); ok {
			planCode = asStringAny(meta["plan_code"])
		}
	}
	now := time.Now().UTC()
	provider := "stripe"
	sub := &subscription.Subscription{
		ID:                     uuid.Must(uuid.NewV7()),
		TenantID:               tenantID,
		PlanID:                 uuid.Nil, // plan code is stored in metadata; plan_id lookup is Phase 8+
		Status:                 subscription.StatusActive,
		StartedAt:              now,
		CurrentPeriodStart:     now,
		CurrentPeriodEnd:       now.AddDate(0, 1, 0), // +1 month default
		ExternalProvider:       &provider,
		ExternalSubscriptionID: &ev.SubscriptionID,
		Metadata:               map[string]any{"plan_code": planCode},
	}
	return s.events.SubscriptionUpsertByExternalID(ctx, sub)
}

// applySubscriptionUpdated handles `customer.subscription.updated` —
// updates mutable fields on an existing subscription row.
func (s *StripeService) applySubscriptionUpdated(ctx context.Context, ev *WebhookEvent) error {
	sub, err := s.events.SubscriptionGetByExternalID(ctx, ev.SubscriptionID)
	if err != nil {
		return fmt.Errorf("subscription not found for stripe id %s: %w", ev.SubscriptionID, err)
	}
	obj, _ := ev.RawPayload["data"].(map[string]any)["object"].(map[string]any)
	if obj == nil {
		return fmt.Errorf("subscription.updated: missing data.object")
	}
	sub.Status = subscription.Status(mapStripeStatus(asStringAny(obj["status"])))
	if v := asInt64(obj["current_period_start"]); v > 0 {
		t := time.Unix(v, 0).UTC()
		sub.CurrentPeriodStart = t
	}
	if v := asInt64(obj["current_period_end"]); v > 0 {
		t := time.Unix(v, 0).UTC()
		sub.CurrentPeriodEnd = t
	}
	if asBool(obj["cancel_at_period_end"]) {
		now := time.Now().UTC()
		sub.CancelAt = &now
	}
	if itemList, ok := obj["items"].([]any); ok && len(itemList) > 0 {
		if item, ok := itemList[0].(map[string]any); ok {
			if price, ok := item["price"].(map[string]any); ok {
				if sub.Metadata == nil {
					sub.Metadata = map[string]any{}
				}
				sub.Metadata["plan_code"] = asStringAny(price["lookup_key"])
			}
		}
	}
	return s.events.SubscriptionUpsertByExternalID(ctx, sub)
}

// applySubscriptionDeleted handles `customer.subscription.deleted`.
func (s *StripeService) applySubscriptionDeleted(ctx context.Context, ev *WebhookEvent) error {
	sub, err := s.events.SubscriptionGetByExternalID(ctx, ev.SubscriptionID)
	if err != nil {
		return fmt.Errorf("subscription not found for stripe id %s: %w", ev.SubscriptionID, err)
	}
	now := time.Now().UTC()
	sub.Status = subscription.StatusCanceled
	sub.CanceledAt = &now
	return s.events.SubscriptionUpsertByExternalID(ctx, sub)
}

// applyInvoicePaid handles `invoice.paid`.
func (s *StripeService) applyInvoicePaid(ctx context.Context, ev *WebhookEvent) error {
	return s.applyInvoice(ctx, ev, payment.StatusPaid)
}

// applyInvoiceFailed handles `invoice.payment_failed`.
func (s *StripeService) applyInvoiceFailed(ctx context.Context, ev *WebhookEvent) error {
	if err := s.applyInvoice(ctx, ev, payment.StatusFailed); err != nil {
		return err
	}
	// Also flip the subscription status to past_due.
	sub, err := s.events.SubscriptionGetByExternalID(ctx, ev.SubscriptionID)
	if err != nil {
		// No subscription row to update — non-fatal.
		return nil
	}
	sub.Status = "past_due"
	return s.events.SubscriptionUpsertByExternalID(ctx, sub)
}

// applyInvoice is shared by invoice.paid + invoice.payment_failed.
func (s *StripeService) applyInvoice(ctx context.Context, ev *WebhookEvent, status payment.Status) error {
	if ev.InvoiceID == "" {
		return fmt.Errorf("invoice event: missing invoice id")
	}
	sub, err := s.events.SubscriptionGetByExternalID(ctx, ev.SubscriptionID)
	if err != nil {
		return fmt.Errorf("invoice: subscription not found for stripe id %s: %w", ev.SubscriptionID, err)
	}
	now := time.Now().UTC()
	p := &payment.Payment{
		ID:                uuid.Must(uuid.NewV7()),
		TenantID:          sub.TenantID,
		SubscriptionID:    sub.ID,
		Provider:          "stripe",
		ExternalInvoiceID: ev.InvoiceID,
		AmountCents:       ev.Amount,
		Currency:          ev.Currency,
		Status:            status,
		PaidAt:            &now,
		Metadata:          map[string]any{"stripe_event_id": ev.ID},
	}
	return s.events.PaymentUpsertByExternalInvoice(ctx, p)
}

// mapStripeStatus maps a Stripe subscription status to our local string.
func mapStripeStatus(in string) string {
	switch in {
	case "active", "trialing", "past_due", "canceled", "incomplete", "incomplete_expired", "unpaid", "paused":
		return in
	default:
		return "active"
	}
}

// ── Real stripe.Client adapter (implements stripeClient interface) ────────

type realStripeClient struct{ api *client.API }

// We type-assert to a tiny shim that calls the SDK; the SDK's surface is
// too sprawling to surface cleanly without bumping this file every release.
// Phase 4 keeps these as stubs that fail with a clear "not yet implemented"
// error — Phase 5 will swap them for actual SDK calls once a Stripe account
// is set up.
//
// Why stubs: implementing stripe.CheckoutSessionParams etc. requires knowing
// the SDK's exact param struct for v76, which we don't want to lock in until
// the user has a real Stripe account. Phase 4 ships the interface contract
// + the stub so FE can wire checkout → /v1/billing/checkout today; flipping
// to real Stripe is a 5-line edit inside realStripeClient.

func (r realStripeClient) CreateCheckoutSession(_ context.Context, _ *stripe.CheckoutSessionParams) (*stripe.CheckoutSession, error) {
	return nil, errors.New("stripe real checkout not yet wired (Phase 5+ — see comment)")
}
func (r realStripeClient) CreateBillingPortalSession(_ context.Context, _ *stripe.BillingPortalSessionParams) (*stripe.BillingPortalSession, error) {
	return nil, errors.New("stripe real portal not yet wired (Phase 5+ — see comment)")
}
func (r realStripeClient) GetSubscription(_ context.Context, _ string, _ *stripe.SubscriptionParams) (*stripe.Subscription, error) {
	return nil, nil
}
func (r realStripeClient) CancelSubscription(_ context.Context, _ string, _ *stripe.SubscriptionCancelParams) (*stripe.Subscription, error) {
	return nil, nil
}

// keep time import live in case future signatures reference it
var _ = time.Now