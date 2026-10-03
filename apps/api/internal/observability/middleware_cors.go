package observability

import (
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// CORS middleware wraps echo's CORS with allowlist support.
// allowed must be a slice of exact origins (no wildcards in Phase 0 —
// spec §36 doesn't require them).
func CORS(allowed []string) echo.MiddlewareFunc {
	return middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: allowed,
		AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders: []string{
			echo.HeaderOrigin,
			echo.HeaderContentType,
			echo.HeaderAuthorization,
			HeaderRequestID,
			"X-Tenant-Id",
		},
		ExposeHeaders:    []string{HeaderRequestID},
		AllowCredentials: true,
		MaxAge:           86400,
	})
}