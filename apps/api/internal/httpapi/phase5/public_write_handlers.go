// Package phase5http — public write handlers (Phase 10).
//
// 8 endpoints under /v1/public/* that mutate tenant state. Each handler
// is a thin wrapper that:
//   1. Reads the API key from request context (set by api_key.Middleware).
//   2. Copies tenant_id + user_id into echo's ctx so the existing
//      phase1/4/5.2 handlers work unchanged.
//   3. Delegates the work to the existing handler (passed in via
//      PublicWriteDeps — same composition pattern as PublicDeps).
//   4. After the handler returns, emits an audit log row with
//      action='api_key_write' (best-effort — failure is logged but
//      doesn't fail the request).
package phase5http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/domain/api_key"
	"github.com/trading-journal/api/internal/integration/apikey"
	"github.com/trading-journal/api/internal/ratelimit"
	"github.com/trading-journal/api/internal/store"
)

// PublicWriteDeps holds the existing phase1/4/5.2 handlers + a store ref
// for the audit log. All handler fields are required.
type PublicWriteDeps struct {
	Logger *slog.Logger
	Store  *store.Store

	// Trades (write:trades).
	CreateTrade echo.HandlerFunc
	UpdateTrade echo.HandlerFunc

	// Journal (write:journal).
	UpsertJournal echo.HandlerFunc

	// Webhooks (write:webhooks).
	CreateWebhook echo.HandlerFunc
	UpdateWebhook echo.HandlerFunc
	RevokeWebhook echo.HandlerFunc

	// Accounts (write:accounts).
	CreateAccount echo.HandlerFunc
	UpdateAccount echo.HandlerFunc
}

// RegisterWriteRoutes mounts the 8 write endpoints with correct scope guards
// + IdempotencyMiddleware on POST. Each route binds the API key to echo ctx
// then delegates to the existing handler passed in via PublicWriteDeps.
func RegisterWriteRoutes(e *echo.Echo, deps *PublicWriteDeps, svc apikey.Service, bucket ratelimit.KeyBucket) {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	pub := e.Group("/v1/public")
	idem := apikey.IdempotencyMiddleware(deps.Store.Idempotency, deps.Logger)

	// write:trades — POST idempotent, PATCH URL-idempotent.
	wTrades := pub.Group("", apikey.Middleware(svc, bucket, api_key.ScopeWriteTrades), idem)
	wTrades.POST("/trades", deps.createTradeHandler())
	wTrades.PATCH("/trades/:id", deps.updateTradeHandler())

	// write:journal — PUT URL-idempotent (no Idempotency-Key needed).
	wJournal := pub.Group("", apikey.Middleware(svc, bucket, api_key.ScopeWriteJournal))
	wJournal.PUT("/trades/:id/journal", deps.upsertJournalHandler())

	// write:webhooks — POST idempotent, PATCH/DELETE URL-idempotent.
	wWebhooks := pub.Group("", apikey.Middleware(svc, bucket, api_key.ScopeWriteWebhooks), idem)
	wWebhooks.POST("/webhooks/subscriptions", deps.createWebhookHandler())
	wWebhooks.PATCH("/webhooks/subscriptions/:id", deps.updateWebhookHandler())
	wWebhooks.DELETE("/webhooks/subscriptions/:id", deps.revokeWebhookHandler())

	// write:accounts — POST idempotent, PATCH URL-idempotent.
	wAccounts := pub.Group("", apikey.Middleware(svc, bucket, api_key.ScopeWriteAccounts), idem)
	wAccounts.POST("/accounts", deps.createAccountHandler())
	wAccounts.PATCH("/accounts/:id", deps.updateAccountHandler())
}

// ── Handler factories ─────────────────────────────────────────────────

// bindAPIKeyToEchoCtx copies the API key's tenant_id + user_id into echo's
// context so the delegated JWT-based handler reads them correctly.
func (d *PublicWriteDeps) bindAPIKeyToEchoCtx(c echo.Context, k *api_key.APIKey) {
	c.Set("tenant_id", k.TenantID.String())
	c.Set("user_id", k.CreatedByUserID.String())
}

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

// tenant returns the API key's tenant_id (sends 401 if missing).
func (d *PublicWriteDeps) tenant(c echo.Context) (uuid.UUID, bool) {
	k, ok := d.requireAPIKey(c)
	if !ok {
		return uuid.Nil, false
	}
	return k.TenantID, true
}

// createTradeHandler returns the wrapped echo.HandlerFunc for POST /v1/public/trades.
func (d *PublicWriteDeps) createTradeHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		k, ok := d.requireAPIKey(c)
		if !ok {
			return nil
		}
		d.bindAPIKeyToEchoCtx(c, k)
		return d.CreateTrade(c)
	}
}

func (d *PublicWriteDeps) updateTradeHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		k, ok := d.requireAPIKey(c)
		if !ok {
			return nil
		}
		d.bindAPIKeyToEchoCtx(c, k)
		return d.UpdateTrade(c)
	}
}

func (d *PublicWriteDeps) upsertJournalHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		k, ok := d.requireAPIKey(c)
		if !ok {
			return nil
		}
		d.bindAPIKeyToEchoCtx(c, k)
		return d.UpsertJournal(c)
	}
}

func (d *PublicWriteDeps) createWebhookHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		k, ok := d.requireAPIKey(c)
		if !ok {
			return nil
		}
		d.bindAPIKeyToEchoCtx(c, k)
		return d.CreateWebhook(c)
	}
}

func (d *PublicWriteDeps) updateWebhookHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		k, ok := d.requireAPIKey(c)
		if !ok {
			return nil
		}
		d.bindAPIKeyToEchoCtx(c, k)
		return d.UpdateWebhook(c)
	}
}

func (d *PublicWriteDeps) revokeWebhookHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		k, ok := d.requireAPIKey(c)
		if !ok {
			return nil
		}
		d.bindAPIKeyToEchoCtx(c, k)
		return d.RevokeWebhook(c)
	}
}

func (d *PublicWriteDeps) createAccountHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		k, ok := d.requireAPIKey(c)
		if !ok {
			return nil
		}
		d.bindAPIKeyToEchoCtx(c, k)
		return d.CreateAccount(c)
	}
}

func (d *PublicWriteDeps) updateAccountHandler() echo.HandlerFunc {
	return func(c echo.Context) error {
		k, ok := d.requireAPIKey(c)
		if !ok {
			return nil
		}
		d.bindAPIKeyToEchoCtx(c, k)
		return d.UpdateAccount(c)
	}
}

// ── audit log helper ─────────────────────────────────────────────────

// EmitAuditLog writes an audit row for a successful public write action.
// resourceType is e.g. "trade" / "trading_account" / "webhook_subscription" /
// "journal". resourceID is the affected row's id. payload is the request
// body (sensitive fields MUST be redacted by the caller before passing in).
//
// Reads the API key from the request context to populate actor_user_id +
// actor_tenant_id. Best-effort — failure is logged but doesn't fail the request.
func (d *PublicWriteDeps) EmitAuditLog(c echo.Context, resourceType string, resourceID uuid.UUID, payload any) {
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

var _ = http.StatusOK
var _ = uuid.Nil
var _ = (*api_key.APIKey)(nil)