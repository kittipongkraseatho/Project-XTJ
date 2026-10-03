// Package phase4http hosts HTTP handlers for Phase 4 endpoints
// (/v1/billing/* + /v1/reports/schedules/*).
package phase4http

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/auth"
	"github.com/trading-journal/api/internal/domain/plan"
	"github.com/trading-journal/api/internal/integration/billing"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/store"
)

// Handler is the Phase 4 HTTP handler container.
type Handler struct {
	store   *store.Store
	billing billing.Service
	log     *slog.Logger
}

func New(st *store.Store, b billing.Service, log *slog.Logger) *Handler {
	return &Handler{store: st, billing: b, log: log}
}

// ── helpers ──────────────────────────────────────────────────────────

func idsFromCtx(c echo.Context) (uuid.UUID, uuid.UUID, bool) {
	uid, ok1 := auth.UserIDFromContext(c.Request().Context())
	if !ok1 {
		return uuid.Nil, uuid.Nil, false
	}
	v, has := c.Get("tenant_id").(string)
	if !has || v == "" {
		return uuid.Nil, uuid.Nil, false
	}
	tid, err := uuid.FromString(v)
	if err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	return uid, tid, true
}

func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ── Plans ────────────────────────────────────────────────────────────

// ListPlans — GET /v1/billing/plans
func (h *Handler) ListPlans(c echo.Context) error {
	plans, err := h.store.Plans.ListActive(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	type planDTO struct {
		Code        string `json:"code"`
		Name        string `json:"name"`
		Description string `json:"description"`
		PriceCents  int64  `json:"price_cents"`
		Currency    string `json:"currency"`
		Interval    string `json:"interval"`
	}
	out := make([]planDTO, 0, len(plans))
	for _, p := range plans {
		out = append(out, planDTO{
			Code:        string(p.Code),
			Name:        p.Name,
			Description: strOrEmpty(p.Description),
			PriceCents:  p.PriceCents,
			Currency:    p.Currency,
			Interval:    string(p.Interval),
		})
	}
	return c.JSON(http.StatusOK, map[string]any{"plans": out})
}

// ── Checkout ─────────────────────────────────────────────────────────

// Checkout — POST /v1/billing/checkout
//
// Body: {plan_code, success_url, cancel_url}. Returns {checkout_url, session_id}.
func (h *Handler) Checkout(c echo.Context) error {
	uid, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	var body struct {
		PlanCode   string `json:"plan_code"`
		SuccessURL string `json:"success_url"`
		CancelURL  string `json:"cancel_url"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	if body.PlanCode == "" || body.SuccessURL == "" || body.CancelURL == "" {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "missing required field"})))
	}
	if !isKnownPlanCode(body.PlanCode) {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "unknown plan_code"})))
	}

	email := ""
	if u, err := h.store.Users.GetByID(c.Request().Context(), uid); err == nil {
		email = u.Email
	}

	res, err := h.billing.CreateCheckout(c.Request().Context(), billing.CheckoutRequest{
		TenantID:      tid,
		CustomerEmail: email,
		PlanCode:      body.PlanCode,
		SuccessURL:    body.SuccessURL,
		CancelURL:     body.CancelURL,
	})
	if err != nil {
		h.log.Error("billing.checkout_failed", "tenant_id", tid, "plan", body.PlanCode, "err", err)
		return c.JSON(http.StatusBadGateway, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, res)
}

// Portal — GET /v1/billing/portal
func (h *Handler) Portal(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	url, err := h.billing.PortalURL(c.Request().Context(), billing.PortalRequest{
		TenantID:  tid,
		ReturnURL: origin(c, "/billing"),
	})
	if err != nil {
		return c.JSON(http.StatusBadGateway, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"portal_url": url})
}

// ── Subscription ─────────────────────────────────────────────────────

// CurrentSubscription — GET /v1/billing/subscription
func (h *Handler) CurrentSubscription(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}

	sub, err := h.store.Subscriptions.GetActiveByTenant(c.Request().Context(), tid)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "no active subscription"})))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}

	p, err := h.store.Plans.GetByID(c.Request().Context(), sub.PlanID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}

	type subDTO struct {
		PlanCode               string `json:"plan_code"`
		PlanName               string `json:"plan_name"`
		Status                 string `json:"status"`
		CurrentPeriodStart     string `json:"current_period_start"`
		CurrentPeriodEnd       string `json:"current_period_end"`
		CancelAt               string `json:"cancel_at,omitempty"`
		CancelAtPeriodEnd      bool   `json:"cancel_at_period_end"`
		ExternalSubscriptionID string `json:"external_subscription_id,omitempty"`
	}
	dto := subDTO{
		PlanCode:           string(p.Code),
		PlanName:           p.Name,
		Status:             string(sub.Status),
		CurrentPeriodStart: sub.CurrentPeriodStart.Format("2006-01-02"),
		CurrentPeriodEnd:   sub.CurrentPeriodEnd.Format("2006-01-02"),
		CancelAtPeriodEnd:  sub.CancelAt != nil,
	}
	if sub.CancelAt != nil {
		dto.CancelAt = sub.CancelAt.Format("2006-01-02")
	}
	if sub.ExternalSubscriptionID != nil {
		dto.ExternalSubscriptionID = *sub.ExternalSubscriptionID
	}
	return c.JSON(http.StatusOK, dto)
}

// Cancel — POST /v1/billing/cancel
func (h *Handler) Cancel(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	var body struct {
		AtPeriodEnd *bool `json:"at_period_end"`
	}
	_ = c.Bind(&body)
	atEnd := true
	if body.AtPeriodEnd != nil {
		atEnd = *body.AtPeriodEnd
	}

	_, err := h.billing.CancelSubscription(c.Request().Context(), billing.CancelRequest{
		TenantID:    tid,
		AtPeriodEnd: atEnd,
	})
	if err != nil {
		if errors.Is(err, billing.ErrNoSubscription{}) {
			return c.JSON(http.StatusNotFound, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "no active subscription"})))
		}
		return c.JSON(http.StatusBadGateway, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"status": "cancel_scheduled", "at_period_end": atEnd})
}

// Payments — GET /v1/billing/payments
func (h *Handler) Payments(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	payments, err := h.store.Payments.ListByTenant(c.Request().Context(), tid, 50)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"payments": payments})
}

// Webhook — POST /v1/billing/webhook
//
// Public endpoint (no JWT). Stripe signs each request; we verify via the
// billing.Service implementation.
//
// Phase 4 ships verification only; subscription state updates are deferred
// to Phase 5 (the real provider integration).
func (h *Handler) Webhook(c echo.Context) error {
	body := make([]byte, 0)
	if c.Request().Body != nil {
		buf := make([]byte, 1024*1024)
		n, _ := c.Request().Body.Read(buf)
		body = buf[:n]
	}
	sig := c.Request().Header.Get("Stripe-Signature")
	if sig == "" {
		return c.JSON(http.StatusBadRequest, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "missing Stripe-Signature"})))
	}
	if _, err := h.billing.VerifyWebhookSignature(body, sig); err != nil {
		var invalid billing.ErrInvalidWebhook
		if errors.As(err, &invalid) {
			return c.JSON(http.StatusBadRequest, observability.FromError(invalid))
		}
		return c.JSON(http.StatusBadRequest, observability.FromError(err))
	}
	h.log.Info("billing.webhook_verified", "size", len(body))
	return c.NoContent(http.StatusNoContent)
}

// ── helpers ──────────────────────────────────────────────────────────

func origin(c echo.Context, path string) string {
	if o := c.Request().Header.Get("Origin"); o != "" {
		return o + path
	}
	return path
}

func isKnownPlanCode(s string) bool {
	switch plan.Code(s) {
	case plan.CodeFree, plan.CodeUploadJournal, plan.CodeStarter, plan.CodePro:
		return true
	}
	return false
}