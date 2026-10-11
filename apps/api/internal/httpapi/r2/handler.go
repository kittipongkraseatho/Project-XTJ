// Package r2http exposes the internal JWT-authenticated R2 product routes.
// These routes are intentionally separate from the API-key public surface.
package r2http

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/auth"
	"github.com/trading-journal/api/internal/domain/strategy"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/rbac"
	behaviorservice "github.com/trading-journal/api/internal/service/behavior"
	strategyservice "github.com/trading-journal/api/internal/service/strategy"
	"github.com/trading-journal/api/internal/store"
	"github.com/trading-journal/api/internal/tenant"
)

type Handler struct {
	behavior   *behaviorservice.Service
	strategies *strategyservice.Service
	log        *slog.Logger
}

func New(behaviorSvc *behaviorservice.Service, strategySvc *strategyservice.Service, log *slog.Logger) *Handler {
	return &Handler{behavior: behaviorSvc, strategies: strategySvc, log: log}
}

func (h *Handler) RegisterRoutes(authed *echo.Group) {
	authed.GET("/accounts/:account_id/behavior-profile", h.GetBehaviorProfile)
	authed.POST("/accounts/:account_id/behavior-profile", h.CalculateBehaviorProfile, rbac.RequireRole(rbac.RoleMember))
	authed.GET("/strategies", h.ListStrategies)
	authed.POST("/strategies", h.CreateStrategy, rbac.RequireRole(rbac.RoleMember))
	authed.GET("/strategies/:id/versions", h.ListStrategyVersions)
	authed.POST("/strategies/:id/versions", h.CreateStrategyVersion, rbac.RequireRole(rbac.RoleMember))
	authed.PUT("/trades/:id/strategy-version", h.LinkTradeStrategyVersion, rbac.RequireRole(rbac.RoleMember))
}

func (h *Handler) CalculateBehaviorProfile(c echo.Context) error {
	tid, ok := tenantFromContext(c)
	if !ok {
		return h.error(c, http.StatusUnauthorized, observability.ErrUnauthorized)
	}
	accountID, err := parseParamUUID(c, "account_id")
	if err != nil {
		return h.validation(c, "invalid account_id")
	}
	var body behaviorProfileRequest
	if err := c.Bind(&body); err != nil {
		return h.validation(c, "invalid body")
	}
	from, to, err := parseWindow(body.From, body.To)
	if err != nil {
		return h.validation(c, err.Error())
	}
	uid, ok := auth.UserIDFromContext(c.Request().Context())
	if !ok {
		return h.error(c, http.StatusUnauthorized, observability.ErrUnauthorized)
	}
	profile, err := h.behavior.AnalyzeAs(c.Request().Context(), tid, &uid, accountID, from, to)
	if err != nil {
		return h.mapServiceError(c, err)
	}
	return c.JSON(http.StatusOK, profileDTO(profile))
}

func (h *Handler) GetBehaviorProfile(c echo.Context) error {
	tid, ok := tenantFromContext(c)
	if !ok {
		return h.error(c, http.StatusUnauthorized, observability.ErrUnauthorized)
	}
	accountID, err := parseParamUUID(c, "account_id")
	if err != nil {
		return h.validation(c, "invalid account_id")
	}
	profile, err := h.behavior.Latest(c.Request().Context(), tid, accountID)
	if err != nil {
		return h.mapServiceError(c, err)
	}
	return c.JSON(http.StatusOK, profileDTO(profile))
}

func (h *Handler) ListStrategies(c echo.Context) error {
	tid, ok := tenantFromContext(c)
	if !ok {
		return h.error(c, http.StatusUnauthorized, observability.ErrUnauthorized)
	}
	items, err := h.strategies.List(c.Request().Context(), tid)
	if err != nil {
		return h.mapServiceError(c, err)
	}
	out := make([]strategyResponse, 0, len(items))
	for _, item := range items {
		out = append(out, strategyDTO(item))
	}
	return c.JSON(http.StatusOK, out)
}

func (h *Handler) CreateStrategy(c echo.Context) error {
	tid, uid, ok := idsFromContext(c)
	if !ok {
		return h.error(c, http.StatusUnauthorized, observability.ErrUnauthorized)
	}
	var body strategyCreateRequest
	if err := c.Bind(&body); err != nil {
		return h.validation(c, "invalid body")
	}
	item, err := h.strategies.Create(c.Request().Context(), strategyservice.CreateInput{
		TenantID: tid, OwnerUserID: uid, Name: body.Name, Description: body.Description,
	})
	if err != nil {
		return h.mapServiceError(c, err)
	}
	return c.JSON(http.StatusCreated, strategyDTO(item))
}

