// Package authhttp hosts HTTP handlers for /v1/auth/*.
package authhttp

import (
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/auth"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/service"
)

type Handler struct {
	svc *service.AuthService
	log *slog.Logger
}

func New(svc *service.AuthService, log *slog.Logger) *Handler {
	return &Handler{svc: svc, log: log}
}

func (h *Handler) Register(c echo.Context) error {
	var body struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"display_name"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	res, err := h.svc.Register(c.Request().Context(), service.RegisterInput{
		Email:       body.Email,
		Password:    body.Password,
		DisplayName: body.DisplayName,
	}, buildCallCtx(c))
	if err != nil {
		return c.JSON(observability.HTTPStatus(err), observability.FromError(err))
	}
	return c.JSON(http.StatusCreated, res)
}

func (h *Handler) Login(c echo.Context) error {
	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	res, err := h.svc.Login(c.Request().Context(), service.LoginInput{
		Email: body.Email, Password: body.Password,
	}, buildCallCtx(c))
	if err != nil {
		return c.JSON(observability.HTTPStatus(err), observability.FromError(err))
	}
	return c.JSON(http.StatusOK, res)
}

func (h *Handler) Refresh(c echo.Context) error {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	res, err := h.svc.Refresh(c.Request().Context(), service.RefreshInput{
		RefreshToken: body.RefreshToken,
	}, buildCallCtx(c))
	if err != nil {
		return c.JSON(observability.HTTPStatus(err), observability.FromError(err))
	}
	return c.JSON(http.StatusOK, res)
}

func (h *Handler) Logout(c echo.Context) error {
	sessionID, userID, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	if err := h.svc.Logout(c.Request().Context(), service.LogoutInput{
		SessionID: sessionID, UserID: userID,
	}, buildCallCtx(c)); err != nil {
		return c.JSON(observability.HTTPStatus(err), observability.FromError(err))
	}
	return c.NoContent(http.StatusNoContent)
}

// idsFromCtx pulls user_id and session_id from JWT-populated echo context.
func idsFromCtx(c echo.Context) (sessionID, userID uuid.UUID, ok bool) {
	uid, ok1 := auth.UserIDFromContext(c.Request().Context())
	sid, ok2 := auth.SessionIDFromContext(c.Request().Context())
	if !ok1 || !ok2 {
		return uuid.Nil, uuid.Nil, false
	}
	return sid, uid, true
}

func buildCallCtx(c echo.Context) service.CallCtx {
	return service.CallCtx{
		RequestID: observability.RequestIDFromContext(c.Request().Context()),
		UserAgent: c.Request().Header.Get("User-Agent"),
		IP:        parseIP(c.RealIP()),
	}
}

func parseIP(s string) *netip.Addr {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return nil
	}
	return &addr
}