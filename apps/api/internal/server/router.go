package server

import (
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"

	"github.com/trading-journal/api/internal/auth"
	"github.com/trading-journal/api/internal/config"
	authhttp "github.com/trading-journal/api/internal/httpapi/auth"
	mehttp "github.com/trading-journal/api/internal/httpapi/me"
	phase1http "github.com/trading-journal/api/internal/httpapi/phase1"
	"github.com/trading-journal/api/internal/integration/cloudinary"
	"github.com/trading-journal/api/internal/integration/oauth"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/service"
	"github.com/trading-journal/api/internal/store"
	"github.com/trading-journal/api/internal/worker"
)

// RegisterRoutes mounts every endpoint under /v1 + the system endpoints.
//
// Phase 0 + Phase 1 endpoints (all real):
//
//   - GET  /healthz / /livez / /readyz
//   - POST /v1/auth/register         (public, IP-rate-limited)
//   - POST /v1/auth/login            (public, IP-rate-limited)
//   - POST /v1/auth/refresh          (public, IP-rate-limited)
//   - GET  /v1/auth/oauth/:provider/start     (public, IP-rate-limited)
//   - GET  /v1/auth/oauth/:provider/callback  (public)
//   - POST /v1/auth/logout           (auth required)
//   - GET  /v1/me                    (auth required)
//   - POST /v1/imports               (auth required, multipart, async via worker)
//   - GET  /v1/imports               (auth required)
//   - GET  /v1/accounts              (auth required)
//   - POST /v1/accounts              (auth required)
//   - GET  /v1/trades                (auth required)
//   - GET  /v1/trades/:id            (auth required)
//   - GET  /v1/trades/:id/journal    (auth required)
//   - PUT  /v1/trades/:id/journal    (auth required)
//   - GET  /v1/trades/:id/tags       (auth required)
//   - PUT  /v1/trades/:id/tags       (auth required)
//   - GET  /v1/trades/:id/images     (auth required)
//   - POST /v1/trades/:id/images     (auth required, multipart, Cloudinary)
//   - GET  /v1/tags                  (auth required)
//   - POST /v1/tags                  (auth required)
//   - DELETE /v1/tags/:id            (auth required)
//   - GET  /v1/analytics/overview     (auth required)
//   - GET  /v1/analytics/daily-pl     (auth required)
//   - GET  /v1/analytics/equity-curve (auth required)
//   - GET  /v1/analytics/symbol-breakdown (auth required, Phase 1 §30 advanced)
func RegisterRoutes(e *echo.Echo, pool *pgxpool.Pool, rdb *redis.Client, rl *observability.RateLimiter, cfg *config.Config, logger *slog.Logger, workerPool *worker.Pool) {
	// System endpoints (no auth, no rate limit)
	e.GET("/healthz", Healthz())
	e.GET("/livez", Healthz())
	e.GET("/readyz", Readyz(pool, rdb))

	// JWT signing key — loaded/generated on first boot (spec §7.2).
	jwtSvc, err := auth.NewJWTService(cfg.JWTKeyPath, cfg.JWTIssuer, cfg.JWTAudience, cfg.AccessTokenTTL)
	if err != nil {
		logger.Error("init jwt service", slog.String("error", err.Error()))
		panic(err)
	}

	st := store.New(pool)

	// OAuth providers (only those with both ID + secret are usable)
	registry := oauth.NewRegistry()
	if cfg.GoogleOAuthEnabled() {
		registry.Register(oauth.NewGoogleProvider(cfg.OAuthGoogleID, cfg.OAuthGoogleSecret, cfg.OAuthGoogleRedirect))
		logger.Info("oauth provider enabled", slog.String("provider", "google"))
	}
	if cfg.LineOAuthEnabled() {
		registry.Register(oauth.NewLineProvider(cfg.OAuthLineID, cfg.OAuthLineSecret, cfg.OAuthLineRedirect))
		logger.Info("oauth provider enabled", slog.String("provider", "line"))
	}

	authSvc := service.NewAuthService(cfg, st, jwtSvc, logger)
	authH := authhttp.New(authSvc, logger)
	meH := mehttp.New(authSvc, logger)
	oauthFlow := oauth.NewFlow(cfg, st, registry, jwtSvc, logger)
	oauthH := authhttp.NewOAuthHandlers(oauthFlow, logger)
	importSvc := service.NewImportService(st, logger)
	importSvc.AttachPool(workerPool) // bg async; nil-safe fallback

	var cldSvc cloudinary.Service = cloudinary.StubService{}
	if cfg.CloudinaryEnabled() {
		cs, err := cloudinary.New(cfg.CloudinaryCloudName, cfg.CloudinaryAPIKey, cfg.CloudinaryAPISecret)
		if err != nil {
			logger.Error("init cloudinary", slog.String("error", err.Error()))
			panic(err)
		}
		cldSvc = cs
		logger.Info("cloudinary enabled", slog.String("cloud_name", cfg.CloudinaryCloudName))
	} else {
		logger.Warn("cloudinary not configured (env CLOUDINARY_* missing) — image upload disabled")
	}

	p1 := phase1http.New(st, importSvc, cldSvc, logger)

	v1 := e.Group("/v1")

	// Public auth (rate-limited per IP)
	authPublic := v1.Group("/auth")
	authPublic.Use(rl.AuthIPLimit())
	authPublic.POST("/register", authH.Register)
	authPublic.POST("/login", authH.Login)
	authPublic.POST("/refresh", authH.Refresh)
	authPublic.GET("/oauth/:provider/start", oauthH.Start)
	authPublic.GET("/oauth/:provider/callback", oauthH.Callback)

	// Authed routes
	authed := v1.Group("", auth.AuthMiddleware(jwtSvc, logger))
	authed.Use(rl.UserLimit(), rl.TenantLimit())
	authed.POST("/auth/logout", authH.Logout)
	authed.GET("/me", meH.Me)

	// Phase 1 — imports / trades / analytics / accounts (auth required)
	authed.POST("/imports", p1.Upload)
	authed.GET("/imports", p1.ListImports)
	authed.GET("/accounts", p1.ListAccounts)
	authed.POST("/accounts", p1.CreateAccount)
	authed.GET("/trades", p1.ListTrades)
	authed.GET("/trades/:id", p1.GetTrade)
	authed.GET("/analytics/overview", p1.OverviewAnalytics)
	authed.GET("/analytics/daily-pl", p1.DailyPL)
	authed.GET("/analytics/equity-curve", p1.EquityCurve)
	authed.GET("/analytics/symbol-breakdown", p1.SymbolBreakdown)

	// Phase 1 — journal, tags, images (auth + tenant scoped)
	authed.GET("/trades/:id/journal", p1.GetJournal)
	authed.PUT("/trades/:id/journal", p1.UpsertJournal)
	authed.GET("/trades/:id/tags", p1.ListTradeTags)
	authed.PUT("/trades/:id/tags", p1.SetTradeTags)
	authed.GET("/trades/:id/images", p1.ListImages)
	authed.POST("/trades/:id/images", p1.AttachImage)
	authed.GET("/tags", p1.ListTags)
	authed.POST("/tags", p1.CreateTag)
	authed.DELETE("/tags/:id", p1.DeleteTag)
}