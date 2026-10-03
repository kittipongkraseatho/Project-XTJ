// Package phase1http hosts HTTP handlers for Phase 1 endpoints
// (/imports, /trades, /journal, /tags, /analytics, /accounts).
package phase1http

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/auth"
	"github.com/trading-journal/api/internal/domain/trading_account"
	"github.com/trading-journal/api/internal/entitlement"
	"github.com/trading-journal/api/internal/integration/cloudinary"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/service"
	"github.com/trading-journal/api/internal/store"
)

// Handler is the Phase 1 HTTP handler container.
type Handler struct {
	store      *store.Store
	importSvc  *service.ImportService
	cloudinary cloudinary.Service
	log        *slog.Logger
}

func New(st *store.Store, importSvc *service.ImportService, cld cloudinary.Service, log *slog.Logger) *Handler {
	return &Handler{store: st, importSvc: importSvc, cloudinary: cld, log: log}
}

// ── helpers ──────────────────────────────────────────────────────────

func idsFromCtx(c echo.Context) (uuid.UUID, uuid.UUID, bool) {
	uid, ok1 := auth.UserIDFromContext(c.Request().Context())
	tid, err := tenantIDFromEcho(c)
	if !ok1 || err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	return uid, tid, true
}

func tenantIDFromEcho(c echo.Context) (uuid.UUID, error) {
	v, has := c.Get("tenant_id").(string)
	if !has || v == "" {
		return uuid.Nil, observability.ErrUnauthorized
	}
	id, err := uuid.FromString(v)
	if err != nil {
		return uuid.Nil, observability.ErrUnauthorized
	}
	return id, nil
}

func parseDate(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("missing")
	}
	return time.Parse("2006-01-02", s)
}

// ── Imports ───────────────────────────────────────────────────────────

// Upload — POST /v1/imports (multipart)
func (h *Handler) Upload(c echo.Context) error {
	uid, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	accountIDStr := c.FormValue("trading_account_id")
	if accountIDStr == "" {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "missing trading_account_id"})))
	}
	accountID, err := uuid.FromString(accountIDStr)
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid trading_account_id"})))
	}

	fileHeader, err := c.FormFile("file")
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithCause(err).WithDetails(map[string]any{"reason": "missing file"})))
	}
	src, err := fileHeader.Open()
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	defer src.Close()

	buf := make([]byte, fileHeader.Size)
	if _, err := src.Read(buf); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}

	res, err := h.importSvc.Run(c.Request().Context(), service.ImportInput{
		TenantID:         tid,
		UserID:           uid,
		TradingAccountID: accountID,
		OriginalFilename: fileHeader.Filename,
		ByteSize:         fileHeader.Size,
		Content:          buf,
	})
	if err != nil {
		return c.JSON(observability.HTTPStatus(err), observability.FromError(err))
	}
	return c.JSON(http.StatusCreated, res)
}

// ListImports — GET /v1/imports?limit=
func (h *Handler) ListImports(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	limit := 50
	if s := c.QueryParam("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	jobs, err := h.store.ImportJobs.ListByTenant(c.Request().Context(), tid, limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"imports": jobs})
}

// ── Trades ────────────────────────────────────────────────────────────

// ListTrades — GET /v1/trades?account_id=&limit=&offset=
func (h *Handler) ListTrades(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	var accountID *uuid.UUID
	if s := c.QueryParam("account_id"); s != "" {
		id, err := uuid.FromString(s)
		if err != nil {
			return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid account_id"})))
		}
		accountID = &id
	}
	limit, offset := 50, 0
	if s := c.QueryParam("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	if s := c.QueryParam("offset"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 0 {
			offset = n
		}
	}
	trades, err := h.store.Trades.ListByTenant(c.Request().Context(), tid, accountID, limit, offset)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	total, err := h.store.Trades.CountByTenant(c.Request().Context(), tid)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"trades": trades, "total": total})
}

// GetTrade — GET /v1/trades/:id
func (h *Handler) GetTrade(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	t, err := h.store.Trades.GetByID(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	if t.TenantID != tid {
		return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
	}
	return c.JSON(http.StatusOK, t)
}

// ── Analytics ─────────────────────────────────────────────────────────

// OverviewAnalytics — GET /v1/analytics/overview?account_id=
func (h *Handler) OverviewAnalytics(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	var accountID *uuid.UUID
	if s := c.QueryParam("account_id"); s != "" {
		id, err := uuid.FromString(s)
		if err != nil {
			return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid account_id"})))
		}
		accountID = &id
	}
	stats, err := h.store.Trades.AggregateOverview(c.Request().Context(), tid, accountID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, stats)
}

// DailyPL — GET /v1/analytics/daily-pl?from=&to=&account_id=
func (h *Handler) DailyPL(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	from, err := parseDate(c.QueryParam("from"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid from"})))
	}
	to, err := parseDate(c.QueryParam("to"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid to"})))
	}
	if to.Before(from) {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "to < from"})))
	}
	rows, err := h.store.DailyMetrics.ListByTenantRange(c.Request().Context(), tid, from, to.Add(24*time.Hour))
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"daily": rows})
}

