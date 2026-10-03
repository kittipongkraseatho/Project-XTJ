package observability

import (
	"context"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"
)

const (
	// HeaderRequestID is the canonical header for request correlation.
	HeaderRequestID = "X-Request-ID"
	// CtxKeyRequestID is the context key used by AccessLog + downstream callers.
	CtxKeyRequestID ctxKey = iota
)

type ctxKey int

// RequestID middleware assigns a per-request UUIDv7, echoes it in the response header,
// and stores it in both echo.Context (via Set) and the request context.
//
// If the client supplies X-Request-ID it is preserved (truncated/sanitized to
// UUID shape if too long). Otherwise we generate.
func RequestID() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			rid := c.Request().Header.Get(HeaderRequestID)
			if rid == "" || len(rid) > 64 {
				if u, err := uuid.NewV7(); err == nil {
					rid = u.String()
				} else {
					rid = "req-" + randomFallback()
				}
			}
			c.Response().Header().Set(HeaderRequestID, rid)
			c.Set(string(HeaderRequestID), rid)
			ctx := context.WithValue(c.Request().Context(), CtxKeyRequestID, rid)
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}

// RequestIDFromContext returns the request id, or empty string if missing.
func RequestIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(CtxKeyRequestID).(string); ok {
		return v
	}
	return ""
}

func randomFallback() string {
	if u, err := uuid.NewV7(); err == nil {
		return u.String()
	}
	return "unknown"
}