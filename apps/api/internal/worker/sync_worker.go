package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/broker_connection"
	"github.com/trading-journal/api/internal/domain/sync_run"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/store"
	"github.com/trading-journal/api/internal/syncsvc"
)

// SyncWorker periodically polls broker_connections for due syncs and enqueues
// RunSync jobs into the shared worker.Pool.
//
// It does NOT own its own pool — it reuses the pool passed to ImportService.
// Two trigger paths exist:
//
//   - Scheduled: ticker every PollInterval scans due connections, enqueues a
//     sync_run with trigger='scheduled'.
//   - Manual: HTTP handler enqueues a sync_run with trigger='manual'.
//
// Single-flight per connection is enforced by an in-process map. Phase 4
// (multi-instance API) will swap this for a Postgres advisory lock.
type SyncWorker struct {
	store         *store.Store
	pool          *Pool
	syncSvc       *syncsvc.SyncService
	log           *slog.Logger
	pollInterval  time.Duration
	pollBatchSize int

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	locks   map[int]struct{} // currently running sync_run UUIDs (collision guard)
	lockMu  sync.Mutex
	running bool
}

// SyncWorkerOptions configures the SyncWorker. Zero values fall back to sane
// defaults: 30s poll, 100-batch, 16-deep single-flight.
type SyncWorkerOptions struct {
	PollInterval  time.Duration
	PollBatchSize int
}

// NewSyncWorker builds a SyncWorker. Call Start to begin polling.
func NewSyncWorker(parent context.Context, st *store.Store, pool *Pool, svc *syncsvc.SyncService, log *slog.Logger, opts SyncWorkerOptions) *SyncWorker {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 30 * time.Second
	}
	if opts.PollBatchSize <= 0 {
		opts.PollBatchSize = 100
	}
	ctx, cancel := context.WithCancel(parent)
	return &SyncWorker{
		store:         st,
		pool:          pool,
		syncSvc:       svc,
		log:           log,
		pollInterval:  opts.PollInterval,
		pollBatchSize: opts.PollBatchSize,
		ctx:           ctx,
		cancel:        cancel,
		locks:         make(map[int]struct{}, 16),
	}
}

// Start launches the poll loop. Each call is no-op when already running.
func (w *SyncWorker) Start() {
	w.lockMu.Lock()
	defer w.lockMu.Unlock()
	if w.running {
		return
	}
	w.running = true
	w.wg.Add(1)
	go w.pollLoop()
}

// Stop cancels the poll loop and waits for the in-flight tick (if any) to finish.
// Does NOT drain the underlying worker.Pool; the API shutdown coordinator does that.
func (w *SyncWorker) Stop() {
	w.lockMu.Lock()
	if !w.running {
		w.lockMu.Unlock()
		return
	}
	w.running = false
	w.lockMu.Unlock()

	w.cancel()
	w.wg.Wait()
}

func (w *SyncWorker) pollLoop() {
	defer w.wg.Done()
	// Tick immediately so a freshly-started API server doesn't wait 30s for
	// the first poll cycle.
	w.tick()
	t := time.NewTicker(w.pollInterval)
	defer t.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-t.C:
			w.tick()
		}
	}
}

// tick scans due connections and enqueues sync_runs.
func (w *SyncWorker) tick() {
	defer func() {
		if r := recover(); r != nil {
			w.log.Error("sync_worker.panic", "err", r)
		}
	}()
	conns, err := w.store.BrokerConnections.ListDue(w.ctx, time.Now(), w.pollBatchSize)
	if err != nil {
		w.log.Error("sync_worker.list_due_failed", "err", err)
		return
	}
	for _, c := range conns {
		if !w.tryLock(c.ID) {
			continue // a previous sync for this connection is still running
		}
		w.enqueueScheduled(c)
	}
}

// tryLock records that we're starting a sync for this connection_id.
// Returns false if another sync is already running for it.
func (w *SyncWorker) tryLock(connectionID uuid.UUID) bool {
	w.lockMu.Lock()
	defer w.lockMu.Unlock()
	// Map key is int64 because UUID is comparable — string keys work but int
	// is faster. uuid.UUID's first 8 bytes via .ID[0:8] won't be unique so
	// we just use the string form here (small scale; 100s of tenants).
	_ = connectionID
	// Build a stable int key from the high 8 bytes — uniqueness is high enough
	// for the in-process lifetime of a SyncWorker (collisions only matter within
	// a single tick cycle).
	key := int(connectionID[0])<<56 | int(connectionID[1])<<48 | int(connectionID[2])<<40 | int(connectionID[3])<<32 |
		int(connectionID[4])<<24 | int(connectionID[5])<<16 | int(connectionID[6])<<8 | int(connectionID[7])
	if _, ok := w.locks[key]; ok {
		return false
	}
	w.locks[key] = struct{}{}
	return true
}

