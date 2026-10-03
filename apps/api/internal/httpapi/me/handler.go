// Package mehttp hosts the /v1/me handler.
package mehttp

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/auth"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/service"
	"github.com/trading-journal/api/internal/tenant"
)

type Handler struct {
	svc *service.AuthService
	log *slog.Logger
}

func New(svc *service.AuthService, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

func (h *Handler) Me(c echo.Context) error {
	uid, ok1 := auth.UserIDFromContext(c.Request().Context())
	tid, err := tenant.TenantIDFromContext(c.Request().Context())
	role := roleFromEcho(c)
	if !ok1 || err != nil {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	res, err := h.svc.Me(c.Request().Context(), service.MeInput{
		UserID: uid, TenantID: tid, Role: role,
	})
	if err != nil {
		return c.JSON(observability.HTTPStatus(err), observability.FromError(err))
	}
	return c.JSON(http.StatusOK, res)
}

func roleFromEcho(c echo.Context) string {
	if v, ok := c.Get("role").(string); ok {
		return v
	}
	return ""
}