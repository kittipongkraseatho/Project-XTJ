// Package phase9http — Phase 9 AI Expansion endpoints (weekly recap + Q&A chat).
//
//	POST   /v1/accounts/:id/recaps              — kick off a recap for an account
//	GET    /v1/accounts/:id/recaps              — list recaps for the account
//	GET    /v1/recaps/:id                       — get one recap (with content)
//	GET    /v1/chat/threads                     — list user's threads
//	POST   /v1/chat/threads                     — find-or-create thread
//	DELETE /v1/chat/threads/:id                 — delete a thread
//	GET    /v1/chat/threads/:id/messages        — list messages (paginated)
//	POST   /v1/chat/threads/:id/messages        — send a user message (kicks reply)
package phase9http

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/auth"
	"github.com/trading-journal/api/internal/domain/ai"
	intai "github.com/trading-journal/api/internal/integration/ai"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/store"
	"github.com/trading-journal/api/internal/tenant"
)

// Handler owns all Phase 9 AI Expansion endpoints.
type Handler struct {
	recapSvc intai.RecapService
	chatSvc  intai.ChatService
	store    *store.Store
	log      *slog.Logger
}

// New builds the handler.
func New(recapSvc intai.RecapService, chatSvc intai.ChatService, st *store.Store, log *slog.Logger) *Handler {
	return &Handler{recapSvc: recapSvc, chatSvc: chatSvc, store: st, log: log}
}

// RegisterRoutes mounts all Phase 9 endpoints on the given authed group.
func (h *Handler) RegisterRoutes(authed *echo.Group) {
	authed.POST("/accounts/:id/recaps", h.CreateRecap)
	authed.GET("/accounts/:id/recaps", h.ListRecaps)
	authed.GET("/recaps/:id", h.GetRecap)
	authed.GET("/chat/threads", h.ListThreads)
	authed.POST("/chat/threads", h.CreateThread)
	authed.DELETE("/chat/threads/:id", h.DeleteThread)
	authed.GET("/chat/threads/:id/messages", h.ListMessages)
	authed.POST("/chat/threads/:id/messages", h.SendMessage)
}

// ── Recaps ───────────────────────────────────────────────────────────

// CreateRecap — POST /v1/accounts/:id/recaps
// Body: { "week_start": "2026-09-28" } (optional; defaults to last completed week)
func (h *Handler) CreateRecap(c echo.Context) error {
	tid, uid, ok := h.ctx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	accountID, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid account id"})))
	}
	var body struct {
		WeekStart string `json:"week_start"` // YYYY-MM-DD (Monday UTC); optional
	}
	_ = c.Bind(&body)

	in := intai.RecapInput{
		TenantID: tid, AccountID: accountID, UserID: uid,
		PlanCode: h.lookupPlanCode(c.Request().Context(), tid),
	}
	if body.WeekStart != "" {
		t, err := time.Parse("2006-01-02", body.WeekStart)
		if err != nil {
			return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid week_start format (YYYY-MM-DD)"})))
		}
		in.WeekStart = t.UTC()
	}
	r, err := h.recapSvc.RecapAccount(c.Request().Context(), in)
	if err != nil {
		switch {
		case errors.Is(err, intai.ErrBudgetExhausted):
			return c.JSON(http.StatusPaymentRequired, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "daily_budget_exhausted"})))
		default:
			return c.JSON(http.StatusInternalServerError, observability.FromError(err))
		}
	}
	if r.Status == ai.StatusSuccess {
		return c.JSON(http.StatusOK, r)
	}
	return c.JSON(http.StatusAccepted, r)
}

// ListRecaps — GET /v1/accounts/:id/recaps
func (h *Handler) ListRecaps(c echo.Context) error {
	tid, _, ok := h.ctx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	accountID, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid account id"})))
	}
	rs, err := h.recapSvc.ListRecapsForAccount(c.Request().Context(), tid, accountID, 20)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, rs)
}

// GetRecap — GET /v1/recaps/:id
func (h *Handler) GetRecap(c echo.Context) error {
	tid, _, ok := h.ctx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid recap id"})))
	}
	r, err := h.recapSvc.GetRecap(c.Request().Context(), tid, id)
	if err != nil {
		if errors.Is(err, intai.ErrRecapNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "recap not found"})))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, r)
}

// ── Chat threads ─────────────────────────────────────────────────────

// CreateThread — POST /v1/chat/threads { account_id?: uuid }
func (h *Handler) CreateThread(c echo.Context) error {
	tid, uid, ok := h.ctx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	var body struct {
		AccountID *string `json:"account_id"`
		Title     string  `json:"title"`
	}
	_ = c.Bind(&body)
	in := intai.ChatCreateInput{TenantID: tid, UserID: uid, Title: body.Title}
	if body.AccountID != nil && *body.AccountID != "" {
		acc, err := uuid.FromString(*body.AccountID)
		if err != nil {
			return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid account_id"})))
		}
		in.AccountID = &acc
	}
	t, err := h.chatSvc.CreateThread(c.Request().Context(), in)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusCreated, t)
}

// ListThreads — GET /v1/chat/threads
func (h *Handler) ListThreads(c echo.Context) error {
	tid, uid, ok := h.ctx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	ts, err := h.chatSvc.ListThreads(c.Request().Context(), tid, uid, 50)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, ts)
}

// DeleteThread — DELETE /v1/chat/threads/:id
func (h *Handler) DeleteThread(c echo.Context) error {
	tid, _, ok := h.ctx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid thread id"})))
	}
	if err := h.chatSvc.DeleteThread(c.Request().Context(), tid, id); err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.NoContent(http.StatusNoContent)
}

// ── Chat messages ────────────────────────────────────────────────────

// ListMessages — GET /v1/chat/threads/:id/messages
func (h *Handler) ListMessages(c echo.Context) error {
	tid, _, ok := h.ctx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid thread id"})))
	}
	ms, err := h.chatSvc.ListMessages(c.Request().Context(), tid, id, 50)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, ms)
}

// SendMessage — POST /v1/chat/threads/:id/messages { content: "..." }
func (h *Handler) SendMessage(c echo.Context) error {
	tid, uid, ok := h.ctx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid thread id"})))
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid body"})))
	}
	m, err := h.chatSvc.SendMessage(c.Request().Context(), intai.ChatSendInput{
		TenantID: tid, UserID: uid, ThreadID: id,
		Content: body.Content, PlanCode: h.lookupPlanCode(c.Request().Context(), tid),
	})
	if err != nil {
		switch {
		case errors.Is(err, intai.ErrBudgetExhausted):
			return c.JSON(http.StatusPaymentRequired, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "daily_budget_exhausted"})))
		case errors.Is(err, intai.ErrChatThreadNotFound):
			return c.JSON(http.StatusNotFound, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "thread not found"})))
		default:
			return c.JSON(http.StatusInternalServerError, observability.FromError(err))
		}
	}
	return c.JSON(http.StatusAccepted, m)
}

// ── helpers ──────────────────────────────────────────────────────────

func (h *Handler) ctx(c echo.Context) (tid, uid uuid.UUID, yes bool) {
	t, err := tenant.TenantIDFromContext(c.Request().Context())
	if err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	u, ok := auth.UserIDFromContext(c.Request().Context())
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	return t, u, true
}

// lookupPlanCode returns the tenant's plan code. Phase 9 inherits Phase 6's
// minimal lookup (always returns "" → free budget). Production hardening can
// wire the entitlement package + subscriptions table.
func (h *Handler) lookupPlanCode(_ interface{}, _ uuid.UUID) string {
	return ""
}