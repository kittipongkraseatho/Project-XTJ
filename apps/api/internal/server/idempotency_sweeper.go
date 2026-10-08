package server

import (
	"context"
	"log/slog"
	"time"

	"github.com/trading-journal/api/internal/store"
)

// IdempotencySweeper periodically deletes expired rows from
// api_key_idempotency. Free-up disk only — the middleware's expires_at filter
// is the correctness boundary, so this sweeper can fall behind without
// affecting API behavior.
//
// Runs every hour by default. Started in server.New alongside SyncWorker + WebhookWorker.
type IdempotencySweeper struct {
	repo    *store.IdempotencyRepo
	interval time.Duration
	log     *slog.Logger
}

// NewIdempotencySweeper constructs the sweeper.
func NewIdempotencySweeper(repo *store.IdempotencyRepo, interval time.Duration, log *slog.Logger) *IdempotencySweeper {
	if interval <= 0 {
		interval = time.Hour
	}
	if log == nil {
		log = slog.Default()
	}
	return &IdempotencySweeper{repo: repo, interval: interval, log: log}
}

// Start launches the sweeper goroutine. Returns a stop function.
func (s *IdempotencySweeper) Start(parent context.Context) func() {
	ctx, cancel := context.WithCancel(parent)
	go s.loop(ctx)
	return cancel
}

func (s *IdempotencySweeper) loop(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	// First sweep on startup (best-effort).
	if _, err := s.repo.SweepExpired(ctx); err != nil {
		s.log.Warn("idempotency sweep failed", slog.String("err", err.Error()))
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := s.repo.SweepExpired(ctx)
			if err != nil {
				s.log.Warn("idempotency sweep failed", slog.String("err", err.Error()))
				continue
			}
			if n > 0 {
				s.log.Info("idempotency sweep done", slog.Int64("deleted", n))
			}
		}
	}
}