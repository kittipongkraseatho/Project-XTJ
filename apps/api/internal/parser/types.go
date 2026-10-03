// Package parser parses MT5 history exports (HTML + CSV) into normalized
// deal rows ready for aggregation into trades.
//
// Spec §11: "Upload → Validate → Hash → Detect Format → Parse → Normalize →
// Validate → Deduplicate → Persist → Calculate Metrics → Complete"
package parser

import (
	"io"
	"time"

	"github.com/gofrs/uuid"
)

// Deal is a single normalized MT5 fill. Maps directly to the `deals` table.
type Deal struct {
	ID             uuid.UUID
	ProviderDealID string    // MT5 ticket — unique per account
	Symbol         string
	Type           string // "buy" | "sell" | "balance" | ...
	EntryType      string // "entry" | "exit" | "inout" — drives aggregation
	Price         float64
	Volume        float64
	Commission    float64
	Swap          float64
	Profit        float64
	Magic         *int64
	Comment       string
	ExecutedAt    time.Time
}

// ParseError is a non-fatal row-level parse issue. The import continues
// but logs each to import_jobs.errors.
type ParseError struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}

// ParseResult is the per-format parser output.
type ParseResult struct {
	Deals  []Deal
	Errors []ParseError
}

// Detect returns the format hint based on file content (first 4 KB).
type Format string

const (
	FormatMT5HTML    Format = "mt5_html"
	FormatMT5CSV     Format = "mt5_csv"
	FormatGenericCSV Format = "generic_csv"
	FormatUnknown    Format = "unknown"
)

// Parser is the contract every concrete parser satisfies.
type Parser interface {
	Format() Format
	Parse(reader io.Reader) (*ParseResult, error)
}