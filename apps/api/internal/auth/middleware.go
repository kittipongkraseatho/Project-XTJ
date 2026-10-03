package auth

import (
	"context"
	"log/slog"
	"strings"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/tenant"
)

// AuthMiddleware verifies the Authorization: Bearer header, verifies the token,
// attaches user_id / tenant_id / session_id / role to echo context and request context,
// and propagates tenant_id to ctx via tenant.WithTenantID.
//
// On any failure returns 401 UNAUTHORIZED envelope (no info leak).
func AuthMiddleware(j *JWTService, logger *slog.Logger) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			raw := c.Request().Header.Get(echo.HeaderAuthorization)
			if raw == "" {
				return c.JSON(401, observability.FromError(observability.ErrUnauthorized))
			}
			if !strings.HasPrefix(raw, "Bearer ") {
				return c.JSON(401, observability.FromError(observability.ErrUnauthorized))
			}
			tok := strings.TrimPrefix(raw, "Bearer ")

			claims, err := j.Verify(tok)
			if err != nil {
				logger.Debug("jwt verify failed", slog.String("error", err.Error()))
				return c.JSON(401, observability.FromError(observability.ErrUnauthorized))
			}

			userID, err := uuid.FromString(claims.Subject)
			if err != nil {
				return c.JSON(401, observability.FromError(observability.ErrUnauthorized))
			}
			tenantID, err := uuid.FromString(claims.TenantID)
			if err != nil {
				return c.JSON(401, observability.FromError(observability.ErrUnauthorized))
			}
			sessionID, err := uuid.FromString(claims.SessionID)
			if err != nil {
				return c.JSON(401, observability.FromError(observability.ErrUnauthorized))
			}

			c.Set("user_id", userID.String())
			c.Set("tenant_id", tenantID.String())
			c.Set("session_id", sessionID.String())
			c.Set("role", claims.Role)

			ctx := c.Request().Context()
			ctx = tenant.WithTenantID(ctx, tenantID)
			ctx = withUserID(ctx, userID)
			ctx = withSessionID(ctx, sessionID)
			c.SetRequest(c.Request().WithContext(ctx))

			return next(c)
		}
	}
}

// ── Context helpers ───────────────────────────────────────────────────

type ctxKey int

const (
	ctxUserID ctxKey = iota + 100
	ctxSessionID
)

func withUserID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, ctxUserID, id)
}

func UserIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	v, ok := ctx.Value(ctxUserID).(uuid.UUID)
	return v, ok
}

func withSessionID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, ctxSessionID, id)
}

func SessionIDFromContext(ctx context.Context) (uuid.UUID, bool) {
	v, ok := ctx.Value(ctxSessionID).(uuid.UUID)
	return v, ok
}