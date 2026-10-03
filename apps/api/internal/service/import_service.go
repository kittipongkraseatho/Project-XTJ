package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/trading-journal/api/internal/domain/deal"
	"github.com/trading-journal/api/internal/domain/import_job"
	"github.com/trading-journal/api/internal/domain/imported_file"
	"github.com/trading-journal/api/internal/domain/position"
	"github.com/trading-journal/api/internal/domain/trade"
	whdomain "github.com/trading-journal/api/internal/domain/webhook"
	whsvc "github.com/trading-journal/api/internal/integration/webhook"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/parser"
	"github.com/trading-journal/api/internal/store"
	"github.com/trading-journal/api/internal/worker"
)

// ImportService orchestrates the import flow per spec §11:
// Upload → Validate → Hash → Detect Format → Parse → Normalize →
// Validate → Deduplicate → Persist → Calculate Metrics → Complete
type ImportService struct {
	store      *store.Store
	log        *slog.Logger
	pool       *worker.Pool
	webhookSvc whsvc.Service // Phase 5.2 — optional; nil = no events
}

func NewImportService(st *store.Store, log *slog.Logger) *ImportService {
	return &ImportService{store: st, log: log}
}

// AttachPool wires a worker.Pool so Enqueue() can push jobs.
func (s *ImportService) AttachPool(p *worker.Pool) { s.pool = p }

// AttachWebhookService wires the webhook emitter. Caller owns the
// lifetime; pass nil to disable events.
func (s *ImportService) AttachWebhookService(w whsvc.Service) { s.webhookSvc = w }

type ImportInput struct {
	TenantID         uuid.UUID
	UserID           uuid.UUID
	TradingAccountID uuid.UUID
	OriginalFilename string
	ByteSize         int64
	Content          []byte
}

type TradeDTO struct {
	ID        uuid.UUID `json:"id"`
	Symbol    string    `json:"symbol"`
	Direction string    `json:"direction"`
	OpenTime  time.Time `json:"open_time"`
	CloseTime time.Time `json:"close_time"`
	NetPL     float64   `json:"net_p_l"`
	Outcome   string    `json:"outcome"`
}

type ImportStats struct {
	Parsed     int `json:"parsed"`
	Inserted   int `json:"inserted"`
	Skipped    int `json:"skipped"`
	TradeCount int `json:"trade_count"`
}

type ImportResult struct {
	ImportedFile *imported_file.ImportedFile `json:"imported_file"`
	ImportJob    *import_job.ImportJob       `json:"import_job"`
	Trades       []TradeDTO                  `json:"trades"`
	Stats        ImportStats                 `json:"stats"`
}

// ── Async entrypoint (Phase 1 §10 step 17) ────────────────────────────

