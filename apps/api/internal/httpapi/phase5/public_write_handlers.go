// Package phase5http — public write handlers (Phase 10).
//
// 8 endpoints under /v1/public/* that mutate tenant state. Each handler:
//   1. Reads the API key from request context (set by api_key.Middleware).
//   2. Verifies tenant ownership of the target resource (defense in depth).
//   3. Calls the existing store/service directly (small business logic).
//   4. After success, emits an audit log row (action='api_key_write').
//
// The audit log is best-effort — failure is logged but doesn't fail the request.
// Idempotency-Key (required on POST) is enforced by the middleware; we don't
// touch it here.
package phase5http

import (
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/domain/api_key"
	"github.com/trading-journal/api/internal/domain/journal_entry"
	"github.com/trading-journal/api/internal/domain/trade"
	"github.com/trading-journal/api/internal/domain/trading_account"
	"github.com/trading-journal/api/internal/domain/webhook"
	"github.com/trading-journal/api/internal/integration/apikey"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/ratelimit"
	"github.com/trading-journal/api/internal/store"
)

// PublicWriteDeps holds the store + logger the 8 write handlers need.
type PublicWriteDeps struct {
	Logger *slog.Logger
	Store  *store.Store
}

// RegisterWriteRoutes mounts the 8 write endpoints on /v1/public/* with the
// correct scope guards + IdempotencyMiddleware on POST.
func RegisterWriteRoutes(e *echo.Echo, deps *PublicWriteDeps, svc apikey.Service, bucket ratelimit.KeyBucket) {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	pub := e.Group("/v1/public")
	idem := apikey.IdempotencyMiddleware(deps.Store.Idempotency, deps.Logger)

	// write:trades
	wTrades := pub.Group("", apikey.Middleware(svc, bucket, api_key.ScopeWriteTrades), idem)
	wTrades.POST("/trades", deps.CreatePublicTrade)
	wTrades.PATCH("/trades/:id", deps.UpdatePublicTrade)

	// write:journal
	wJournal := pub.Group("", apikey.Middleware(svc, bucket, api_key.ScopeWriteJournal))
	wJournal.PUT("/trades/:id/journal", deps.UpsertPublicJournal)

	// write:webhooks
	wWebhooks := pub.Group("", apikey.Middleware(svc, bucket, api_key.ScopeWriteWebhooks), idem)
	wWebhooks.POST("/webhooks/subscriptions", deps.CreatePublicWebhook)
	wWebhooks.PATCH("/webhooks/subscriptions/:id", deps.UpdatePublicWebhook)
	wWebhooks.DELETE("/webhooks/subscriptions/:id", deps.RevokePublicWebhook)

	// write:accounts
	wAccounts := pub.Group("", apikey.Middleware(svc, bucket, api_key.ScopeWriteAccounts), idem)
	wAccounts.POST("/accounts", deps.CreatePublicAccount)
	wAccounts.PATCH("/accounts/:id", deps.UpdatePublicAccount)
}

// ── Context helpers ──────────────────────────────────────────────────

func (d *PublicWriteDeps) requireAPIKey(c echo.Context) (*api_key.APIKey, bool) {
	k := apikey.FromContext(c.Request().Context())
	if k == nil {
		_ = c.JSON(http.StatusUnauthorized, map[string]any{
			"error": map[string]any{"code": "API_KEY_REQUIRED", "message": "no api key on context"},
		})
		return nil, false
	}
	return k, true
}

func (d *PublicWriteDeps) tenant(c echo.Context) (uuid.UUID, bool) {
	k, ok := d.requireAPIKey(c)
	if !ok {
		return uuid.Nil, false
	}
	return k.TenantID, true
}

// emitAuditLog writes an audit row (best-effort).
func (d *PublicWriteDeps) emitAuditLog(c echo.Context, resourceType string, resourceID uuid.UUID, payload any) {
	if d == nil || d.Store == nil {
		return
	}
	k := apikey.FromContext(c.Request().Context())
	if k == nil {
		return
	}
	rt := resourceType
	rid := resourceID
	meta, _ := json.Marshal(map[string]any{"payload": payload})
	if err := d.Store.Audit.Insert(c.Request().Context(), store.AuditEntry{
		Action:        "api_key_write",
		ActorUserID:   &k.CreatedByUserID,
		ActorTenantID: &k.TenantID,
		ResourceType:  &rt,
		ResourceID:    &rid,
		Metadata:      map[string]any{"payload": json.RawMessage(meta)},
	}); err != nil && d.Logger != nil {
		d.Logger.Warn("audit log insert failed",
			slog.String("err", err.Error()),
			slog.String("resource_type", resourceType),
			slog.String("resource_id", rid.String()))
	}
}

