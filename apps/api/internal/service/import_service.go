package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/parser"
	"github.com/trading-journal/api/internal/store"
)

// ImportService orchestrates the import flow per spec §11:
// Upload → Validate → Hash → Detect Format → Parse → Normalize →
// Validate → Deduplicate → Persist → Calculate Metrics → Complete
//
// Phase 1: runs synchronously after upload (no background worker yet).
type ImportService struct {
	store *store.Store
	log   *slog.Logger
}

func NewImportService(st *store.Store, log *slog.Logger) *ImportService {
	return &ImportService{store: st, log: log}
}

// ImportInput is the upload payload from the FE.
type ImportInput struct {
	TenantID         uuid.UUID
	UserID           uuid.UUID
	TradingAccountID uuid.UUID
	OriginalFilename string
	ByteSize         int64
	Content          []byte
}

// ImportResult is the response sent to FE.
type ImportResult struct {
	ImportedFile *imported_file.ImportedFile `json:"imported_file"`
	ImportJob    *import_job.ImportJob       `json:"import_job"`
	Trades       []TradeDTO                  `json:"trades"`
	Stats        ImportStats                 `json:"stats"`
}

// ImportStats summarizes the run.
type ImportStats struct {
	Parsed     int `json:"parsed"`
	Inserted   int `json:"inserted"`
	Skipped    int `json:"skipped"`
	TradeCount int `json:"trade_count"`
}

// TradeDTO is a slim representation of the inserted trade for FE display.
type TradeDTO struct {
	ID         uuid.UUID `json:"id"`
	Symbol     string    `json:"symbol"`
	Direction  string    `json:"direction"`
	OpenTime   time.Time `json:"open_time"`
	CloseTime  time.Time `json:"close_time"`
	NetPL      float64   `json:"net_p_l"`
	Outcome    string    `json:"outcome"`
}

// Run executes the full import pipeline.
func (s *ImportService) Run(ctx context.Context, in ImportInput) (*ImportResult, error) {
	// 1. Hash + dedup check
	sum := sha256.Sum256(in.Content)
	hash := hex.EncodeToString(sum[:])

	if existing, err := s.store.ImportedFiles.GetByContentHash(ctx, in.TenantID, hash); err == nil {
		// Duplicate — return existing record + 0 new rows
		job, _ := s.store.ImportJobs.GetByID(ctx, existing.ID)
		_ = job
		return &ImportResult{ImportedFile: existing, Stats: ImportStats{}}, nil
	}

	// 2. Validate format
	format := parser.Detect(in.Content)
	if format == parser.FormatUnknown {
		return nil, observability.ErrValidation.WithDetails(map[string]any{"reason": "unknown file format"})
	}

	// 3. Persist imported_files
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
			// Race: someone else inserted with the same hash just now. Re-fetch.
			if existing, ferr := s.store.ImportedFiles.GetByContentHash(ctx, in.TenantID, hash); ferr == nil {
				return &ImportResult{ImportedFile: existing, Stats: ImportStats{}}, nil
			}
		}
		return nil, fmt.Errorf("insert imported_file: %w", err)
	}

	// 4. Create import_job
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
	_ = s.store.ImportJobs.MarkRunning(ctx, job.ID)

	// 5. Parse
	var parseRes *parser.ParseResult
	var parseErr error
	switch format {
	case parser.FormatMT5HTML:
		parseRes, parseErr = parser.MT5HTMLParser{}.Parse(byteReader(in.Content))
	case parser.FormatMT5CSV, parser.FormatGenericCSV:
		parseRes, parseErr = parser.CSVParser{}.Parse(byteReader(in.Content))
	}
	if parseErr != nil {
		_ = s.store.ImportJobs.MarkFailed(ctx, job.ID, parseErr.Error())
		return nil, observability.ErrParseFailed.WithCause(parseErr)
	}
	if parseRes == nil || len(parseRes.Deals) == 0 {
		_ = s.store.ImportJobs.MarkCompleted(ctx, job.ID, 0, 0, 0, []map[string]any{{"message": "no deals parsed"}})
		return &ImportResult{ImportedFile: f, ImportJob: job, Trades: []TradeDTO{}, Stats: ImportStats{}}, nil
	}

	// 6. Persist deals (with dedup via UNIQUE constraint + ON CONFLICT DO NOTHING)
	parsed := len(parseRes.Deals)
	inserted := 0
	skipped := 0

	var tradeDTOs []TradeDTO

	if err := s.store.WithTx(ctx, func(tx pgx.Tx) error {
		dealRepo := s.store.Deals.WithTx(tx)
		tradeRepo := s.store.Trades.WithTx(tx)
		positionRepo := s.store.Positions.WithTx(tx)

		// Aggregate FIRST so we don't dedup the trades (we want the derived trades).
		trades := parser.Aggregate(parseRes.Deals)

		for _, pd := range parseRes.Deals {
			d := &deal.Deal{
				ID:               pd.ID,
				TenantID:         in.TenantID,
				TradingAccountID:  in.TradingAccountID,
				ImportJobID:      &job.ID,
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
			if !ok {
				skipped++
			} else {
				inserted++
			}
		}

		// Insert derived positions + trades
		for _, t := range trades {
			// Insert position
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
				Metadata:         map[string]any{},
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
	}); err != nil {
		_ = s.store.ImportJobs.MarkFailed(ctx, job.ID, err.Error())
		return nil, err
	}

	// 7. Mark completed + audit
	_ = s.store.ImportJobs.MarkCompleted(ctx, job.ID, parsed, inserted, skipped, parseErrorMaps(parseRes.Errors))
	_ = s.store.Audit.Insert(ctx, store.AuditEntry{
		ActorUserID: &in.UserID, ActorTenantID: &in.TenantID,
		Action: "import.completed",
		ResourceType: strPtr("import_job"), ResourceID: &job.ID,
		Metadata: map[string]any{
			"parsed": parsed, "inserted": inserted, "skipped": skipped,
			"trade_count": len(tradeDTOs),
		},
	})

	return &ImportResult{
		ImportedFile: f,
		ImportJob:    job,
		Trades:       tradeDTOs,
		Stats: ImportStats{
			Parsed:     parsed,
			Inserted:   inserted,
			Skipped:    skipped,
			TradeCount: len(tradeDTOs),
		},
	}, nil
}

// ── helpers ──────────────────────────────────────────────────────────

// byteReader adapts a []byte to io.Reader (cheap adapter).
func byteReader(b []byte) *bytesReader { return &bytesReader{b: b} }

type bytesReader struct {
	b []byte
	i int
}

func (r *bytesReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, ioEOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

// ioEOF alias to keep this file self-contained.
var ioEOF = errEOF{}

type errEOF struct{}

func (errEOF) Error() string { return "EOF" }

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