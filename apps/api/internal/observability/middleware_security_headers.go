package observability

import "github.com/labstack/echo/v4"

// SecurityHeaders adds baseline response headers per spec §7.36.
// Phase 0: CSP allows self + Cloudinary (Phase 1+ adds more origins as features need).
func SecurityHeaders() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			h := c.Response().Header()
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Content-Security-Policy",
				"default-src 'self'; img-src 'self' https://res.cloudinary.com data:; "+
					"script-src 'self'; style-src 'self' 'unsafe-inline'; "+
					"connect-src 'self'; font-src 'self' data:; "+
					"frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
			return next(c)
		}
	}
}