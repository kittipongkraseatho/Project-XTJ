package phase4http

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/domain/report_schedule"
	"github.com/trading-journal/api/internal/observability"
	schedulesvc "github.com/trading-journal/api/internal/service/report_schedule"
	"github.com/trading-journal/api/internal/store"
)

// SchedulesHandler is the schedules sub-handler.
type SchedulesHandler struct {
	store    *store.Store
	schedule *schedulesvc.ScheduleService
	log      *slog.Logger
}

func NewSchedulesHandler(st *store.Store, svc *schedulesvc.ScheduleService, log *slog.Logger) *SchedulesHandler {
	return &SchedulesHandler{store: st, schedule: svc, log: log}
}

// ── List schedules ────────────────────────────────────────────────────

// List — GET /v1/reports/schedules
func (h *SchedulesHandler) List(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	rows, err := h.schedule.ListByTenant(c.Request().Context(), tid)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"schedules": rows})
}

// Create — POST /v1/reports/schedules
func (h *SchedulesHandler) Create(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	var body struct {
		TradingAccountID string  `json:"trading_account_id"`
		Frequency        string  `json:"frequency"`
		HourUTC          int     `json:"hour_utc"`
		Weekday          *int    `json:"weekday"`
		DayOfMonth       *int    `json:"day_of_month"`
		Recipients       []string `json:"recipients"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	if body.TradingAccountID == "" || body.Frequency == "" {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "missing required field"})))
	}
	accID, err := uuid.FromString(body.TradingAccountID)
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid trading_account_id"})))
	}
	if !report_schedule.Frequency(body.Frequency).IsValid() {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid frequency"})))
	}

	rec, err := h.schedule.Create(c.Request().Context(), schedulesvc.CreateInput{
		TenantID:         tid,
		TradingAccountID: accID,
		Frequency:        report_schedule.Frequency(body.Frequency),
		HourUTC:          body.HourUTC,
		Weekday:          body.Weekday,
		DayOfMonth:       body.DayOfMonth,
		Recipients:       body.Recipients,
	})
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": err.Error()})))
	}
	return c.JSON(http.StatusCreated, rec)
}

// Get — GET /v1/reports/schedules/:id
func (h *SchedulesHandler) Get(c echo.Context) error {
	_, _, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	rec, err := h.schedule.Get(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, rec)
}

// Delete — DELETE /v1/reports/schedules/:id
func (h *SchedulesHandler) Delete(c echo.Context) error {
	_, _, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	if err := h.schedule.Delete(c.Request().Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.NoContent(http.StatusNoContent)
}