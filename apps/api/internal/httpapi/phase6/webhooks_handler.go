// Package phase6http — Phase 5.2 webhook admin handlers.
//
// All routes are JWT-authed. API-key auth for `write:webhooks` is
// deferred (Phase 5.2 admin endpoints are owner-only).
//
// Endpoints:
//
//	GET    /v1/webhooks                 — list tenant's
//	POST   /v1/webhooks                 — create (plaintext secret ONCE)
//	GET    /v1/webhooks/:id             — fetch one
//	PATCH  /v1/webhooks/:id             — update url / event_types / description / is_active
//	DELETE /v1/webhooks/:id             — revoke (soft-delete)
//	GET    /v1/webhooks/:id/deliveries  — recent deliveries (drill-down)
package phase6http

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/auth"
	"github.com/trading-journal/api/internal/domain/webhook"
	whsvc "github.com/trading-journal/api/internal/integration/webhook"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/store"
	"github.com/trading-journal/api/internal/tenant"
)

// WebhookAdminHandler owns the /v1/webhooks/* admin routes.
type WebhookAdminHandler struct {
	svc   whsvc.Service
	store *store.Store // for the deliveries drill-down
	log   *slog.Logger
}

func NewWebhookAdminHandler(svc whsvc.Service, st *store.Store, log *slog.Logger) *WebhookAdminHandler {
	return &WebhookAdminHandler{svc: svc, store: st, log: log}
}

// ── helpers ─────────────────────────────────────────────────────────

func idsFromCtx(c echo.Context) (uuid.UUID, error) {
	tid, err := tenant.TenantIDFromContext(c.Request().Context())
	if err != nil {
		return uuid.Nil, err
	}
	return tid, nil
}

// generateReq is the body of POST /v1/webhooks.
type generateReq struct {
	URL         string             `json:"url"`
	EventTypes  []webhook.EventType `json:"event_types"`
	Description string             `json:"description"`
}

// Create — POST /v1/webhooks
func (h *WebhookAdminHandler) Create(c echo.Context) error {
	tid, err := idsFromCtx(c)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	uid, ok := auth.UserIDFromContext(c.Request().Context())
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	var body generateReq
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	res, err := h.svc.Create(c.Request().Context(), whsvc.CreateInput{
		TenantID:        tid,
		CreatedByUserID: uid,
		URL:             body.URL,
		EventTypes:      body.EventTypes,
		Description:     body.Description,
	})
	if err != nil {
		return mapServiceErr(c, err)
	}
	return c.JSON(http.StatusCreated, res)
}

// List — GET /v1/webhooks
func (h *WebhookAdminHandler) List(c echo.Context) error {
	tid, err := idsFromCtx(c)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	subs, err := h.svc.List(c.Request().Context(), tid)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"webhooks": subs})
}

// Get — GET /v1/webhooks/:id
func (h *WebhookAdminHandler) Get(c echo.Context) error {
	tid, err := idsFromCtx(c)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	sub, err := h.svc.Get(c.Request().Context(), tid, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "not found"})))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"webhook": sub})
}

// updateReq is the body of PATCH /v1/webhooks/:id.
type updateReq struct {
	URL         *string             `json:"url"`
	EventTypes  []webhook.EventType `json:"event_types"`
	Description *string             `json:"description"`
	IsActive    *bool               `json:"is_active"`
}

// Update — PATCH /v1/webhooks/:id
func (h *WebhookAdminHandler) Update(c echo.Context) error {
	tid, err := idsFromCtx(c)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	var body updateReq
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	sub, err := h.svc.Get(c.Request().Context(), tid, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "not found"})))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	if body.URL != nil {
		sub.URL = *body.URL
	}
	if body.EventTypes != nil {
		for _, e := range body.EventTypes {
			if !e.IsValid() {
				return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
					observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid event type: " + string(e)})))
			}
		}
		converted := make([]webhook.EventType, len(body.EventTypes))
		for i, e := range body.EventTypes {
			converted[i] = webhook.EventType(e)
		}
		sub.EventTypes = converted
	}
	if body.Description != nil {
		sub.Description = *body.Description
	}
	if body.IsActive != nil {
		sub.IsActive = *body.IsActive
	}
	sub.UpdatedAt = time.Now().UTC()
	if err := h.svc.Update(c.Request().Context(), sub); err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"webhook": sub})
}

// Revoke — DELETE /v1/webhooks/:id
func (h *WebhookAdminHandler) Revoke(c echo.Context) error {
	tid, err := idsFromCtx(c)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	// Tenant guard via Get first.
	if _, err := h.svc.Get(c.Request().Context(), tid, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "not found"})))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	if err := h.svc.Revoke(c.Request().Context(), tid, id); err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.NoContent(http.StatusNoContent)
}

// Deliveries — GET /v1/webhooks/:id/deliveries
//
// Returns the last 50 deliveries for the subscription, ordered by
// created_at DESC.
func (h *WebhookAdminHandler) Deliveries(c echo.Context) error {
	tid, err := idsFromCtx(c)
	if err != nil {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	if _, err := h.svc.Get(c.Request().Context(), tid, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "not found"})))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	d, err := h.store.WebhookDeliveries.ListBySubscription(c.Request().Context(), id, 50)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"deliveries": d})
}

// ── service-error mapping ───────────────────────────────────────────

func mapServiceErr(c echo.Context, err error) error {
	switch {
	case errors.Is(err, whsvc.ErrInvalidURL):
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid url"})))
	case errors.Is(err, whsvc.ErrEmptyEvents):
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "at least one event type required"})))
	case errors.Is(err, whsvc.ErrInvalidEvent):
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": err.Error()})))
	default:
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
}