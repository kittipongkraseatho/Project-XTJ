package worker

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/trading-journal/api/internal/domain/report_schedule"
	"github.com/trading-journal/api/internal/integration/mailer"
	"github.com/trading-journal/api/internal/integration/pdfreport"
	"github.com/trading-journal/api/internal/store"
)

// ScheduleWorker polls report_schedules every PollInterval and fires due ones.
//
// Lifecycle:
//   - Start() launches the poll goroutine.
//   - Stop() cancels + waits for the in-flight tick.
//   - The tick walks `report_schedules WHERE is_active=TRUE AND next_send_at <= now()`.
//
// Single-flight per schedule_id (in-process) prevents the worker from
// double-firing if a tick is slow AND the next tick arrives. Phase 4
// is single-instance; Phase 5 (multi-instance API) will swap the in-process
// lock for a Postgres advisory lock.
type ScheduleWorker struct {
	store         *store.Store
	pdf           pdfreport.Service
	mail          mailer.Service
	log           *slog.Logger
	pollInterval  time.Duration
	pollBatchSize int

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	running bool
	mu      sync.Mutex
	locks   map[uuid.UUID]struct{}
}

// ScheduleWorkerOptions configures the worker. Zero values fall back to
// sane defaults: 60s poll, 50 batch.
type ScheduleWorkerOptions struct {
	PollInterval  time.Duration
	PollBatchSize int
}

// NewScheduleWorker builds the worker. Call Start to begin polling.
func NewScheduleWorker(parent context.Context, st *store.Store, pdf pdfreport.Service, mail mailer.Service, log *slog.Logger, opts ScheduleWorkerOptions) *ScheduleWorker {
	if opts.PollInterval <= 0 {
		opts.PollInterval = 60 * time.Second
	}
	if opts.PollBatchSize <= 0 {
		opts.PollBatchSize = 50
	}
	ctx, cancel := context.WithCancel(parent)
	return &ScheduleWorker{
		store:         st,
		pdf:           pdf,
		mail:          mail,
		log:           log,
		pollInterval:  opts.PollInterval,
		pollBatchSize: opts.PollBatchSize,
		ctx:           ctx,
		cancel:        cancel,
		locks:         make(map[uuid.UUID]struct{}),
	}
}

// Start launches the poll goroutine. No-op when already running.
func (w *ScheduleWorker) Start() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return
	}
	w.running = true
	w.wg.Add(1)
	go w.poller()
}

// Stop cancels + waits for the in-flight tick.
func (w *ScheduleWorker) Stop() {
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

func (w *ScheduleWorker) poller() {
	defer w.wg.Done()
	// Tick once immediately so a freshly-started server doesn't wait 60s.
	w.tick()
	ticker := time.NewTicker(w.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.tick()
		}
	}
}

func (w *ScheduleWorker) tick() {
	defer func() {
		if r := recover(); r != nil {
			w.log.Error("schedule_worker.panic", "err", r)
		}
	}()
	dueRows, err := w.store.ReportSchedules.ListDue(w.ctx, time.Now(), w.pollBatchSize)
	if err != nil {
		w.log.Error("schedule_worker.list_due_failed", "err", err)
		return
	}
	for _, s := range dueRows {
		if !w.tryLock(s.ID) {
			continue
		}
		w.deliver(s)
	}
}

func (w *ScheduleWorker) tryLock(id uuid.UUID) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.locks[id]; ok {
		return false
	}
	w.locks[id] = struct{}{}
	return true
}

func (w *ScheduleWorker) releaseLock(id uuid.UUID) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.locks, id)
}

