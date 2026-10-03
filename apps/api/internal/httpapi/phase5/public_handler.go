// Package phase5http hosts the read-only public API endpoints
// (/v1/public/*) that authenticate via API key (not JWT).
//
// These handlers are thin wrappers around the existing phase1/phase4
// handlers — the only new concerns are:
//
//   1. The API-key middleware sets tenant_id on echo + request ctx
//   2. Scope checks happen in the middleware before the handler runs
//   3. Per-key rate limit is also in the middleware
//
// The handlers themselves do the actual store reads; we just remap the
// URL to the existing implementation.
package phase5http

import (
	"log/slog"

	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/domain/api_key"
	"github.com/trading-journal/api/internal/integration/apikey"
	"github.com/trading-journal/api/internal/ratelimit"
)

// PublicDeps wires the public router to the existing phase1/phase4 handlers.
// We hold interfaces rather than the concrete phase1/4 handlers so the BE
// router.go wires only the methods we actually expose.
type PublicDeps struct {
	Logger *slog.Logger

	// Accounts
	AccountsList     echo.HandlerFunc
	AccountsGet      echo.HandlerFunc // path-param :id
	AccountsTrades   echo.HandlerFunc // not exposed; reserved for v1.1

	// Trades
	TradesList echo.HandlerFunc
	TradesGet  echo.HandlerFunc

	// Analytics
	AnalyticsOverview       echo.HandlerFunc
	AnalyticsDailyPL        echo.HandlerFunc
	AnalyticsEquityCurve    echo.HandlerFunc
	AnalyticsSymbolBreakdown echo.HandlerFunc
	AnalyticsSession        echo.HandlerFunc
	AnalyticsEA             echo.HandlerFunc
	AnalyticsDayOfWeek      echo.HandlerFunc
	AnalyticsHourOfDay      echo.HandlerFunc

	// Billing
	BillingPayments echo.HandlerFunc
}

// RegisterRoutes mounts /v1/public/* behind the API-key middleware.
//
// Each group enforces a single scope. The middleware:
//
//   - parses Bearer trj_pk_<prefix>.<secret>
//   - bcrypts + looks up the key
//   - injects tenant_id into echo ctx + request ctx
//   - checks the requested scope (else 403)
//   - runs the per-key rate limiter (else 429)
//
// Routes:
//
//	GET /v1/public/accounts                  read:accounts
//	GET /v1/public/accounts/:id              read:accounts
//	GET /v1/public/trades                    read:trades
//	GET /v1/public/trades/:id                read:trades
//	GET /v1/public/analytics/overview        read:analytics
//	GET /v1/public/analytics/daily-pl        read:analytics
//	GET /v1/public/analytics/equity-curve    read:analytics
//	GET /v1/public/analytics/symbol-breakdown read:analytics
//	GET /v1/public/analytics/session         read:analytics
//	GET /v1/public/analytics/ea              read:analytics
//	GET /v1/public/analytics/day-of-week     read:analytics
//	GET /v1/public/analytics/hour-of-day     read:analytics
//	GET /v1/public/billing/payments          read:payments
func RegisterRoutes(e *echo.Echo, deps *PublicDeps, svc apikey.Service, bucket *ratelimit.KeyBucket) {
	pub := e.Group("/v1/public")

	// Accounts
	accounts := pub.Group("", apikey.Middleware(svc, bucket, api_key.ScopeReadAccounts))
	accounts.GET("/accounts", deps.AccountsList)
	accounts.GET("/accounts/:id", deps.AccountsGet)

	// Trades
	trades := pub.Group("", apikey.Middleware(svc, bucket, api_key.ScopeReadTrades))
	trades.GET("/trades", deps.TradesList)
	trades.GET("/trades/:id", deps.TradesGet)

	// Analytics
	analytics := pub.Group("", apikey.Middleware(svc, bucket, api_key.ScopeReadAnalytics))
	analytics.GET("/analytics/overview", deps.AnalyticsOverview)
	analytics.GET("/analytics/daily-pl", deps.AnalyticsDailyPL)
	analytics.GET("/analytics/equity-curve", deps.AnalyticsEquityCurve)
	analytics.GET("/analytics/symbol-breakdown", deps.AnalyticsSymbolBreakdown)
	analytics.GET("/analytics/session", deps.AnalyticsSession)
	analytics.GET("/analytics/ea", deps.AnalyticsEA)
	analytics.GET("/analytics/day-of-week", deps.AnalyticsDayOfWeek)
	analytics.GET("/analytics/hour-of-day", deps.AnalyticsHourOfDay)

	// Billing
	billing := pub.Group("", apikey.Middleware(svc, bucket, api_key.ScopeReadPayments))
	billing.GET("/billing/payments", deps.BillingPayments)
}