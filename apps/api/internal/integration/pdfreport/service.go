// Package pdfreport generates Trade Report PDFs.
//
// Phase 3 ships a signintech/gopdf implementation (signintech.go) plus a
// minimal valid-PDF Stub (stub.go) used when the PDF feature is gated off
// or in tests that don't care about visual content.
package pdfreport

import (
	"context"
	"time"

	"github.com/gofrs/uuid"
)

// Service generates a trade-report PDF.
type Service interface {
	// Generate builds a PDF for the given account + period. The byte slice is
	// guaranteed to begin with the PDF magic "%PDF-" and end with "%%EOF"
	// so callers can stream it as `application/pdf` without further validation.
	Generate(ctx context.Context, req ReportRequest) ([]byte, error)
}

// ReportRequest is the input to Generate. TenantID is used for log keys and
// audit only — the caller is responsible for fetching the data and stuffing
// it into the Embed struct.
type ReportRequest struct {
	TenantID uuid.UUID
	Account  AccountSnapshot
	Period   Period
	Overview OverviewStats
	Equity   []EquityPoint
	Symbols  []SymbolRow
	Trades   []TradeRow
	IncludeEquityCurve bool
}

// AccountSnapshot is the denormalized account info the PDF header needs.
type AccountSnapshot struct {
	ID            uuid.UUID
	DisplayName   string
	Broker        string
	BrokerAcct    string
	Currency      string
}

// Period is the report's date range (inclusive start, exclusive end).
type Period struct {
	From time.Time
	To   time.Time
}

// OverviewStats is the 4-cell summary block at the top of the report.
type OverviewStats struct {
	TradeCount   int64
	NetPL        float64
	WinRate      float64
	ProfitFactor float64
}

// EquityPoint is a single point on the equity curve.
type EquityPoint struct {
	Time  time.Time
	Equity float64
}

// SymbolRow is one row of the symbol-breakdown table.
type SymbolRow struct {
	Symbol     string
	TradeCount  int64
	NetPL       float64
	WinRate     float64
}

// TradeRow is one row of the trade-list table.
type TradeRow struct {
	OpenTime  time.Time
	CloseTime time.Time
	Symbol    string
	Direction string
	NetPL     float64
	Outcome   string
}