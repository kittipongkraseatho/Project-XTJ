// Package report_schedule contains the ScheduleService + ScheduleWorker that
// power Phase 4's email report delivery.
//
// Lives in service/ (not service/report_schedule) to avoid colliding with
// the existing service package's Go file layout. Keeping it here means the
// import path matches the directory name.
package report_schedule

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/report_schedule"
	"github.com/trading-journal/api/internal/integration/mailer"
	"github.com/trading-journal/api/internal/integration/pdfreport"
	"github.com/trading-journal/api/internal/reportbuild"
	"github.com/trading-journal/api/internal/store"
)

// ScheduleService orchestrates report-schedule CRUD + the worker poll that
// fires due schedules.
type ScheduleService struct {
	store *store.Store
	log   *slog.Logger
}

func NewScheduleService(st *store.Store, log *slog.Logger) *ScheduleService {
	return &ScheduleService{store: st, log: log}
}

// CreateInput is the user-facing request shape.
type CreateInput struct {
	TenantID         uuid.UUID
	TradingAccountID uuid.UUID
	Frequency        report_schedule.Frequency
	HourUTC          int
	Weekday          *int
	DayOfMonth       *int
	Recipients       []string
}

func (s *ScheduleService) Create(ctx context.Context, in CreateInput) (*report_schedule.ReportSchedule, error) {
	if !in.Frequency.IsValid() {
		return nil, errors.New("invalid frequency")
	}
	if in.HourUTC < 0 || in.HourUTC > 23 {
		return nil, errors.New("hour_utc must be 0..23")
	}
	if in.Frequency == report_schedule.FrequencyWeekly && (in.Weekday == nil || *in.Weekday < 0 || *in.Weekday > 6) {
		return nil, errors.New("weekly schedules require weekday (0..6)")
	}
	if in.Frequency == report_schedule.FrequencyMonthly && (in.DayOfMonth == nil || *in.DayOfMonth < 1 || *in.DayOfMonth > 28) {
		return nil, errors.New("monthly schedules require day_of_month (1..28)")
	}

	id, _ := uuid.NewV7()
	rec := &report_schedule.ReportSchedule{
		ID:               id,
		TenantID:         in.TenantID,
		TradingAccountID: in.TradingAccountID,
		Frequency:         in.Frequency,
		HourUTC:           in.HourUTC,
		Weekday:           in.Weekday,
		DayOfMonth:        in.DayOfMonth,
		NextSendAt:        ComputeNextSend(in.Frequency, in.HourUTC, in.Weekday, in.DayOfMonth, time.Now().UTC()),
		Recipients:        in.Recipients,
		IsActive:          true,
		Metadata:          map[string]any{},
	}
	if err := s.store.ReportSchedules.Create(ctx, rec); err != nil {
		return nil, fmt.Errorf("create schedule: %w", err)
	}
	return rec, nil
}

func (s *ScheduleService) ListByTenant(ctx context.Context, tenantID uuid.UUID) ([]*report_schedule.ReportSchedule, error) {
	return s.store.ReportSchedules.ListByTenant(ctx, tenantID)
}

func (s *ScheduleService) Get(ctx context.Context, id uuid.UUID) (*report_schedule.ReportSchedule, error) {
	return s.store.ReportSchedules.GetByIDByTenant(ctx, id)
}

func (s *ScheduleService) Delete(ctx context.Context, id uuid.UUID) error {
	// ownership check
	if _, err := s.store.ReportSchedules.GetByIDByTenant(ctx, id); err != nil {
		return err
	}
	return s.store.ReportSchedules.Delete(ctx, id)
}

// ── ComputeNextSend ───────────────────────────────────────────────────