func (h *Handler) ListStrategyVersions(c echo.Context) error {
	tid, ok := tenantFromContext(c)
	if !ok {
		return h.error(c, http.StatusUnauthorized, observability.ErrUnauthorized)
	}
	strategyID, err := parseParamUUID(c, "id")
	if err != nil {
		return h.validation(c, "invalid strategy id")
	}
	items, err := h.strategies.ListVersions(c.Request().Context(), tid, strategyID)
	if err != nil {
		return h.mapServiceError(c, err)
	}
	out := make([]strategyVersionResponse, 0, len(items))
	for _, item := range items {
		out = append(out, versionDTO(item))
	}
	return c.JSON(http.StatusOK, out)
}

func (h *Handler) CreateStrategyVersion(c echo.Context) error {
	tid, uid, ok := idsFromContext(c)
	if !ok {
		return h.error(c, http.StatusUnauthorized, observability.ErrUnauthorized)
	}
	strategyID, err := parseParamUUID(c, "id")
	if err != nil {
		return h.validation(c, "invalid strategy id")
	}
	var body strategyVersionRequest
	if err := c.Bind(&body); err != nil {
		return h.validation(c, "invalid body")
	}
	rules, err := strategy.ValidateRuleSet(body.Rules)
	if err != nil {
		return h.mapServiceError(c, err)
	}
	version, err := h.strategies.CreateVersion(c.Request().Context(), tid, strategyservice.CreateVersionInput{
		StrategyID: strategyID, CreatedBy: uid, Rules: rules,
	})
	if err != nil {
		return h.mapServiceError(c, err)
	}
	return c.JSON(http.StatusCreated, versionDTO(version))
}

func (h *Handler) LinkTradeStrategyVersion(c echo.Context) error {
	tid, ok := tenantFromContext(c)
	if !ok {
		return h.error(c, http.StatusUnauthorized, observability.ErrUnauthorized)
	}
	tradeID, err := parseParamUUID(c, "id")
	if err != nil {
		return h.validation(c, "invalid trade id")
	}
	var body strategyLinkRequest
	if err := c.Bind(&body); err != nil {
		return h.validation(c, "invalid body")
	}
	versionID, err := uuid.FromString(body.StrategyVersionID)
	if err != nil {
		return h.validation(c, "invalid strategy_version_id")
	}
	uid, ok := auth.UserIDFromContext(c.Request().Context())
	if !ok {
		return h.error(c, http.StatusUnauthorized, observability.ErrUnauthorized)
	}
	if err := h.strategies.LinkTradeAs(c.Request().Context(), tid, &uid, tradeID, versionID); err != nil {
		return h.mapServiceError(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}

func parseWindow(fromRaw, toRaw string) (time.Time, time.Time, error) {
	if fromRaw == "" || toRaw == "" {
		return time.Time{}, time.Time{}, errors.New("from and to are required RFC3339 timestamps")
	}
	from, err := time.Parse(time.RFC3339, fromRaw)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("from must be an RFC3339 timestamp")
	}
	to, err := time.Parse(time.RFC3339, toRaw)
	if err != nil {
		return time.Time{}, time.Time{}, errors.New("to must be an RFC3339 timestamp")
	}
	if !to.After(from) {
		return time.Time{}, time.Time{}, errors.New("to must be after from")
	}
	return from.UTC(), to.UTC(), nil
}

func parseParamUUID(c echo.Context, name string) (uuid.UUID, error) {
	return uuid.FromString(c.Param(name))
}

func tenantFromContext(c echo.Context) (uuid.UUID, bool) {
	tid, err := tenant.TenantIDFromContext(c.Request().Context())
	return tid, err == nil
}

func idsFromContext(c echo.Context) (uuid.UUID, uuid.UUID, bool) {
	tid, ok := tenantFromContext(c)
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	uid, ok := auth.UserIDFromContext(c.Request().Context())
	return tid, uid, ok
}

func (h *Handler) validation(c echo.Context, reason string) error {
	return h.error(c, http.StatusUnprocessableEntity, observability.ErrValidation.WithDetails(map[string]any{"reason": reason}))
}

func (h *Handler) mapServiceError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return h.error(c, http.StatusNotFound, observability.ErrValidation.WithDetails(map[string]any{"reason": "resource not found"}))
	case errors.Is(err, strategy.ErrInvalidRuleSet):
		return h.validation(c, "invalid strategy rules")
	case errors.Is(err, strategyservice.ErrNameRequired), errors.Is(err, behaviorservice.ErrInvalidWindow):
		return h.validation(c, err.Error())
	default:
		if h.log != nil {
			h.log.Error("r2 request failed", slog.String("error", err.Error()))
		}
		return h.error(c, http.StatusInternalServerError, observability.ErrInternal)
	}
}

func (h *Handler) error(c echo.Context, status int, err error) error {
	return c.JSON(status, observability.FromError(err))
}
