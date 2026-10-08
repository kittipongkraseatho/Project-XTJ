// Package phase5http — public write handlers for trades (Phase 10).
//
// Phase 1 ships no public trade-create endpoint (it only does imports).
// These 2 thin handlers implement write:trades scope:
package phase5http

import (
	"errors"
	"net/http"
	"time"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/domain/trade"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/store"
)

// TradeCreateInput is the public POST body for /v1/public/trades.
type TradeCreateInput struct {
	TradingAccountID uuid.UUID `json:"trading_account_id"`
	Symbol           string    `json:"symbol"`
	Direction        string    `json:"direction"`
	Volume           float64   `json:"volume"`
	OpenPrice        float64   `json:"open_price"`
	OpenTime         time.Time `json:"open_time"`
	ClosePrice       *float64  `json:"close_price,omitempty"`
	CloseTime        *time.Time `json:"close_time,omitempty"`
	Magic            *int64    `json:"magic,omitempty"`
}

// TradeUpdateInput is the PATCH body for /v1/public/trades/:id.
type TradeUpdateInput struct {
	ClosePrice *float64   `json:"close_price,omitempty"`
	CloseTime  *time.Time `json:"close_time,omitempty"`
	Magic      *int64     `json:"magic,omitempty"`
}

// CreatePublicTrade — POST /v1/public/trades
// Requires write:trades scope; tenant_id derived from API key.
func (d *PublicWriteDeps) CreatePublicTrade(c echo.Context) error {
	tenantID, ok := d.tenant(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	var body TradeCreateInput
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	if body.TradingAccountID == uuid.Nil || body.Symbol == "" ||
		body.Direction == "" || body.Volume == 0 || body.OpenPrice == 0 || body.OpenTime.IsZero() {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "missing required field"})))
	}
	// Verify the trading account belongs to the tenant.
	acc, err := d.Store.TradingAccounts.GetByID(c.Request().Context(), body.TradingAccountID)
	if err != nil || acc.TenantID != tenantID {
		return c.JSON(http.StatusNotFound, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "trading_account not found"})))
	}
	// The user_id for the trade = API key owner.
	k := apikeyFromCtx(c)
	uid := k.CreatedByUserID
	tr := &trade.Trade{
		ID:               uuid.Must(uuid.NewV7()),
		TenantID:         tenantID,
		UserID:           uid,
		TradingAccountID: body.TradingAccountID,
		Symbol:           body.Symbol,
		Direction:        trade.Direction(body.Direction),
		Volume:           body.Volume,
		OpenPrice:        body.OpenPrice,
		ClosePrice:       body.ClosePrice,
		OpenTime:         body.OpenTime.UTC(),
		CloseTime:        body.CloseTime,
		Magic:            body.Magic,
		Metadata:         map[string]any{},
	}
	if err := d.Store.Trades.Insert(c.Request().Context(), tr); err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	d.EmitAuditLog(c, "trade", tr.ID, body)
	return c.JSON(http.StatusCreated, tr)
}

// UpdatePublicTrade — PATCH /v1/public/trades/:id
func (d *PublicWriteDeps) UpdatePublicTrade(c echo.Context) error {
	tenantID, ok := d.tenant(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	tr, err := d.Store.Trades.GetByID(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "trade not found"})))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	if tr.TenantID != tenantID {
		return c.JSON(http.StatusNotFound, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "trade not found"})))
	}
	var body TradeUpdateInput
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	if body.ClosePrice != nil {
		tr.ClosePrice = body.ClosePrice
	}
	if body.CloseTime != nil {
		tr.CloseTime = body.CloseTime
	}
	if body.Magic != nil {
		tr.Magic = body.Magic
	}
	if err := d.Store.Trades.Update(c.Request().Context(), tr); err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	d.EmitAuditLog(c, "trade", tr.ID, body)
	return c.JSON(http.StatusOK, tr)
}