// EquityCurve — GET /v1/analytics/equity-curve?account_id=&limit=
func (h *Handler) EquityCurve(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	var accountID *uuid.UUID
	if s := c.QueryParam("account_id"); s != "" {
		id, err := uuid.FromString(s)
		if err != nil {
			return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid account_id"})))
		}
		accountID = &id
	}
	limit := 100
	if v := c.QueryParam("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
	}
	rows, err := h.store.Trades.EquityCurve(c.Request().Context(), tid, accountID, limit)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"equity_curve": rows})
}

// SymbolBreakdown — GET /v1/analytics/symbol-breakdown?account_id=
func (h *Handler) SymbolBreakdown(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	var accountID *uuid.UUID
	if s := c.QueryParam("account_id"); s != "" {
		id, err := uuid.FromString(s)
		if err != nil {
			return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid account_id"})))
		}
		accountID = &id
	}
	rows, err := h.store.Trades.SymbolBreakdown(c.Request().Context(), tid, accountID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"symbols": rows})
}

// SessionBreakdown — GET /v1/analytics/session?account_id=
//
// Aggregates net P/L and win rate by FX trading session (asia / london /
// newyork / sydney). Phase 3 — sessions overlap; buckets are independent.
func (h *Handler) SessionBreakdown(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	accountID, err := parseAccountID(c)
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(err))
	}
	rows, err := h.store.Trades.SessionBreakdown(c.Request().Context(), tid, accountID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"sessions": rows})
}

// EABreakdown — GET /v1/analytics/ea?account_id=
//
// Aggregates by MT5 magic (EA identifier). magic=0 is "Manual trades".
func (h *Handler) EABreakdown(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	accountID, err := parseAccountID(c)
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(err))
	}
	rows, err := h.store.Trades.EABreakdown(c.Request().Context(), tid, accountID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"eas": rows})
}

// DayOfWeekBreakdown — GET /v1/analytics/day-of-week?account_id=
func (h *Handler) DayOfWeekBreakdown(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	accountID, err := parseAccountID(c)
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(err))
	}
	rows, err := h.store.Trades.DayOfWeekBreakdown(c.Request().Context(), tid, accountID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"weekdays": rows})
}

// HourOfDayBreakdown — GET /v1/analytics/hour-of-day?account_id=
func (h *Handler) HourOfDayBreakdown(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	accountID, err := parseAccountID(c)
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(err))
	}
	rows, err := h.store.Trades.HourOfDayBreakdown(c.Request().Context(), tid, accountID)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"hours": rows})
}

// parseAccountID extracts the optional ?account_id= query param. Centralized
// here so the 4 new analytics handlers stay tiny.
func parseAccountID(c echo.Context) (*uuid.UUID, error) {
	s := c.QueryParam("account_id")
	if s == "" {
		return nil, nil
	}
	id, err := uuid.FromString(s)
	if err != nil {
		return nil, observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid account_id"})
	}
	return &id, nil
}

// ── Trading accounts ──────────────────────────────────────────────────

// ListAccounts — GET /v1/accounts
func (h *Handler) ListAccounts(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	accounts, err := h.store.TradingAccounts.ListByTenant(c.Request().Context(), tid)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusOK, map[string]any{"accounts": accounts})
}

// CreateAccount — POST /v1/accounts
//
// Phase 3: enforces the tenant's plan limit (max-trading-accounts) before
// creating. New tenants on 'free' get 1 account; 'pro' gets 5. The lookup
// is best-effort — an unparseable plan defaults to free limits (least privilege).
func (h *Handler) CreateAccount(c echo.Context) error {
	uid, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	var body struct {
		Broker          string   `json:"broker"`
		BrokerAccountID string   `json:"broker_account_id"`
		DisplayName     string   `json:"display_name"`
		Currency        string   `json:"currency"`
		Leverage        *float64 `json:"leverage"`
		Kind            string   `json:"kind"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	if body.Broker == "" || body.BrokerAccountID == "" || body.DisplayName == "" {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "missing required field"})))
	}
	cur := body.Currency
	if cur == "" {
		cur = "USD"
	}
	kind := body.Kind
	if kind == "" {
		kind = "real"
	}

	// Plan limit check (Phase 3 §2.2)
	code, limits := entitlement.LookupForTenant(c.Request().Context(), h.store, tid)
	current, err := h.store.TradingAccounts.CountByTenant(c.Request().Context(), tid)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	if current >= int64(limits.MaxTradingAccounts) {
		return c.JSON(http.StatusPaymentRequired, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{
				"reason":               "plan_limit_reached",
				"resource":             "trading_accounts",
				"limit":                limits.MaxTradingAccounts,
				"current":              current,
				"plan":                 string(code),
				"upgrade_required":     true,
			})))
	}

	a := &trading_account.TradingAccount{
		ID:              uuid.Must(uuid.NewV7()),
		UserID:          uid,
		TenantID:        tid,
		Broker:          trading_account.Broker(body.Broker),
		BrokerAccountID: body.BrokerAccountID,
		DisplayName:     body.DisplayName,
		Currency:        cur,
		Leverage:        body.Leverage,
		Kind:            trading_account.Kind(kind),
		Metadata:        map[string]any{},
		IsActive:        true,
	}
	if err := h.store.TradingAccounts.Create(c.Request().Context(), a); err != nil {
		if store.IsUniqueViolation(err) {
			return c.JSON(http.StatusConflict, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "duplicate broker_account_id"})))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	return c.JSON(http.StatusCreated, a)
}