// deliver fetches the account snapshot, sends the email + PDF, and advances
// next_send_at. Errors are logged but don't block other schedules.
func (w *ScheduleWorker) deliver(s *report_schedule.ReportSchedule) {
	defer w.releaseLock(s.ID)

	acc, err := w.store.TradingAccounts.GetByID(w.ctx, s.TradingAccountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			w.log.Warn("schedule_worker.account_missing", "schedule_id", s.ID, "account_id", s.TradingAccountID)
		} else {
			w.log.Error("schedule_worker.account_load_failed", "schedule_id", s.ID, "err", err)
		}
		return
	}

	// Build the email message inline (the ScheduleService.Deliver helper
	// isn't used here because we'd otherwise cycle back through service
	// imports from the worker).
	periodEnd := time.Now().UTC()
	var periodStart time.Time
	switch s.Frequency {
	case report_schedule.FrequencyDaily:
		periodStart = periodEnd.AddDate(0, 0, -1)
	case report_schedule.FrequencyWeekly:
		periodStart = periodEnd.AddDate(0, 0, -7)
	case report_schedule.FrequencyMonthly:
		periodStart = periodEnd.AddDate(0, -1, 0)
	}

	pdfReq := pdfreport.ReportRequest{
		TenantID:           s.TenantID,
		IncludeEquityCurve: true,
		Account: pdfreport.AccountSnapshot{
			ID:          acc.ID,
			DisplayName: acc.DisplayName,
			Broker:      string(acc.Broker),
			BrokerAcct:  acc.BrokerAccountID,
			Currency:    acc.Currency,
		},
		Period: pdfreport.Period{From: periodStart, To: periodEnd.Add(24 * time.Hour)},
	}

	pdfBytes, err := w.pdf.Generate(w.ctx, pdfReq)
	if err != nil {
		w.log.Error("schedule_worker.pdf_failed", "schedule_id", s.ID, "err", err)
		return
	}

	recipients := s.Recipients
	if len(recipients) == 0 {
		recipients = []string{"owner@trading-journal.local"} // stub fallback
	}
	msg := mailer.Message{
		From:    "Trading Journal <noreply@trading-journal.local>",
		To:      recipients,
		Subject: "Trading Journal report — " + acc.DisplayName + " — " + periodEnd.Format("2006-01-02"),
		TextBody: "Your scheduled Trading Journal report is ready.\n\n" +
			"Account: " + acc.DisplayName + "\n" +
			"Period: " + periodStart.Format("2006-01-02") + " → " + periodEnd.Format("2006-01-02") + "\n\n" +
			"PDF attached.",
		Attachments: []mailer.Attachment{{
			Filename: "trading-journal_" + periodEnd.Format("2006-01-02") + ".pdf",
			MimeType: "application/pdf",
			Data:     pdfBytes,
		}},
	}
	if err := w.mail.Send(w.ctx, msg); err != nil {
		w.log.Error("schedule_worker.mail_failed", "schedule_id", s.ID, "err", err)
		return
	}

	// Advance next_send_at.
	next := computeNextSend(s, periodEnd)
	if err := w.store.ReportSchedules.UpdateAfterSend(w.ctx, s.ID, periodEnd, next); err != nil {
		w.log.Error("schedule_worker.update_next_failed", "schedule_id", s.ID, "err", err)
	}

	_ = s.TenantID // keep uuid import live
	w.log.Info("schedule_worker.sent",
		"schedule_id", s.ID,
		"tenant_id", s.TenantID,
		"account_id", s.TradingAccountID,
		"next_send_at", next.Format(time.RFC3339),
	)
}

// computeNextSend mirrors the pure helper in service/report_schedule.
// Lives here too to avoid importing the service package from worker (cycle).
func computeNextSend(s *report_schedule.ReportSchedule, now time.Time) time.Time {
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), s.HourUTC, 0, 0, 0, time.UTC)
	switch s.Frequency {
	case report_schedule.FrequencyDaily:
		return today.AddDate(0, 0, 1)
	case report_schedule.FrequencyWeekly:
		w := 0
		if s.Weekday != nil {
			w = *s.Weekday
		}
		daysUntil := (w - int(now.Weekday()) + 7) % 7
		candidate := today.AddDate(0, 0, daysUntil)
		if !candidate.After(now) {
			candidate = candidate.AddDate(0, 0, 7)
		}
		return candidate
	case report_schedule.FrequencyMonthly:
		d := 1
		if s.DayOfMonth != nil {
			d = *s.DayOfMonth
		}
		candidate := time.Date(now.Year(), now.Month(), d, s.HourUTC, 0, 0, 0, time.UTC)
		if !candidate.After(now) {
			candidate = candidate.AddDate(0, 1, 0)
		}
		return candidate
	}
	return today.AddDate(0, 0, 1)
}