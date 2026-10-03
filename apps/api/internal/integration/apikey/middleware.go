package apikey

import (
	"context"
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/domain/api_key"
	"github.com/trading-journal/api/internal/ratelimit"
	"github.com/trading-journal/api/internal/tenant"
)

type ctxKey int

const apiKeyCtxKey ctxKey = iota

// WithAPIKey stores the APIKey on the context (handler-side read).
func WithAPIKey(ctx context.Context, k *api_key.APIKey) context.Context {
	return context.WithValue(ctx, apiKeyCtxKey, k)
}

// FromContext returns the APIKey attached by the middleware, or nil.
func FromContext(ctx context.Context) *api_key.APIKey {
	k, _ := ctx.Value(apiKeyCtxKey).(*api_key.APIKey)
	return k
}

// Middleware authenticates requests using an API-key bearer.
//
// On success the context carries:
//   - tenant id (via tenant.WithTenantID)
//   - api_key struct (via FromContext)
//
// On failure: 401 API_KEY_INVALID / 401 API_KEY_EXPIRED / 429 RATE_LIMITED.
//
// Usage:
//
//	e.GET("/v1/public/...", apikey.Middleware(svc, bucket, read:accounts))
func Middleware(svc Service, bucket *ratelimit.KeyBucket, required api_key.Scope) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			authz := c.Request().Header.Get("Authorization")
			if authz == "" {
				return c.JSON(http.StatusUnauthorized, errorEnvelope("API_KEY_REQUIRED", "missing Authorization header"))
			}
			k, err := svc.Authenticate(c.Request().Context(), authz)
			if err != nil {
				switch {
				case errors.Is(err, ErrInvalidBearer):
					return c.JSON(http.StatusUnauthorized, errorEnvelope("API_KEY_INVALID", "bearer format wrong"))
				case errors.Is(err, ErrUnknownKey):
					return c.JSON(http.StatusUnauthorized, errorEnvelope("API_KEY_INVALID", "unknown key"))
				case errors.Is(err, ErrBadSecret):
					return c.JSON(http.StatusUnauthorized, errorEnvelope("API_KEY_INVALID", "bad secret"))
				case errors.Is(err, ErrRevoked):
					return c.JSON(http.StatusUnauthorized, errorEnvelope("API_KEY_REVOKED", "key revoked"))
				case errors.Is(err, ErrExpired):
					return c.JSON(http.StatusUnauthorized, errorEnvelope("API_KEY_EXPIRED", "key expired"))
				case errors.Is(err, ErrInactive):
					return c.JSON(http.StatusUnauthorized, errorEnvelope("API_KEY_INACTIVE", "key inactive"))
				default:
					return c.JSON(http.StatusInternalServerError, errorEnvelope("INTERNAL_ERROR", err.Error()))
				}
			}
			if required != "" && !k.HasScope(required) {
				return c.JSON(http.StatusForbidden, errorEnvelope("API_KEY_SCOPE_MISSING",
					"missing required scope: "+string(required)))
			}
			if bucket != nil && !bucket.Allow(k.ID) {
				c.Response().Header().Set("Retry-After", "60")
				return c.JSON(http.StatusTooManyRequests, errorEnvelope("RATE_LIMITED", "per-key rate limit exceeded"))
			}
			ctx := tenant.WithTenantID(c.Request().Context(), k.TenantID)
			ctx = WithAPIKey(ctx, k)
			c.SetRequest(c.Request().WithContext(ctx))
			return next(c)
		}
	}
}

// errorEnvelope keeps the error JSON consistent with the rest of the
// API's `{error: {code, message, details}}` shape.
func errorEnvelope(code, msg string) map[string]any {
	return map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": msg,
		},
	}
}