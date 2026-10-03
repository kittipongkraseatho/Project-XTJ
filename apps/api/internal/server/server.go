// Package server wires the Echo HTTP server, all middleware, and route registrations.
//
// Middleware order (matters for correctness) per spec §3.2:
//
//	SecurityHeaders → RequestID → Recovery → CORS → AccessLog → RateLimit(auth-ip/user/tenant) → Auth → RBAC
package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/redis/go-redis/v9"

	"github.com/trading-journal/api/internal/config"
	"github.com/trading-journal/api/internal/observability"
)

// New builds a fully wired Echo server.
//
// cleanup must be called on shutdown to close DB and Redis pools.
func New(ctx context.Context, cfg *config.Config, logger *slog.Logger) (*echo.Echo, func(), error) {
	pool, err := openPostgres(ctx, cfg)
	if err != nil {
		return nil, noop, fmt.Errorf("open postgres: %w", err)
	}

	rdb, err := openRedis(ctx, cfg)
	if err != nil {
		pool.Close()
		return nil, noop, fmt.Errorf("open redis: %w", err)
	}

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	// ── Global middleware ──────────────────────────────────────────────
	e.Use(observability.SecurityHeaders())
	e.Use(observability.RequestID())
	e.Use(observability.Recovery(logger))
	e.Use(observability.CORS(cfg.CORSAllowed()))
	e.Use(observability.AccessLog(logger))

	rl := observability.NewRateLimiter(rdb)
	RegisterRoutes(e, pool, rdb, rl, cfg, logger)

	cleanup := func() {
		pool.Close()
		if err := rdb.Close(); err != nil {
			logger.Warn("close redis", slog.String("error", err.Error()))
		}
	}
	return e, cleanup, nil
}

func openPostgres(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	if cfg.DBMaxConns > 0 {
		poolCfg.MaxConns = cfg.DBMaxConns
	}
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return pool, nil
}

func openRedis(ctx context.Context, cfg *config.Config) (*redis.Client, error) {
	opt, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("parse REDIS_URL: %w", err)
	}
	rdb := redis.NewClient(opt)
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return rdb, nil
}

func noop() {}

// EnsureKeysDir creates the JWT keypair directory if missing.
// Exposed for cmd/api/main.go so it can pre-create before JWT service init.
func EnsureKeysDir(path string) error {
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir keys dir: %w", err)
	}
	return nil
}