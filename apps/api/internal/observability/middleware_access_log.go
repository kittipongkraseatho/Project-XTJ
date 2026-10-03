package observability

import (
	"log/slog"
	"time"

	"github.com/labstack/echo/v4"
)

// AccessLog emits a structured slog JSON line per request after the handler returns.
// Fields: ts, level=info, message, request_id, method, path, status, latency_ms,
// ip, user_id (if authed), tenant_id (if authed).
//
// Auth middleware sets "user_id" and "tenant_id" on the echo context BEFORE
// AccessLog runs; AccessLog reads them from there.
func AccessLog(logger *slog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			err := next(c)
			latency := time.Since(start)

			req := c.Request()
			rid, _ := c.Get(HeaderRequestID).(string)
			uid, _ := c.Get("user_id").(string)
			tid, _ := c.Get("tenant_id").(string)

			status := c.Response().Status
			if err != nil {
				if he, ok := err.(*echo.HTTPError); ok {
					status = he.Code
				}
			}

			attrs := []any{
				slog.String("request_id", rid),
				slog.String("method", req.Method),
				slog.String("path", req.URL.Path),
				slog.Int("status", status),
				slog.Float64("latency_ms", float64(latency.Microseconds())/1000.0),
				slog.String("ip", c.RealIP()),
			}
			if uid != "" {
				attrs = append(attrs, slog.String("user_id", uid))
			}
			if tid != "" {
				attrs = append(attrs, slog.String("tenant_id", tid))
			}
			if err != nil {
				attrs = append(attrs, slog.String("error", err.Error()))
				logger.Warn("http request", attrs...)
			} else {
				logger.Info("http request", attrs...)
			}
			return err
		}
	}
}