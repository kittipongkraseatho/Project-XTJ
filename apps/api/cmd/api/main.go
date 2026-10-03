// Command api is the Trading Journal backend HTTP entrypoint.
//
// Lifecycle:
//  1. Load config from env
//  2. Set up slog JSON logger
//  3. Connect to Postgres (pgxpool) + Redis
//  4. Load EdDSA keypair for JWT signing
//  5. Build Echo server with middleware chain
//  6. Register routes
//  7. Graceful shutdown on SIGINT/SIGTERM (drains in 15s)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/trading-journal/api/internal/config"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/server"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := observability.NewLogger(cfg.LogLevel)
	slog.SetDefault(logger)
	logger.Info("starting trading-journal api",
		slog.String("env", cfg.Env),
		slog.Int("port", cfg.APIPort),
	)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv, cleanup, err := server.New(ctx, cfg, logger)
	if err != nil {
		return fmt.Errorf("build server: %w", err)
	}
	defer cleanup()

	httpSrv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.APIPort),
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", slog.String("addr", httpSrv.Addr))
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining")
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("listen: %w", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	logger.Info("server stopped cleanly")
	return nil
}