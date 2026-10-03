// Package phase2http hosts HTTP handlers for Phase 2 endpoints
// (/v1/brokers/connections + nested sync-runs).
//
// Phase 2 implements the MetaApi-style broker sync architecture with a MockProvider
// in place of the real SDK. The HTTP contract is broker-agnostic; a Phase 4
// MetaApi adapter plugs in without touching these handlers.
package phase2http

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/auth"
	"github.com/trading-journal/api/internal/domain/broker_connection"
	"github.com/trading-journal/api/internal/domain/sync_run"
	"github.com/trading-journal/api/internal/integration/broker"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/store"
	"github.com/trading-journal/api/internal/syncsvc"
	"github.com/trading-journal/api/internal/worker"
)

// Handler is the Phase 2 HTTP handler container.
type Handler struct {
	store    *store.Store
	syncSvc  *syncsvc.SyncService
	syncWk   *worker.SyncWorker
	registry *broker.Registry
	log      *slog.Logger
}

// New wires the dependencies. registry MUST contain at least the mock provider
// for the demo flow to work.
func New(st *store.Store, svc *syncsvc.SyncService, wk *worker.SyncWorker, reg *broker.Registry, log *slog.Logger) *Handler {
	return &Handler{store: st, syncSvc: svc, syncWk: wk, registry: reg, log: log}
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

// ── Connections ──────────────────────────────────────────────────────

// CreateConnection — POST /v1/brokers/connections
//
// Body: {trading_account_id, provider, login, password, server, sync_interval_seconds?}
// 201 → {id, status:'connected', ...} on success; 400 on unknown provider; 403 on cross-tenant.
func (h *Handler) CreateConnection(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	var body struct {
		TradingAccountID    string `json:"trading_account_id"`
		Provider            string `json:"provider"`
		Login               string `json:"login"`
		Password            string `json:"password"`
		Server              string `json:"server"`
		SyncIntervalSeconds int    `json:"sync_interval_seconds"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	if body.TradingAccountID == "" || body.Provider == "" {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "missing required field"})))
	}
	accountID, err := uuid.FromString(body.TradingAccountID)
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid trading_account_id"})))
	}

	prov := broker_connection.Provider(body.Provider)
	if !prov.IsValid() {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "unknown provider"})))
	}
	if !h.registry.Has(prov) {
		return c.JSON(http.StatusBadRequest, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "provider not configured"})))
	}

	conn, err := h.syncSvc.CreateAndConnect(c.Request().Context(), syncsvc.CreateConnectionInput{
		TenantID:            tid,
		TradingAccountID:    accountID,
		Provider:            prov,
		Login:               body.Login,
		Password:            body.Password,
		Server:              body.Server,
		SyncIntervalSeconds: body.SyncIntervalSeconds,
	})
	if err != nil {
		return c.JSON(observability.HTTPStatus(err), observability.FromError(err))
	}
	return c.JSON(http.StatusCreated, conn)
}

// ListConnections — GET /v1/brokers/connections
func (h *Handler) ListConnections(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	conns, err := h.store.BrokerConnections.ListByTenant(c.Request().Context(), tid, 100)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"connections": conns})
}

// GetConnection — GET /v1/brokers/connections/:id
func (h *Handler) GetConnection(c echo.Context) error {
	_, _, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	conn, err := h.store.BrokerConnections.GetByIDByTenant(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, conn)
}

// Disconnect — POST /v1/brokers/connections/:id/disconnect
//
// Sets status='disconnected', zeroes external_account_id. Idempotent.
func (h *Handler) Disconnect(c echo.Context) error {
	_, _, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	conn, err := h.store.BrokerConnections.GetByIDByTenant(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	zero := ""
	if err := h.store.BrokerConnections.UpdateStatus(c.Request().Context(), conn.ID, broker_connection.StatusDisconnected, &zero); err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"id": conn.ID, "status": broker_connection.StatusDisconnected})
}

// DeleteConnection — DELETE /v1/brokers/connections/:id
func (h *Handler) DeleteConnection(c echo.Context) error {
	_, _, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	// Verify tenant ownership first (defense in depth).
	if _, err := h.store.BrokerConnections.GetByIDByTenant(c.Request().Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	if err := h.store.BrokerConnections.Delete(c.Request().Context(), id); err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.NoContent(http.StatusNoContent)
}

// ── Sync ─────────────────────────────────────────────────────────────

// TriggerSync — POST /v1/brokers/connections/:id/sync
//
// Enqueues a sync_run immediately. Returns 202 with run_id.
func (h *Handler) TriggerSync(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}

	var body struct {
		Trigger string `json:"trigger"` // default "manual"
	}
	_ = c.Bind(&body)
	trigger := sync_run.TriggerManual
	if body.Trigger != "" {
		t := sync_run.Trigger(body.Trigger)
		if t.IsValid() {
			trigger = t
		}
	}

	run, err := h.syncWk.EnqueueManual(c.Request().Context(), tid, id, trigger)
	if err != nil {
		return c.JSON(observability.HTTPStatus(err), observability.FromError(err))
	}
	return c.JSON(http.StatusAccepted, map[string]any{
		"sync_run_id": run.ID,
		"status":      run.Status,
		"trigger":     run.Trigger,
	})
}

// ListSyncRuns — GET /v1/brokers/connections/:id/sync-runs
func (h *Handler) ListSyncRuns(c echo.Context) error {
	_, _, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	// Defense in depth: ensure the connection is owned by this tenant.
	if _, err := h.store.BrokerConnections.GetByIDByTenant(c.Request().Context(), id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	limit := 50
	if s := c.QueryParam("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	runs, err := h.store.SyncRuns.ListByConnection(c.Request().Context(), id, limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"sync_runs": runs})
}

// GetSyncRun — GET /v1/brokers/sync-runs/:id
func (h *Handler) GetSyncRun(c echo.Context) error {
	_, _, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	run, err := h.store.SyncRuns.GetByIDByTenant(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, run)
}