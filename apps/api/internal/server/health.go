package server

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"
)

// Healthz is a process-alive probe — no deps, returns 200 if Echo can serve.
func Healthz() echo.HandlerFunc {
	return func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{
			"status": "ok",
			"phase":  "0",
		})
	}
}

// Readyz is a readiness probe — checks Postgres + Redis reachability with
// short timeouts so a flaky downstream doesn't block k8s routing indefinitely.
func Readyz(pool *pgxpool.Pool, rdb *redis.Client) echo.HandlerFunc {
	return func(c echo.Context) error {
		ctx, cancel := context.WithTimeout(c.Request().Context(), 2*time.Second)
		defer cancel()

		checks := map[string]string{}
		overall := http.StatusOK

		if err := pool.Ping(ctx); err != nil {
			checks["postgres"] = "down: " + err.Error()
			overall = http.StatusServiceUnavailable
		} else {
			checks["postgres"] = "ok"
		}

		if err := rdb.Ping(ctx).Err(); err != nil {
			checks["redis"] = "down: " + err.Error()
			overall = http.StatusServiceUnavailable
		} else {
			checks["redis"] = "ok"
		}

		return c.JSON(overall, map[string]any{
			"status": map[bool]string{true: "ready", false: "not_ready"}[overall == http.StatusOK],
			"checks": checks,
		})
	}
}