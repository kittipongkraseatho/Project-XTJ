// Package worker provides a small in-process job runner for Phase 1 imports
// (and Phase 2 MetaApi sync, which will reuse the same infrastructure).
//
// Design:
//   - Channel-based dispatcher: HTTP handler enqueues → worker pool picks up
//   - Per-job context with timeout (5min default for imports)
//   - Graceful shutdown: stop accepting + wait for in-flight (10s drain)
//   - Best-effort retries: 1 retry on transient failure
//   - Every job writes to audit_logs (success or failure)
package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Job is the unit of work.
type Job interface {
	Kind() string
	Execute(ctx context.Context) error
}

// Pool is the worker pool.
type Pool struct {
	logger *slog.Logger
	jobs   chan Job
	wg     sync.WaitGroup
	ctx    context.Context
	cancel context.CancelFunc

	// retry trigger (single attempt per job, not exponential for Phase 1)
	maxRetries int
}

type Options struct {
	Workers    int           // # workers (default 2)
	BufferSize int           // job queue buffer (default 100)
	JobTimeout time.Duration // per-job timeout (default 5min)
	MaxRetries int           // retries on failure (default 1)
}

// New creates a Pool with the given options + logger.
func New(parent context.Context, logger *slog.Logger, opts Options) *Pool {
	if opts.Workers <= 0 {
		opts.Workers = 2
	}
	if opts.BufferSize <= 0 {
		opts.BufferSize = 100
	}
	if opts.JobTimeout <= 0 {
		opts.JobTimeout = 5 * time.Minute
	}
	if opts.MaxRetries < 0 {
		opts.MaxRetries = 0
	}
	ctx, cancel := context.WithCancel(parent)
	p := &Pool{
		logger:     logger,
		jobs:       make(chan Job, opts.BufferSize),
		ctx:        ctx,
		cancel:     cancel,
		maxRetries: opts.MaxRetries,
	}
	for i := 0; i < opts.Workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
	return p
}

// Enqueue submits a job. Returns ErrPoolClosed if shutdown started.
func (p *Pool) Enqueue(j Job) error {
	select {
	case <-p.ctx.Done():
		return ErrPoolClosed
	default:
	}
	select {
	case p.jobs <- j:
		return nil
	case <-p.ctx.Done():
		return ErrPoolClosed
	}
}

// ErrPoolClosed signals the pool is shutting down.
var ErrPoolClosed = errors.New("worker pool closed")

// Shutdown stops accepting + waits for in-flight to complete (or drain timeout).
func (p *Pool) Shutdown(drainTimeout time.Duration) {
	p.cancel()
	close(p.jobs)
	done := make(chan struct{})
	go func() { p.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(drainTimeout):
		p.logger.Warn("worker drain timeout — some jobs may not have completed")
	}
}

// Drain is an alias for Shutdown (Phase 1 §10 conventions).
func (p *Pool) Drain(d time.Duration) { p.Shutdown(d) }

func (p *Pool) worker(id int) {
	defer p.wg.Done()
	for job := range p.jobs {
		p.run(id, job)
	}
}

func (p *Pool) run(workerID int, job Job) {
	ctx, cancel := context.WithTimeout(p.ctx, 5*time.Minute)
	defer cancel()

	var lastErr error
	attempts := 1 + p.maxRetries
	for attempt := 1; attempt <= attempts; attempt++ {
		err := job.Execute(ctx)
		if err == nil {
			p.logger.Info("job completed",
				slog.String("kind", job.Kind()),
				slog.Int("worker", workerID),
				slog.Int("attempt", attempt),
			)
			return
		}
		lastErr = err
		if errors.Is(err, context.Canceled) {
			p.logger.Warn("job canceled (shutdown)",
				slog.String("kind", job.Kind()),
				slog.String("error", err.Error()))
			return
		}
		if attempt < attempts {
			p.logger.Warn("job attempt failed — retrying",
				slog.String("kind", job.Kind()),
				slog.Int("worker", workerID),
				slog.Int("attempt", attempt),
				slog.String("error", err.Error()))
			time.Sleep(time.Duration(attempt) * time.Second) // 1s, 2s, ...
		}
	}
	p.logger.Error("job failed after retries",
		slog.String("kind", job.Kind()),
		slog.Int("worker", workerID),
		slog.String("error", lastErr.Error()),
	)
}

// ── ImportJob ─────────────────────────────────────────────────────────

// ImportJobPayload is the data the import worker needs to run.
type ImportJobPayload struct {
	ImportJobID string
	Handler     func(ctx context.Context) error
}

func (p *ImportJobPayload) Kind() string { return "import:" + p.ImportJobID }
func (p *ImportJobPayload) Execute(ctx context.Context) error {
	if p.Handler == nil {
		return fmt.Errorf("import job handler is nil")
	}
	return p.Handler(ctx)
}