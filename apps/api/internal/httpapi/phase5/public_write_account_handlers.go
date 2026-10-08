// Package phase5http — public write handlers for accounts (Phase 10).
package phase5http

import (
	"net/http"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/domain/api_key"
	"github.com/trading-journal/api/internal/integration/apikey"
	"github.com/trading-journal/api/internal/observability"
)

// apikeyFromCtx returns the API key attached by api_key.Middleware, or nil.
func apikeyFromCtx(c echo.Context) *api_key.APIKey {
	return apikey.FromContext(c.Request().Context())
}

// AccountUpdateInput is the PATCH body for /v1/public/accounts/:id.
type AccountUpdateInput struct {
	DisplayName *string  `json:"display_name,omitempty"`
	Currency    *string  `json:"currency,omitempty"`
	Leverage    *float64 `json:"leverage,omitempty"`
}

// UpdatePublicAccount — PATCH /v1/public/accounts/:id
// Phase 1 ships POST /v1/accounts but no PATCH; we add a thin one.
func (d *PublicWriteDeps) UpdatePublicAccount(c echo.Context) error {
	tenantID, ok := d.tenant(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	a, err := d.Store.TradingAccounts.GetByID(c.Request().Context(), id)
	if err != nil || a.TenantID != tenantID {
		return c.JSON(http.StatusNotFound, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "account not found"})))
	}
	var body AccountUpdateInput
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	if body.DisplayName != nil {
		a.DisplayName = *body.DisplayName
	}
	if body.Currency != nil {
		a.Currency = *body.Currency
	}
	if body.Leverage != nil {
		a.Leverage = body.Leverage
	}
	if err := d.Store.TradingAccounts.Update(c.Request().Context(), a); err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	d.EmitAuditLog(c, "trading_account", a.ID, body)
	return c.JSON(http.StatusOK, a)
}