// Enqueue runs the dedup + format-detect + row-creation synchronously (so we
// can return 201 + import_job_id immediately), then queues the heavy work to
// the worker pool. Returns the queued job + import_file records; trades arrive
// later via polling /v1/trades.
//
// If no pool is attached, falls back to synchronous Run() (dev mode).
func (s *ImportService) Enqueue(ctx context.Context, in ImportInput) (*ImportResult, error) {
	// Phase 1: hash + dedup check (synchronous, fast)
	sum := sha256.Sum256(in.Content)
	hash := hex.EncodeToString(sum[:])
	if existing, err := s.store.ImportedFiles.GetByContentHash(ctx, in.TenantID, hash); err == nil {
		job, _ := s.store.ImportJobs.GetByID(ctx, existing.ID)
		return &ImportResult{ImportedFile: existing, ImportJob: job, Trades: []TradeDTO{}, Stats: ImportStats{}}, nil
	}

	format := parser.Detect(in.Content)
	if format == parser.FormatUnknown {
		return nil, observability.ErrValidation.WithDetails(map[string]any{"reason": "unknown file format"})
	}

	f := &imported_file.ImportedFile{
		ID:               uuid.Must(uuid.NewV7()),
		TenantID:         in.TenantID,
		TradingAccountID: in.TradingAccountID,
		UserID:           in.UserID,
		Format:           importedFileFormat(string(format)),
		OriginalFilename: in.OriginalFilename,
		ByteSize:         in.ByteSize,
		ContentHash:      hash,
		Metadata:         map[string]any{},
	}
	if err := s.store.ImportedFiles.Create(ctx, f); err != nil {
		if store.IsUniqueViolation(err) {
			if existing, ferr := s.store.ImportedFiles.GetByContentHash(ctx, in.TenantID, hash); ferr == nil {
				job, _ := s.store.ImportJobs.GetByID(ctx, existing.ID)
				return &ImportResult{ImportedFile: existing, ImportJob: job}, nil
			}
		}
		return nil, fmt.Errorf("insert imported_file: %w", err)
	}

	job := &import_job.ImportJob{
		ID:             uuid.Must(uuid.NewV7()),
		TenantID:       in.TenantID,
		ImportedFileID: f.ID,
		Status:         import_job.StatusQueued,
		Errors:         []map[string]any{},
	}
	if err := s.store.ImportJobs.Create(ctx, job); err != nil {
		return nil, fmt.Errorf("insert import_job: %w", err)
	}

	// If worker pool not wired, do synchronous fallback.
	if s.pool == nil {
		_ = s.store.ImportJobs.MarkRunning(ctx, job.ID)
		_, _, err := s.RunPipeline(ctx, job.ID, in, f, format)
		if err != nil {
			return nil, err
		}
		// Reload to get final state.
		final, _ := s.store.ImportJobs.GetByID(ctx, job.ID)
		return &ImportResult{ImportedFile: f, ImportJob: final, Trades: []TradeDTO{}}, nil
	}

	// Queue async work — capture the heavy stuff in a closure.
	jobID := job.ID
	fileID := f.ID
	tenantID := in.TenantID
	accountID := in.TradingAccountID
	userID := in.UserID
	filename := in.OriginalFilename
	content := in.Content

	payload := &worker.ImportJobPayload{
		ImportJobID: jobID.String(),
		Handler: func(ctx context.Context) error {
			innerIn := ImportInput{
				TenantID:         tenantID,
				UserID:           userID,
				TradingAccountID: accountID,
				OriginalFilename: filename,
				ByteSize:         int64(len(content)),
				Content:          content,
			}
			f2, err := s.store.ImportedFiles.GetByContentHash(ctx, tenantID, hash)
			if err != nil || f2 == nil {
				return fmt.Errorf("imported_file %s not found", fileID)
			}
			detected := parser.Detect(content)
			_, _, err = s.RunPipeline(ctx, jobID, innerIn, f2, detected)
			return err
		},
	}
	if err := s.pool.Enqueue(payload); err != nil {
		return nil, fmt.Errorf("enqueue import: %w", err)
	}

	return &ImportResult{ImportedFile: f, ImportJob: job, Trades: []TradeDTO{}}, nil
}