// ComputeNextSend returns the next UTC timestamp at which the schedule
// should fire, given the current time. Pure function (no DB / I/O) so
// tests can pin a clock.
func ComputeNextSend(freq report_schedule.Frequency, hourUTC int, weekday *int, dayOfMonth *int, now time.Time) time.Time {
	now = now.UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), hourUTC, 0, 0, 0, time.UTC)

	switch freq {
	case report_schedule.FrequencyDaily:
		if today.After(now) {
			return today
		}
		return today.AddDate(0, 0, 1)

	case report_schedule.FrequencyWeekly:
		w := 0
		if weekday != nil {
			w = *weekday
		}
		// Days until target: if today matches but the time has passed,
		// move to next week.
		daysUntil := (w - int(now.Weekday()) + 7) % 7
		candidate := today.AddDate(0, 0, daysUntil)
		if !candidate.After(now) {
			candidate = candidate.AddDate(0, 0, 7)
		}
		return candidate

	case report_schedule.FrequencyMonthly:
		d := 1
		if dayOfMonth != nil {
			d = *dayOfMonth
		}
		candidate := time.Date(now.Year(), now.Month(), d, hourUTC, 0, 0, 0, time.UTC)
		if !candidate.After(now) {
			candidate = candidate.AddDate(0, 1, 0)
		}
		return candidate
	}
	return today.AddDate(0, 0, 1)
}

// ── Delivery (used by both cron and the worker) ────────────────────────

// SendDelivery is the input for sending one report email.
type SendDelivery struct {
	Schedule report_schedule.ReportSchedule
	Account  AccountInfo
	// Recipients override (empty → tenant owner email)
	Recipients []string
}

// AccountInfo is the denormalized account snapshot the email + report need.
type AccountInfo struct {
	ID          uuid.UUID
	DisplayName string
	Broker      string
}

// Deliver generates the PDF and emails it. Returns the next_send_at to
// persist on the schedule row.
func (s *ScheduleService) Deliver(ctx context.Context, pdfSvc pdfreport.Service, mail mailer.Service, d SendDelivery) (time.Time, error) {
	// Build the PDF embed.
	periodEnd := time.Now().UTC()
	var periodStart time.Time
	switch d.Schedule.Frequency {
	case report_schedule.FrequencyDaily:
		periodStart = periodEnd.AddDate(0, 0, -1)
	case report_schedule.FrequencyWeekly:
		periodStart = periodEnd.AddDate(0, 0, -7)
	case report_schedule.FrequencyMonthly:
		periodStart = periodEnd.AddDate(0, -1, 0)
	}

	req, err := reportbuild.BuildReportRequest(
		ctx, s.store,
		d.Schedule.TenantID, d.Account.ID,
		periodStart, periodEnd,
		pdfreport.AccountSnapshot{
			ID:          d.Account.ID,
			DisplayName: d.Account.DisplayName,
			Broker:      d.Account.Broker,
			BrokerAcct:  "",
			Currency:    "USD",
		},
		true,
	)
	if err != nil {
		return time.Time{}, fmt.Errorf("build report request: %w", err)
	}

	pdfBytes, err := pdfSvc.Generate(ctx, req)
	if err != nil {
		return time.Time{}, fmt.Errorf("pdf generate: %w", err)
	}

	// Email.
	if len(d.Recipients) == 0 {
		d.Recipients = []string{"owner@trading-journal.local"} // stub fallback
	}
	subject := fmt.Sprintf("Trading Journal report — %s — %s",
		d.Account.DisplayName, periodEnd.Format("2006-01-02"))
	msg := mailer.Message{
		From:    "Trading Journal <noreply@trading-journal.local>",
		To:      d.Recipients,
		Subject: subject,
		TextBody: fmt.Sprintf("Your scheduled Trading Journal report is ready (account: %s, period: %s → %s). PDF attached.",
			d.Account.DisplayName, periodStart.Format("2006-01-02"), periodEnd.Format("2006-01-02")),
		Attachments: []mailer.Attachment{{
			Filename: fmt.Sprintf("trading-journal_%s_%s.pdf",
				d.Account.DisplayName, periodEnd.Format("2006-01-02")),
			MimeType: "application/pdf",
			Data:     pdfBytes,
		}},
	}
	if err := mail.Send(ctx, msg); err != nil {
		return time.Time{}, fmt.Errorf("mail send: %w", err)
	}

	// Compute next.
	now := time.Now().UTC()
	next := ComputeNextSend(d.Schedule.Frequency, d.Schedule.HourUTC, d.Schedule.Weekday, d.Schedule.DayOfMonth, now)
	return next, nil
}