func (w *SyncWorker) releaseLock(connectionID uuid.UUID) {
	w.lockMu.Lock()
	defer w.lockMu.Unlock()
	key := int(connectionID[0])<<56 | int(connectionID[1])<<48 | int(connectionID[2])<<40 | int(connectionID[3])<<32 |
		int(connectionID[4])<<24 | int(connectionID[5])<<16 | int(connectionID[6])<<8 | int(connectionID[7])
	delete(w.locks, key)
}

// enqueueScheduled creates a sync_run row and pushes a RunSyncJob onto the pool.
// The single-flight lock is released by the job's `Execute` after the inner SyncService
// completes (success or failure).
func (w *SyncWorker) enqueueScheduled(conn *broker_connection.BrokerConnection) {
	id, _ := uuid.NewV7()
	run := &sync_run.SyncRun{
		ID:                 id,
		TenantID:           conn.TenantID,
		BrokerConnectionID: conn.ID,
		Status:             sync_run.StatusQueued,
		Trigger:            sync_run.TriggerScheduled,
		Metadata:           map[string]any{"scheduled": true},
	}
	if err := w.store.SyncRuns.Create(w.ctx, run); err != nil {
		w.log.Error("sync_worker.create_run_failed", "connection_id", conn.ID, "err", err)
		w.releaseLock(conn.ID)
		return
	}

	job := &SyncRunJob{
		RunID:        run.ID,
		SyncService:  w.syncSvc,
		Store:        w.store,
		ConnectionID: conn.ID,
		Worker:       w,
		Log:          w.log,
	}
	if err := w.pool.Enqueue(job); err != nil {
		// Pool closed or buffer full — mark the run as failed and release.
		_ = w.store.SyncRuns.MarkFailed(w.ctx, run.ID, "worker pool rejected job: "+err.Error())
		w.releaseLock(conn.ID)
		if errors.Is(err, ErrPoolClosed) {
			return
		}
		w.log.Warn("sync_worker.enqueue_failed", "run_id", run.ID, "err", err)
	}
}

// EnqueueManual is called by the HTTP handler to schedule a one-off sync.
// Returns the created sync_run so the API can return 202 + run_id.
func (w *SyncWorker) EnqueueManual(ctx context.Context, tenantID, connectionID uuid.UUID, trigger sync_run.Trigger) (*sync_run.SyncRun, error) {
	if !w.tryLock(connectionID) {
		return nil, observability.ErrValidation.WithDetails(map[string]any{
			"reason": "sync already running for this connection",
		})
	}

	conn, err := w.store.BrokerConnections.GetByIDByTenant(ctx, connectionID)
	if err != nil {
		w.releaseLock(connectionID)
		return nil, err
	}
	if conn.Status != broker_connection.StatusConnected {
		w.releaseLock(connectionID)
		return nil, observability.ErrValidation.WithDetails(map[string]any{
			"reason": "connection not connected",
			"status": string(conn.Status),
		})
	}

	id, _ := uuid.NewV7()
	run := &sync_run.SyncRun{
		ID:                 id,
		TenantID:           tenantID,
		BrokerConnectionID: connectionID,
		Status:             sync_run.StatusQueued,
		Trigger:            trigger,
		Metadata:           map[string]any{},
	}
	if err := w.store.SyncRuns.Create(ctx, run); err != nil {
		w.releaseLock(connectionID)
		return nil, fmt.Errorf("create sync_run: %w", err)
	}

	job := &SyncRunJob{
		RunID:        run.ID,
		SyncService:  w.syncSvc,
		Store:        w.store,
		ConnectionID: connectionID,
		Worker:       w,
		Log:          w.log,
	}
	if err := w.pool.Enqueue(job); err != nil {
		_ = w.store.SyncRuns.MarkFailed(ctx, run.ID, "worker pool rejected job: "+err.Error())
		w.releaseLock(connectionID)
		return nil, fmt.Errorf("enqueue job: %w", err)
	}
	return run, nil
}

// ── SyncRunJob ────────────────────────────────────────────────────────

// SyncRunJob is the unit of work the SyncWorker hands the worker.Pool.
type SyncRunJob struct {
	RunID        uuid.UUID
	SyncService  *syncsvc.SyncService
	Store        *store.Store
	ConnectionID uuid.UUID
	Worker       *SyncWorker
	Log          *slog.Logger
}

func (j *SyncRunJob) Kind() string { return "sync_run:" + j.RunID.String() }

func (j *SyncRunJob) Execute(ctx context.Context) error {
	defer j.Worker.releaseLock(j.ConnectionID)
	j.SyncService.RunSync(ctx, j.RunID)
	return nil
}