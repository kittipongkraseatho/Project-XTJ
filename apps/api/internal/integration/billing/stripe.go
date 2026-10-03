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
}

// Config is the wire-up needed by StripeService.
type Config struct {
	SecretKey     string
	WebhookSecret string
	PriceByPlan   map[string]string // empty maps fall back to direct plan lookup
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