// ── write:trades ──────────────────────────────────────────────────────

// CreatePublicTrade — POST /v1/public/trades
func (d *PublicWriteDeps) CreatePublicTrade(c echo.Context) error {
	k, tenantID, ok := d.requireAPIKeyAll(c)
	if !ok {
		return nil
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
	// Tenant guard.
	acc, err := d.Store.TradingAccounts.GetByID(c.Request().Context(), body.TradingAccountID)
	if err != nil || acc.TenantID != tenantID {
		return c.JSON(http.StatusNotFound, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "trading_account not found"})))
	}
	tr := &trade.Trade{
		ID:               uuid.Must(uuid.NewV7()),
		TenantID:         tenantID,
		UserID:           k.CreatedByUserID,
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
	d.emitAuditLog(c, "trade", tr.ID, body)
	return c.JSON(http.StatusCreated, tr)
}

// UpdatePublicTrade — PATCH /v1/public/trades/:id
func (d *PublicWriteDeps) UpdatePublicTrade(c echo.Context) error {
	tenantID, ok := d.tenant(c)
	if !ok {
		return nil
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	tr, err := d.Store.Trades.GetByID(c.Request().Context(), id)
	if err != nil {
		if err == store.ErrNotFound {
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
	d.emitAuditLog(c, "trade", tr.ID, body)
	return c.JSON(http.StatusOK, tr)
}

// ── write:journal ────────────────────────────────────────────────────

// UpsertPublicJournal — PUT /v1/public/trades/:id/journal
func (d *PublicWriteDeps) UpsertPublicJournal(c echo.Context) error {
	k, tenantID, ok := d.requireAPIKeyAll(c)
	if !ok {
		return nil
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	tr, err := d.Store.Trades.GetByID(c.Request().Context(), id)
	if err != nil {
		if err == store.ErrNotFound {
			return c.JSON(http.StatusNotFound, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "trade not found"})))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	if tr.TenantID != tenantID {
		return c.JSON(http.StatusNotFound, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "trade not found"})))
	}
	var body JournalUpsertInput
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	entry := &journal_entry.JournalEntry{
		ID:          uuid.Must(uuid.NewV7()),
		TenantID:    tenantID,
		UserID:      k.CreatedByUserID,
		TradeID:     id,
		Strategy:    body.Strategy,
		EntryReason: body.EntryReason,
		ExitReason:  body.ExitReason,
		Emotion:     body.Emotion,
		Mistake:     body.Mistake,
		Notes:       body.Notes,
		Metadata:    map[string]any{},
	}
	if err := d.Store.JournalEntries.Upsert(c.Request().Context(), entry); err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	d.emitAuditLog(c, "journal", id, body)
	return c.JSON(http.StatusOK, entry)
}

// ── write:webhooks ───────────────────────────────────────────────────