// RunPipeline executes the parse + persist steps. Caller is responsible for
// creating the import_job row + marking running.
func (s *ImportService) RunPipeline(ctx context.Context, jobID uuid.UUID, in ImportInput, f *imported_file.ImportedFile, format parser.Format) (*ImportResult, []TradeDTO, error) {
	_ = s.store.ImportJobs.MarkRunning(ctx, jobID)

	var parseRes *parser.ParseResult
	var parseErr error
	switch format {
	case parser.FormatMT5HTML:
		parseRes, parseErr = parser.MT5HTMLParser{}.Parse(byteReader(in.Content))
	case parser.FormatMT5CSV, parser.FormatGenericCSV:
		parseRes, parseErr = parser.CSVParser{}.Parse(byteReader(in.Content))
	}
	if parseErr != nil {
		_ = s.store.ImportJobs.MarkFailed(ctx, jobID, parseErr.Error())
		return nil, nil, observability.ErrParseFailed.WithCause(parseErr)
	}
	if parseRes == nil || len(parseRes.Deals) == 0 {
		_ = s.store.ImportJobs.MarkCompleted(ctx, jobID, 0, 0, 0, []map[string]any{{"message": "no deals parsed"}})
		return &ImportResult{ImportedFile: f, Stats: ImportStats{}}, nil, nil
	}

	parsed := len(parseRes.Deals)
	inserted, skipped := 0, 0
	var tradeDTOs []TradeDTO

	err := s.store.WithTx(ctx, func(tx pgx.Tx) error {
		dealRepo := s.store.Deals.WithTx(tx)
		tradeRepo := s.store.Trades.WithTx(tx)
		positionRepo := s.store.Positions.WithTx(tx)

		trades := parser.Aggregate(parseRes.Deals)

		for _, pd := range parseRes.Deals {
			d := &deal.Deal{
				ID:               pd.ID,
				TenantID:         in.TenantID,
				TradingAccountID: in.TradingAccountID,
				ImportJobID:      &jobID,
				ProviderDealID:   pd.ProviderDealID,
				Symbol:           pd.Symbol,
				Type:             deal.Type(pd.Type),
				EntryType:        deal.EntryType(pd.EntryType),
				Volume:           pd.Volume,
				Price:            pd.Price,
				Commission:       pd.Commission,
				Swap:             pd.Swap,
				Profit:           pd.Profit,
				Magic:            pd.Magic,
				ExecutedAt:       pd.ExecutedAt,
				Metadata:         map[string]any{},
			}
			if pd.Comment != "" {
				c := pd.Comment
				d.Comment = &c
			}
			ok, err := dealRepo.Insert(ctx, d)
			if err != nil {
				return fmt.Errorf("insert deal %s: %w", pd.ProviderDealID, err)
			}
			if ok {
				inserted++
			} else {
				skipped++
			}
		}

		for _, t := range trades {
			posID := uuid.Must(uuid.NewV7())
			closeTime := t.CloseTime
			openTime := t.OpenTime
			state := position.StateClosed
			if t.CloseTime.IsZero() {
				state = position.StateOpen
			}
			closePrice := t.ClosePrice
			pos := &position.Position{
				ID:                 posID,
				TenantID:           in.TenantID,
				TradingAccountID:   in.TradingAccountID,
				ProviderPositionID: t.EntryDealID,
				Direction:          position.Direction(t.Direction),
				Symbol:             t.Symbol,
				Volume:             t.Volume,
				OpenPrice:          t.OpenPrice,
				ClosePrice:         &closePrice,
				OpenTime:           openTime,
				CloseTime:          &closeTime,
				State:              state,
				RealizedPL:         t.NetPL,
				Commission:         t.Commission,
				Swap:               t.Swap,
				Magic:              t.Magic,
				Metadata:           map[string]any{},
			}
			if err := positionRepo.Insert(ctx, pos); err != nil {
				return fmt.Errorf("insert position: %w", err)
			}

			holdingSec := t.HoldingSec
			closePriceV := t.ClosePrice
			tr := &trade.Trade{
				ID:               t.ID,
				TenantID:         in.TenantID,
				UserID:           in.UserID,
				TradingAccountID: in.TradingAccountID,
				PositionID:       &posID,
				Symbol:           t.Symbol,
				Direction:        trade.Direction(t.Direction),
				Volume:           t.Volume,
				OpenPrice:        t.OpenPrice,
				ClosePrice:       &closePriceV,
				OpenTime:         t.OpenTime,
				CloseTime:        &closeTime,
				HoldingSeconds:   &holdingSec,
				GrossProfit:      t.GrossProfit,
				GrossLoss:        t.GrossLoss,
				NetPL:            t.NetPL,
				Commission:       t.Commission,
				Swap:             t.Swap,
				Magic:            t.Magic,
				Outcome:          trade.Outcome(t.Outcome),
				IsPartial:        t.IsPartial,
				Metadata:         nil,
			}
			if tr.Metadata == nil {
				tr.Metadata = map[string]any{}
			}
			if err := tradeRepo.Insert(ctx, tr); err != nil {
				return fmt.Errorf("insert trade: %w", err)
			}
			tradeDTOs = append(tradeDTOs, TradeDTO{
				ID:        tr.ID,
				Symbol:    tr.Symbol,
				Direction: string(tr.Direction),
				OpenTime:  tr.OpenTime,
				CloseTime: *tr.CloseTime,
				NetPL:     tr.NetPL,
				Outcome:   string(tr.Outcome),
			})
		}

		return nil
	})
	if err != nil {
		_ = s.store.ImportJobs.MarkFailed(ctx, jobID, err.Error())
		return nil, nil, err
	}

	_ = s.store.ImportJobs.MarkCompleted(ctx, jobID, parsed, inserted, skipped, parseErrorMaps(parseRes.Errors))
	_ = s.store.Audit.Insert(ctx, store.AuditEntry{
		ActorUserID: &in.UserID, ActorTenantID: &in.TenantID,
		Action: "import.completed",
		ResourceType: strPtr("import_job"), ResourceID: &jobID,
		Metadata: map[string]any{
			"parsed": parsed, "inserted": inserted, "skipped": skipped,
			"trade_count": len(tradeDTOs),
		},
	})

	// Phase 5.2 — emit webhook events (best-effort; failures don't fail
	// the import).
	s.emitWebhookEvents(ctx, in, jobID, tradeDTOs, parsed, inserted, skipped)

	return &ImportResult{
		ImportedFile: f,
		Stats:        ImportStats{Parsed: parsed, Inserted: inserted, Skipped: skipped, TradeCount: len(tradeDTOs)},
	}, tradeDTOs, nil
}

