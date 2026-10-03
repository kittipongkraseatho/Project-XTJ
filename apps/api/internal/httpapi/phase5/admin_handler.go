// Package phase5http — admin endpoints for API-key lifecycle management.
//
// These endpoints live under /v1/api-keys and are JWT-authed (the user
// must own the tenant). They are the same shape as the Phase 4 schedules
// admin routes — same handler pattern, same error envelope.
//
//	GET    /v1/api-keys             — list tenant's keys (prefix only, never secret)
//	POST   /v1/api-keys             — generate a new key (returns plaintext ONCE)
//	GET    /v1/api-keys/:id         — fetch one
//	PATCH  /v1/api-keys/:id         — update name / scopes / expires_at
//	DELETE /v1/api-keys/:id         — revoke (soft-delete)
package phase5http

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/auth"
	"github.com/trading-journal/api/internal/domain/api_key"
	"github.com/trading-journal/api/internal/integration/apikey"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/tenant"
)

// AdminHandler owns the API-key admin routes. It's separate from the
// public handler so we don't accidentally expose admin funcs to API-key
// callers.
type AdminHandler struct {
	svc apikey.Service
	log *slog.Logger
}

// NewAdminHandler wires the admin handler.
func NewAdminHandler(svc apikey.Service, log *slog.Logger) *AdminHandler {
	return &AdminHandler{svc: svc, log: log}
}

// generateReq is the JSON body for POST /v1/api-keys.
type generateReq struct {
	Name      string   `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresIn string   `json:"expires_in"` // "30d" / "90d" / "1y" / "never"
}

// Generate — POST /v1/api-keys
//
// Returns 201 with {key, plaintext}. The plaintext is shown ONCE — the FE
// is responsible for showing it to the user and discarding.
func (h *AdminHandler) Generate(c echo.Context) error {
	tid, err := tenant.TenantIDFromContext(c.Request().Context())
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
	if body.Name == "" {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "name required"})))
	}
	if len(body.Scopes) == 0 {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "at least one scope required"})))
	}
	// Validate each scope.
	for _, s := range body.Scopes {
		if !api_key.Scope(s).IsValid() {
			return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid scope: " + s})))
		}
	}
	// Parse expires_in (Phase 5 only accepts a small set).
	var expiresPtr *string
	if body.ExpiresIn != "" && body.ExpiresIn != "never" {
		d, err := parseExpiry(body.ExpiresIn)
		if err != nil {
			return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid expires_in"})))
		}
		s := d.UTC().Format(time.RFC3339)
		expiresPtr = &s
	}
	res, err := h.svc.Generate(c.Request().Context(), apikey.GenerateInput{
		TenantID:        tid,
		CreatedByUserID: uid,
		Name:            body.Name,
		Scopes:          toScopeList(body.Scopes),
		ExpiresAt:       expiresPtr,
	})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusCreated, res)
}

// List — GET /v1/api-keys
func (h *AdminHandler) List(c echo.Context) error {
	tid, err := tenant.TenantIDFromContext(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	keys, err := h.svc.List(c.Request().Context(), tid)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"keys": keys})
}

// Get — GET /v1/api-keys/:id
func (h *AdminHandler) Get(c echo.Context) error {
	tid, err := tenant.TenantIDFromContext(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	k, err := h.svc.Get(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	// Tenant guard.
	if k.TenantID != tid {
		return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrValidation.WithDetails(
			map[string]any{"reason": "not found"})))
	}
	return c.JSON(http.StatusOK, map[string]any{"key": k})
}

// Revoke — DELETE /v1/api-keys/:id
func (h *AdminHandler) Revoke(c echo.Context) error {
	tid, err := tenant.TenantIDFromContext(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	// Tenant guard via Get first.
	k, err := h.svc.Get(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	if k.TenantID != tid {
		return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrValidation.WithDetails(
			map[string]any{"reason": "not found"})))
	}
	if err := h.svc.Revoke(c.Request().Context(), id); err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.NoContent(http.StatusNoContent)
}

// updateReq is the JSON body for PATCH /v1/api-keys/:id.
type updateReq struct {
	Name      *string  `json:"name"`
	Scopes    []string `json:"scopes"`
	ExpiresIn *string  `json:"expires_in"`
}

// Update — PATCH /v1/api-keys/:id
func (h *AdminHandler) Update(c echo.Context) error {
	tid, err := tenant.TenantIDFromContext(c.Request().Context())
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
	k, err := h.svc.Get(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	if k.TenantID != tid {
		return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrValidation.WithDetails(
			map[string]any{"reason": "not found"})))
	}
	if body.Name != nil {
		k.Name = *body.Name
	}
	if body.Scopes != nil {
		for _, s := range body.Scopes {
			if !api_key.Scope(s).IsValid() {
				return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
					observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid scope: " + s})))
			}
		}
		k.Scopes = body.Scopes
	}
	if body.ExpiresIn != nil {
		if *body.ExpiresIn == "never" || *body.ExpiresIn == "" {
			k.ExpiresAt = nil
		} else {
			d, err := parseExpiry(*body.ExpiresIn)
			if err != nil {
				return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
					observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid expires_in"})))
			}
			k.ExpiresAt = &d
		}
	}
	if err := h.svc.Update(c.Request().Context(), k); err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"key": k})
}

// parseExpiry parses "30d" / "90d" / "1y" into a UTC time in the future.
// Phase 5 only supports these three values; arbitrary durations deferred.
func parseExpiry(s string) (time.Time, error) {
	now := time.Now().UTC()
	switch s {
	case "30d":
		return now.AddDate(0, 0, 30), nil
	case "90d":
		return now.AddDate(0, 0, 90), nil
	case "1y":
		return now.AddDate(1, 0, 0), nil
	default:
		return time.Time{}, errors.New("unsupported expires_in")
	}
}

func toScopeList(in []string) []api_key.Scope {
	out := make([]api_key.Scope, 0, len(in))
	for _, s := range in {
		out = append(out, api_key.Scope(s))
	}
	return out
}