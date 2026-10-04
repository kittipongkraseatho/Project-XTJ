// Package phase6http — Phase 6 AI analysis admin endpoints.
//
//	POST /v1/trades/:id/analyze     — kick off an analysis (returns the row immediately)
//	GET  /v1/trades/:id/analysis    — get the latest analysis for the trade
//	GET  /v1/ai/usage               — tenant's daily usage + budget headroom
package phase6http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/auth"
	intai "github.com/trading-journal/api/internal/integration/ai"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/store"
	"github.com/trading-journal/api/internal/tenant"
)

// AIHandler owns the /v1/ai/* + /v1/trades/:id/analysis routes.
type AIHandler struct {
	svc   intai.Service
	store *store.Store // for plan-code lookup (Phase 6 daily budget)
	log   *slog.Logger
}

func NewAIHandler(svc intai.Service, st *store.Store, log *slog.Logger) *AIHandler {
	return &AIHandler{svc: svc, store: st, log: log}
}

// ── POST /v1/trades/:id/analyze ──────────────────────────────────────

func (h *AIHandler) Analyze(c echo.Context) error {
	tid, err := tenant.TenantIDFromContext(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	uid, ok := uidFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	tradeID, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid trade id"})))
	}
	planCode := h.lookupPlanCode(c.Request().Context(), tid)
	a, err := h.svc.Analyze(c.Request().Context(), intai.AnalyzeInput{
		TenantID: tid,
		TradeID:  tradeID,
		UserID:   uid,
		PlanCode: planCode,
	})
	if err != nil {
		switch {
		case errors.Is(err, intai.ErrBudgetExhausted):
			return c.JSON(http.StatusPaymentRequired, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{
					"reason": "daily_budget_exhausted",
				})))
		case errors.Is(err, intai.ErrNoSuchTrade):
			return c.JSON(http.StatusNotFound, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "trade not found"})))
		default:
			return c.JSON(http.StatusInternalServerError, observability.FromError(err))
		}
	}
	// 202 if pending; 200 if cached success.
	if a.Status == "success" {
		return c.JSON(http.StatusOK, a)
	}
	return c.JSON(http.StatusAccepted, a)
}

// ── GET /v1/trades/:id/analysis ─────────────────────────────────────

func (h *AIHandler) GetAnalysis(c echo.Context) error {
	tid, err := tenant.TenantIDFromContext(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	tradeID, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid trade id"})))
	}
	res, err := h.svc.GetLatestForTrade(c.Request().Context(), tid, tradeID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// No analysis yet — return 404 so the FE can show "click Analyze"
			return c.JSON(http.StatusNotFound, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "no analysis yet"})))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, res)
}

// ── GET /v1/ai/usage ─────────────────────────────────────────────────

func (h *AIHandler) GetUsage(c echo.Context) error {
	tid, err := tenant.TenantIDFromContext(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	u, err := h.svc.GetDailyUsage(c.Request().Context(), tid)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	// Fill in budget from plan code.
	planCode := h.lookupPlanCode(c.Request().Context(), tid)
	u.DailyBudget = budgetForPlan(planCode) // local helper
	return c.JSON(http.StatusOK, map[string]any{
		"day":             u.Day,
		"requests":        u.Requests,
		"tokens_in":       u.TokensIn,
		"tokens_out":      u.TokensOut,
		"daily_budget":    budgetForPlan(planCode),
		"hard_daily_cap":  100,
		"headroom":        headroom(u.Requests, budgetForPlan(planCode)),
	})
}

// ── helpers ─────────────────────────────────────────────────────────

func budgetForPlan(planCode string) int {
	switch planCode {
	case "pro":
		return 50
	default:
		return 5
	}
}

func headroom(used, budget int) int {
	h := budget - used
	if h < 0 {
		return 0
	}
	return h
}

// uidFromCtx extracts the JWT user id via the auth package.
func uidFromCtx(c echo.Context) (uuid.UUID, bool) {
	return auth.UserIDFromContext(c.Request().Context())
}

// lookupPlanCode returns the tenant's plan code. Phase 6 ships a
// minimal lookup — production hardening can switch to the
// entitlement package.
func (h *AIHandler) lookupPlanCode(_ context.Context, _ uuid.UUID) string {
	// Default to free; Phase 6.x ship a real lookup via subscriptions table.
	return ""
}