package server

import (
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"

	"github.com/trading-journal/api/internal/auth"
	"github.com/trading-journal/api/internal/config"
	"github.com/trading-journal/api/internal/observability"
)

// RegisterRoutes mounts every endpoint under /v1 + the system endpoints.
//
// Phase 0 endpoints:
//   - GET /healthz           — process alive (no deps)
//   - GET /livez             — alias of /healthz for k8s liveness
//   - GET /readyz            — Postgres + Redis reachable
//   - POST /v1/auth/register
//   - POST /v1/auth/login
//   - POST /v1/auth/refresh
//   - POST /v1/auth/logout     (auth required)
//   - GET  /v1/me              (auth required)
//   - GET  /v1/auth/oauth/{provider}/start
//   - GET  /v1/auth/oauth/{provider}/callback
func RegisterRoutes(e *echo.Echo, pool *pgxpool.Pool, rdb *redis.Client, rl *observability.RateLimiter, cfg *config.Config, logger *slog.Logger) {
	// System endpoints (no auth, no rate limit)
	e.GET("/healthz", Healthz())
	e.GET("/livez", Healthz())
	e.GET("/readyz", Readyz(pool, rdb))

	// JWT signing key — loaded/generated on first boot (spec §7.2).
	jwtSvc, err := auth.NewJWTService(cfg.JWTKeyPath, cfg.JWTIssuer, cfg.JWTAudience, cfg.AccessTokenTTL)
	if err != nil {
		logger.Error("init jwt service", slog.String("error", err.Error()))
		// Cannot continue without JWT — fail fast at startup.
		// (server.go's run() will exit with non-zero code.)
		panic(err)
	}
	_ = time.Second // placeholder to keep `time` import if all real usages removed later

	v1 := e.Group("/v1")

	// Public auth (rate-limited per IP)
	authPublic := v1.Group("/auth")
	authPublic.Use(rl.AuthIPLimit())
	authPublic.POST("/register", RegisterHandlerPlaceholder())
	authPublic.POST("/login", LoginHandlerPlaceholder())
	authPublic.POST("/refresh", RefreshHandlerPlaceholder())
	authPublic.GET("/oauth/:provider/start", OAuthStartHandlerPlaceholder())
	authPublic.GET("/oauth/:provider/callback", OAuthCallbackHandlerPlaceholder())

	// Authed routes (auth + user/tenant rate limits)
	authed := v1.Group("", auth.AuthMiddleware(jwtSvc, logger))
	authed.Use(rl.UserLimit(), rl.TenantLimit())
	authed.POST("/auth/logout", LogoutHandlerPlaceholder())
	authed.GET("/me", MeHandlerPlaceholder())
}

// Placeholder handlers — return 501 until their real implementations are added
// in steps 7 (auth handlers) and 9 (oauth handlers). Each step replaces these.

func RegisterHandlerPlaceholder() echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.JSON(501, map[string]string{"error": "register handler not implemented yet"})
	}
}

func LoginHandlerPlaceholder() echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.JSON(501, map[string]string{"error": "login handler not implemented yet"})
	}
}

func RefreshHandlerPlaceholder() echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.JSON(501, map[string]string{"error": "refresh handler not implemented yet"})
	}
}

func LogoutHandlerPlaceholder() echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.JSON(501, map[string]string{"error": "logout handler not implemented yet"})
	}
}

func MeHandlerPlaceholder() echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.JSON(501, map[string]string{"error": "me handler not implemented yet"})
	}
}

func OAuthStartHandlerPlaceholder() echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.JSON(501, map[string]string{"error": "oauth start not implemented yet"})
	}
}

func OAuthCallbackHandlerPlaceholder() echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.JSON(501, map[string]string{"error": "oauth callback not implemented yet"})
	}
}