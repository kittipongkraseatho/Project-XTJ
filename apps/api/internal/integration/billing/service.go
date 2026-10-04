// Package billing abstracts the payment provider (Stripe today, Omise tomorrow).
//
// The Service interface is the only surface HTTP handlers touch. Switching
// providers = swap the constructor in router.go.
package billing

import (
	"context"
	"time"

	"github.com/gofrs/uuid"
)

// Service is the contract every billing provider implements.
type Service interface {
	// CreateCheckout creates a hosted Checkout session for upgrading to the
	// given plan_code. The caller (HTTP handler) redirects the browser to
	// CheckoutURL.
	CreateCheckout(ctx context.Context, req CheckoutRequest) (CheckoutResult, error)

	// PortalURL returns the Customer Portal URL (Stripe Billing Portal, etc.)
	// where users can manage payment methods / view invoices / cancel.
	PortalURL(ctx context.Context, req PortalRequest) (string, error)

	// CurrentSubscription fetches the active subscription for the tenant from
	// the provider. Returns ErrNoSubscription if there is none.
	CurrentSubscription(ctx context.Context, tenantID uuid.UUID) (*Subscription, error)

	// CancelSubscription cancels the subscription at the next period end (or
	// immediately if atPeriodEnd is false). Phase 4 honors atPeriodEnd=true.
	CancelSubscription(ctx context.Context, req CancelRequest) (*Subscription, error)

	// VerifyWebhookSignature verifies the Stripe-Signature header and returns
	// the parsed event payload (JSON body). Implementations are responsible
	// for constant-time comparison and tolerance window checks.
	VerifyWebhookSignature(payload []byte, signatureHeader string) (*WebhookEvent, error)

	// HandleWebhookEvent applies a verified webhook event to local state.
	// Phase 7 added this for the Stripe subscription/payment state machine.
	// Stub implementations return nil (no-op).
	//
	// Idempotent: duplicate event ids (per WebhookEvent.ID) are no-ops.
	// Returns ErrUnknownEvent if the type is not handled.
	HandleWebhookEvent(ctx context.Context, ev *WebhookEvent) error
}

// ── DTOs ──────────────────────────────────────────────────────────────

type CheckoutRequest struct {
	TenantID    uuid.UUID
	CustomerEmail string
	PlanCode      string // maps to a Stripe price id via cfg.PriceIDFor(plan)
	SuccessURL   string
	CancelURL    string
}

type CheckoutResult struct {
	CheckoutURL string
	SessionID   string
}

type PortalRequest struct {
	TenantID    uuid.UUID
	ReturnURL   string
}

type Subscription struct {
	Provider             string
	ExternalID           string
	Status               string // active, trialing, past_due, canceled, etc.
	PlanCode             string
	CurrentPeriodStart   time.Time
	CurrentPeriodEnd     time.Time
	CancelAt             *time.Time
	CancelAtPeriodEnd    bool
	CustomerExternalID   string
}

type CancelRequest struct {
	TenantID      uuid.UUID
	AtPeriodEnd   bool
}

type WebhookEvent struct {
	Type           string         // "checkout.session.completed", etc.
	ID             string         // Stripe event id, used for idempotency
	SubscriptionID string         // external subscription id
	CustomerID     string         // external customer id
	InvoiceID      string         // external invoice id (for invoice events)
	Amount         int64          // amount in minor units (cents)
	Currency       string         // ISO 4217
	Status         string         // paid / failed / etc.
	RawPayload     map[string]any // full event payload for audit
}

// ── Errors ───────────────────────────────────────────────────────────

// ErrNoSubscription is returned by CurrentSubscription when no active
// subscription exists for the tenant.
type ErrNoSubscription struct{}

func (ErrNoSubscription) Error() string { return "no active subscription for tenant" }

// ErrInvalidWebhook is returned when signature verification fails.
type ErrInvalidWebhook struct{ Reason string }

func (e ErrInvalidWebhook) Error() string { return "invalid webhook: " + e.Reason }

// ErrUnknownEvent is returned by HandleWebhookEvent when the event type
// is not one we handle (Phase 7 = 5 known types).
type ErrUnknownEvent struct{ Type string }

func (e ErrUnknownEvent) Error() string { return "unknown billing event: " + e.Type }