// emitWebhookEvents fires `import.completed` once and one `trade.created`
// per newly-inserted trade. All Emit calls are best-effort: errors are
// logged at WARN and never block the import.
func (s *ImportService) emitWebhookEvents(ctx context.Context, in ImportInput, jobID uuid.UUID, trades []TradeDTO, parsed, inserted, skipped int) {
	if s.webhookSvc == nil {
		return
	}
	if err := s.webhookSvc.Emit(ctx, whsvc.EmitInput{
		TenantID:  in.TenantID,
		EventType: whdomain.EventImportCompleted,
		Data: map[string]any{
			"import_job_id":      jobID,
			"trading_account_id": in.TradingAccountID,
			"deals_parsed":       parsed,
			"deals_inserted":     inserted,
			"deals_skipped":      skipped,
			"trades_derived":     len(trades),
		},
	}); err != nil {
		s.log.Warn("webhook emit import.completed failed",
			slog.String("err", err.Error()))
	}
	for _, t := range trades {
		if err := s.webhookSvc.Emit(ctx, whsvc.EmitInput{
			TenantID:  in.TenantID,
			EventType: whdomain.EventTradeCreated,
			Data: map[string]any{
				"trade_id":           t.ID,
				"trading_account_id": in.TradingAccountID,
				"symbol":             t.Symbol,
				"direction":          t.Direction,
				"open_time":          t.OpenTime,
				"close_time":         t.CloseTime,
				"net_pl":             t.NetPL,
				"outcome":            t.Outcome,
			},
		}); err != nil {
			s.log.Warn("webhook emit trade.created failed",
				slog.String("trade_id", t.ID.String()),
				slog.String("err", err.Error()))
		}
	}
}

// Run is the synchronous entrypoint (still exported for sync fallback).
func (s *ImportService) Run(ctx context.Context, in ImportInput) (*ImportResult, error) {
	return s.Enqueue(ctx, in)
}

// ── helpers ──────────────────────────────────────────────────────────

func byteReader(b []byte) *bytesReader { return &bytesReader{b: b} }

type bytesReader struct {
	b []byte
	i int
}

func (r *bytesReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, errEOFIm{}
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

type errEOFIm struct{}

func (errEOFIm) Error() string { return "EOF" }

func importedFileFormat(s string) imported_file.Format {
	switch s {
	case string(parser.FormatMT5HTML):
		return imported_file.FormatMT5HTML
	case string(parser.FormatMT5CSV):
		return imported_file.FormatMT5CSV
	default:
		return imported_file.FormatGenericCSV
	}
}

func parseErrorMaps(errs []parser.ParseError) []map[string]any {
	out := make([]map[string]any, len(errs))
	for i, e := range errs {
		out[i] = map[string]any{"row": e.Row, "message": e.Message}
	}
	return out
}

func strPtr(s string) *string { return &s }

var _ = errors.Is
var _ = time.Second