// CreatePublicWebhook — POST /v1/public/webhooks/subscriptions
// Returns the plaintext secret (shown ONCE — same contract as Phase 5.2 admin).
func (d *PublicWriteDeps) CreatePublicWebhook(c echo.Context) error {
	tenantID, ok := d.tenant(c)
	if !ok {
		return nil
	}
	var body struct {
		URL         string   `json:"url"`
		Description string   `json:"description"`
		Events      []string `json:"events"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	if body.URL == "" || len(body.Events) == 0 {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "url and events required"})))
	}
	// Generate 32-byte random secret (URL-safe base64).
	plaintext, err := generateWebhookSecret()
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	// Phase 5.2 stores the secret as plaintext (DB at-rest encryption handles it).
	evts := make([]webhook.EventType, 0, len(body.Events))
	for _, e := range body.Events {
		evts = append(evts, webhook.EventType(e))
	}
	sub := &webhook.Subscription{
		ID:          uuid.Must(uuid.NewV7()),
		TenantID:    tenantID,
		URL:         body.URL,
		Description: body.Description,
		EventTypes:  evts,
		Secret:      plaintext,
		IsActive:    true,
	}
	if err := d.Store.WebhookSubs.Create(c.Request().Context(), sub); err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	d.emitAuditLog(c, "webhook_subscription", sub.ID, map[string]any{
		"url": body.URL, "events": body.Events,
	})
	events := make([]string, len(sub.EventTypes))
	for i, e := range sub.EventTypes {
		events[i] = string(e)
	}
	return c.JSON(http.StatusCreated, map[string]any{
		"id":               sub.ID,
		"url":              sub.URL,
		"events":           events,
		"description":      sub.Description,
		"is_active":        sub.IsActive,
		"created_at":       sub.CreatedAt,
		"secret":           plaintext,
		"secret_shown_once": true,
	})
}

// UpdatePublicWebhook — PATCH /v1/public/webhooks/subscriptions/:id
func (d *PublicWriteDeps) UpdatePublicWebhook(c echo.Context) error {
	tenantID, ok := d.tenant(c)
	if !ok {
		return nil
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	sub, err := d.Store.WebhookSubs.GetByIDByTenant(c.Request().Context(), tenantID, id)
	if err != nil || sub == nil {
		return c.JSON(http.StatusNotFound, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "subscription not found"})))
	}
	var body map[string]any
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	if v, ok := body["url"].(string); ok && v != "" {
		sub.URL = v
	}
	if v, ok := body["description"].(string); ok {
		sub.Description = v
	}
	if v, ok := body["events"].([]any); ok {
		evs := make([]webhook.EventType, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok {
				evs = append(evs, webhook.EventType(s))
			}
		}
		if len(evs) > 0 {
			sub.EventTypes = evs
		}
	}
	if v, ok := body["is_active"].(bool); ok {
		sub.IsActive = v
	}
	if err := d.Store.WebhookSubs.Update(c.Request().Context(), sub); err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	d.emitAuditLog(c, "webhook_subscription", sub.ID, body)
	return c.JSON(http.StatusOK, sub)
}

// RevokePublicWebhook — DELETE /v1/public/webhooks/subscriptions/:id
func (d *PublicWriteDeps) RevokePublicWebhook(c echo.Context) error {
	tenantID, ok := d.tenant(c)
	if !ok {
		return nil
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	sub, err := d.Store.WebhookSubs.GetByIDByTenant(c.Request().Context(), tenantID, id)
	if err != nil || sub == nil {
		return c.JSON(http.StatusNotFound, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "subscription not found"})))
	}
	if err := d.Store.WebhookSubs.Revoke(c.Request().Context(), tenantID, id, time.Now().UTC()); err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	d.emitAuditLog(c, "webhook_subscription", sub.ID, map[string]any{"revoked": true})
	return c.NoContent(http.StatusNoContent)
}

// ── write:accounts ───────────────────────────────────────────────────

// CreatePublicAccount — POST /v1/public/accounts
func (d *PublicWriteDeps) CreatePublicAccount(c echo.Context) error {
	k, tenantID, ok := d.requireAPIKeyAll(c)
	if !ok {
		return nil
	}
	var body AccountCreateInput
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, observability.FromError(
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
	a := &trading_account.TradingAccount{
		ID:              uuid.Must(uuid.NewV7()),
		TenantID:        tenantID,
		UserID:          k.CreatedByUserID,
		Broker:          trading_account.Broker(body.Broker),
		BrokerAccountID: body.BrokerAccountID,
		DisplayName:     body.DisplayName,
		Currency:        cur,
		Leverage:        body.Leverage,
		Kind:            trading_account.Kind(kind),
		Metadata:        map[string]any{},
		IsActive:        true,
	}
	if err := d.Store.TradingAccounts.Create(c.Request().Context(), a); err != nil {
		if store.IsUniqueViolation(err) {
			return c.JSON(http.StatusConflict, observability.FromError(
				observability.ErrValidation.WithDetails(map[string]any{"reason": "duplicate broker_account_id"})))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	d.emitAuditLog(c, "trading_account", a.ID, body)
	return c.JSON(http.StatusCreated, a)
}

// UpdatePublicAccount — PATCH /v1/public/accounts/:id
func (d *PublicWriteDeps) UpdatePublicAccount(c echo.Context) error {
	tenantID, ok := d.tenant(c)
	if !ok {
		return nil
	}
	id, err := uuid.FromString(c.Param("id"))
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid id"})))
	}
	a, err := d.Store.TradingAccounts.GetByID(c.Request().Context(), id)
	if err != nil || a.TenantID != tenantID {
		return c.JSON(http.StatusNotFound, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "account not found"})))
	}
	var body AccountUpdateInput
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusBadRequest, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	if body.DisplayName != nil {
		a.DisplayName = *body.DisplayName
	}
	if body.Currency != nil {
		a.Currency = *body.Currency
	}
	if body.Leverage != nil {
		a.Leverage = body.Leverage
	}
	if err := d.Store.TradingAccounts.Update(c.Request().Context(), a); err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	d.emitAuditLog(c, "trading_account", a.ID, body)
	return c.JSON(http.StatusOK, a)
}

// ── helpers ──────────────────────────────────────────────────────────

// requireAPIKeyAll extracts (apiKey, tenantID, ok).
func (d *PublicWriteDeps) requireAPIKeyAll(c echo.Context) (*api_key.APIKey, uuid.UUID, bool) {
	k, ok := d.requireAPIKey(c)
	if !ok {
		return nil, uuid.Nil, false
	}
	return k, k.TenantID, true
}

// generateWebhookSecret returns a 32-byte URL-safe base64 secret
// prefixed with "whsec_".
func generateWebhookSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	// Convert to a 64-char URL-safe alphabet (no padding).
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
	out := make([]byte, 64)
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return "whsec_" + string(out), nil
}

// ── Input DTOs ───────────────────────────────────────────────────────

// TradeCreateInput is the POST body for /v1/public/trades.
type TradeCreateInput struct {
	TradingAccountID uuid.UUID  `json:"trading_account_id"`
	Symbol           string     `json:"symbol"`
	Direction        string     `json:"direction"`
	Volume           float64    `json:"volume"`
	OpenPrice        float64    `json:"open_price"`
	OpenTime         time.Time  `json:"open_time"`
	ClosePrice       *float64   `json:"close_price,omitempty"`
	CloseTime        *time.Time `json:"close_time,omitempty"`
	Magic            *int64     `json:"magic,omitempty"`
}

// TradeUpdateInput is the PATCH body for /v1/public/trades/:id.
type TradeUpdateInput struct {
	ClosePrice *float64   `json:"close_price,omitempty"`
	CloseTime  *time.Time `json:"close_time,omitempty"`
	Magic      *int64     `json:"magic,omitempty"`
}

// JournalUpsertInput is the PUT body for /v1/public/trades/:id/journal.
type JournalUpsertInput struct {
	Strategy    *string                `json:"strategy,omitempty"`
	EntryReason *string                `json:"entry_reason,omitempty"`
	ExitReason  *string                `json:"exit_reason,omitempty"`
	Emotion     *journal_entry.Emotion `json:"emotion,omitempty"`
	Mistake     *string                `json:"mistake,omitempty"`
	Notes       *string                `json:"notes,omitempty"`
}

// AccountCreateInput is the POST body for /v1/public/accounts.
type AccountCreateInput struct {
	Broker          string   `json:"broker"`
	BrokerAccountID string   `json:"broker_account_id"`
	DisplayName     string   `json:"display_name"`
	Currency        string   `json:"currency"`
	Leverage        *float64 `json:"leverage,omitempty"`
	Kind            string   `json:"kind,omitempty"`
}

// AccountUpdateInput is the PATCH body for /v1/public/accounts/:id.
type AccountUpdateInput struct {
	DisplayName *string  `json:"display_name,omitempty"`
	Currency    *string  `json:"currency,omitempty"`
	Leverage    *float64 `json:"leverage,omitempty"`
}

var _ = http.StatusOK
var _ = uuid.Nil
var _ = (*api_key.APIKey)(nil)
var _ ratelimit.KeyBucket // keep import alive (RegisterWriteRoutes uses it)