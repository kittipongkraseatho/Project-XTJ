package worker

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	whsvc "github.com/trading-journal/api/internal/integration/webhook"
	"github.com/trading-journal/api/internal/store"
)

// WebhookWorker periodically calls WebhookService.DeliverDue to drain
// pending webhook_deliveries rows.
//
// Phase 5.2 single-instance: in-process sync.Mutex prevents two ticks
// from racing on the same rows (DB-side, the PickDue LIMIT + status
// filter means each tick grabs different rows anyway). Phase 6+ can
// swap for `SELECT ... FOR UPDATE SKIP LOCKED` for multi-instance.
type WebhookWorker struct {
	store         *store.Store
	svc           whsvc.Service
	http          whsvc.HTTPDoer
	log           *slog.Logger
	pollInterval  time.Duration
	batchSize     int
	requestTimeout time.Duration

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu      sync.Mutex
	running bool
}

// WebhookWorkerOptions configures the WebhookWorker. Zero values fall
// back to defaults: 30s poll, 50-batch, 10s per-request timeout.
type WebhookWorkerOptions struct {
	PollInterval   time.Duration
	BatchSize      int
	RequestTimeout time.Duration
}

// NewWebhookWorker builds a WebhookWorker. Call Start to begin polling.
func NewWebhookWorker(parent context.Context, st *store.Store, svc whsvc.Service, log *slog.Logger, opts WebhookWorkerOptions) *WebhookWorker {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 30 * time.Second
	}
	if opts.BatchSize <= 0 {
		opts.BatchSize = 50
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = 10 * time.Second
	}
	// Phase 5.2: shared *http.Client. Phase 6+ may add a custom transport
	// for connection pooling per host.
	httpClient := &http.Client{Timeout: opts.RequestTimeout}
	ctx, cancel := context.WithCancel(parent)
	return &WebhookWorker{
		store:          st,
		svc:            svc,
		http:           httpClient,
		log:            log,
		pollInterval:   opts.PollInterval,
		batchSize:      opts.BatchSize,
		requestTimeout: opts.RequestTimeout,
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Start launches the poll loop. No-op if already running.
func (w *WebhookWorker) Start() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return
	}
	w.running = true
	w.wg.Add(1)
	go w.pollLoop()
}

// Stop cancels the poll loop and waits for the in-flight tick to finish.
func (w *WebhookWorker) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	w.running = false
	w.mu.Unlock()

	w.cancel()
	w.wg.Wait()
}

func (w *WebhookWorker) pollLoop() {
	defer w.wg.Done()
	t := time.NewTicker(w.pollInterval)
	defer t.Stop()
	// Tick once immediately on start so the queue isn't blocked behind a
	// 30s gap in the worst case.
	w.tick()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-t.C:
			w.tick()
		}
	}
}

func (w *WebhookWorker) tick() {
	ctx, cancel := context.WithTimeout(w.ctx, 60*time.Second)
	defer cancel()
	summary, err := w.svc.DeliverDue(ctx, w.batchSize, w.http)
	if err != nil {
		w.log.Warn("webhook worker tick failed",
			slog.String("err", err.Error()))
		return
	}
	if summary.Picked > 0 {
		w.log.Info("webhook tick",
			slog.Int("picked", summary.Picked),
			slog.Int("succeeded", summary.Succeeded),
			slog.Int("failed", summary.Failed),
			slog.Int("exhausted", summary.Exhausted),
			slog.Int("rescheduled", summary.Rescheduled))
	}
}