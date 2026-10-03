package observability

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

// RateLimiter implements a simple Redis sliding-window counter (INCR + EXPIRE).
//
// Spec §7.6 windows are 60s. Key shape:
//
//	ratelimit:auth-ip:{ip}         (60 req / 60s)
//	ratelimit:user:{user_id}       (600 req / 60s)
//	ratelimit:tenant:{tenant_id}   (6000 req / 60s)
type RateLimiter struct {
	rdb       *redis.Client
	authMax   int
	userMax   int
	tenantMax int
	window    time.Duration
}

func NewRateLimiter(rdb *redis.Client) *RateLimiter {
	return &RateLimiter{
		rdb:       rdb,
		authMax:   60,
		userMax:   600,
		tenantMax: 6000,
		window:    60 * time.Second,
	}
}

type rateResult struct {
	Allowed   bool
	Limit     int
	Remaining int
	ResetAt   time.Time
}

// check increments the key and compares to max.
// Returns rateResult with headers data; on Redis error we fail-open (allow)
// but log the error elsewhere — availability > strict rate limit on a Redis hiccup.
func (r *RateLimiter) check(ctx context.Context, key string, max int) rateResult {
	now := time.Now().Unix()
	bucket := now - int64(r.window.Seconds())
	res := r.rdb.Incr(ctx, key)
	if err := res.Err(); err != nil {
		// fail-open
		return rateResult{Allowed: true, Limit: max, Remaining: max, ResetAt: nowToReset(now)}
	}
	count := res.Val()
	if count == 1 {
		// first request in window — set TTL
		r.rdb.Expire(ctx, key, r.window)
	}
	if count > int64(max) {
		return rateResult{Allowed: false, Limit: max, Remaining: 0, ResetAt: nowToReset(now)}
	}
	_ = bucket
	return rateResult{Allowed: true, Limit: max, Remaining: max - int(count), ResetAt: nowToReset(now)}
}

func nowToReset(nowUnix int64) time.Time {
	return time.Unix(nowUnix, 0).Add(time.Minute)
}

// AuthIPLimit applies 60 req/min/IP to /v1/auth/* routes.
// Mount on the auth route group.
func (r *RateLimiter) AuthIPLimit() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			key := fmt.Sprintf("ratelimit:auth-ip:%s", c.RealIP())
			res := r.check(c.Request().Context(), key, r.authMax)
			writeRateHeaders(c, res)
			if !res.Allowed {
				c.Response().Header().Set("Retry-After", strconv.FormatInt(int64(time.Until(res.ResetAt).Seconds()), 10))
				return c.JSON(429, FromError(ErrRateLimited))
			}
			return next(c)
		}
	}
}

// UserLimit applies 600 req/min/user to any authed route.
// Must be mounted AFTER Auth middleware so user_id is set on context.
func (r *RateLimiter) UserLimit() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			uid, _ := c.Get("user_id").(string)
			if uid == "" {
				return next(c)
			}
			key := fmt.Sprintf("ratelimit:user:%s", uid)
			res := r.check(c.Request().Context(), key, r.userMax)
			writeRateHeaders(c, res)
			if !res.Allowed {
				c.Response().Header().Set("Retry-After", strconv.FormatInt(int64(time.Until(res.ResetAt).Seconds()), 10))
				return c.JSON(429, FromError(ErrRateLimited))
			}
			return next(c)
		}
	}
}

// TenantLimit applies 6000 req/min/tenant for the heaviest guard.
// Must be mounted AFTER Auth middleware.
func (r *RateLimiter) TenantLimit() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			tid, _ := c.Get("tenant_id").(string)
			if tid == "" {
				return next(c)
			}
			key := fmt.Sprintf("ratelimit:tenant:%s", tid)
			res := r.check(c.Request().Context(), key, r.tenantMax)
			writeRateHeaders(c, res)
			if !res.Allowed {
				return c.JSON(429, FromError(ErrRateLimited))
			}
			return next(c)
		}
	}
}

func writeRateHeaders(c echo.Context, res rateResult) {
	h := c.Response().Header()
	h.Set("X-RateLimit-Limit", strconv.Itoa(res.Limit))
	h.Set("X-RateLimit-Remaining", strconv.Itoa(res.Remaining))
	h.Set("X-RateLimit-Reset", strconv.FormatInt(res.ResetAt.Unix(), 10))
}