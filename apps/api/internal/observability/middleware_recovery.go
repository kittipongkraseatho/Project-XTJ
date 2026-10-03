package observability

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/labstack/echo/v4"
)

// Recovery middleware catches panics in downstream handlers, logs with stack,
// and responds with a uniform INTERNAL_ERROR envelope. Never leaks panic text.
func Recovery(logger *slog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			defer func() {
				if r := recover(); r != nil {
					rid, _ := c.Get(HeaderRequestID).(string)
					logger.Error("panic recovered",
						slog.Any("panic", r),
						slog.String("request_id", rid),
						slog.String("method", c.Request().Method),
						slog.String("path", c.Request().URL.Path),
						slog.String("stack", string(debug.Stack())),
					)
					_ = c.JSON(http.StatusInternalServerError, FromError(ErrInternal))
				}
			}()
			return next(c)
		}
	}
}