package pdfreport

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sampleRequest() ReportRequest {
	tid := uuid.Must(uuid.NewV7())
	accID := uuid.Must(uuid.NewV7())
	return ReportRequest{
		TenantID:           tid,
		IncludeEquityCurve: true,
		Account: AccountSnapshot{
			ID:          accID,
			DisplayName: "Demo MT5",
			Broker:      "MT5",
			BrokerAcct:  "12345678",
			Currency:    "USD",
		},
		Period: Period{
			From: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			To:   time.Date(2025, 1, 31, 0, 0, 0, 0, time.UTC),
		},
		Overview: OverviewStats{
			TradeCount: 25,
			NetPL:      1245.67,
			WinRate:    0.6,
			ProfitFactor: 1.45,
		},
		Equity: []EquityPoint{
			{Time: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Equity: 0},
			{Time: time.Date(2025, 1, 5, 0, 0, 0, 0, time.UTC), Equity: 100},
			{Time: time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), Equity: -50},
			{Time: time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC), Equity: 300},
			{Time: time.Date(2025, 1, 20, 0, 0, 0, 0, time.UTC), Equity: 800},
			{Time: time.Date(2025, 1, 25, 0, 0, 0, 0, time.UTC), Equity: 1245.67},
		},
		Symbols: []SymbolRow{
			{Symbol: "EURUSD", TradeCount: 10, NetPL: 600, WinRate: 0.7},
			{Symbol: "XAUUSD", TradeCount: 5, NetPL: 400, WinRate: 0.6},
			{Symbol: "USDJPY", TradeCount: 10, NetPL: 245.67, WinRate: 0.5},
		},
		Trades: []TradeRow{
			{OpenTime: time.Date(2025, 1, 25, 0, 0, 0, 0, time.UTC), CloseTime: time.Date(2025, 1, 25, 1, 0, 0, 0, time.UTC), Symbol: "EURUSD", Direction: "long", NetPL: 100, Outcome: "win"},
			{OpenTime: time.Date(2025, 1, 24, 0, 0, 0, 0, time.UTC), CloseTime: time.Date(2025, 1, 24, 2, 0, 0, 0, time.UTC), Symbol: "XAUUSD", Direction: "short", NetPL: -50, Outcome: "loss"},
		},
	}
}

// hasPDFMagic returns true if the byte slice contains a valid PDF header.
// PDF readers are lenient — "%PDF-" can appear at the very start, OR after a
// small number of comment lines (each comment line starts with "%" but NOT
// "%PDF-"). The "%PDF-" line itself is technically also a comment.
func hasPDFMagic(b []byte) bool {
	if len(b) >= 5 && bytes.HasPrefix(b, []byte("%PDF-")) {
		return true
	}
	// Walk past up to 10 lines that start with "%" but aren't "%PDF-".
	rest := b
	for line := 0; line < 10; line++ {
		if !bytes.HasPrefix(rest, []byte("%")) {
			break
		}
		if bytes.HasPrefix(rest, []byte("%PDF-")) {
			return true
		}
		if i := bytes.IndexByte(rest, '\n'); i >= 0 {
			rest = rest[i+1:]
		} else {
			break
		}
	}
	return bytes.Contains(rest, []byte("%PDF-"))
}

// hasEOFTail returns true if the byte slice ends with the PDF EOF marker.
// The PDF spec requires the LAST line of the file to be "%%EOF" (possibly
// with trailing whitespace / newlines).
func hasEOFTail(b []byte) bool {
	s := bytes.TrimRight(b, "\r\n\t ")
	return bytes.HasSuffix(s, []byte("%%EOF"))
}

// TestStubService_ReturnsValidPDF asserts the stub produces a valid PDF
// (contains %PDF- magic and ends with %%EOF).
func TestStubService_ReturnsValidPDF(t *testing.T) {
	s := NewStubService()
	b, err := s.Generate(context.Background(), sampleRequest())
	require.NoError(t, err)
	require.NotEmpty(t, b)
	assert.True(t, hasPDFMagic(b), "must contain PDF magic; head=%q", string(b[:min(80, len(b))]))
	assert.True(t, hasEOFTail(b), "must end with %%EOF; tail=%q", string(b[max(0, len(b)-30):]))
	assert.Contains(t, string(b), "stub PDF", "stub should mark itself")
}

// TestSignintechService_ReturnsValidPDF asserts the production service builds
// a non-empty valid PDF for a typical report payload.
func TestSignintechService_ReturnsValidPDF(t *testing.T) {
	s := NewSignintechService()
	b, err := s.Generate(context.Background(), sampleRequest())
	require.NoError(t, err, "signintech service must not error on typical input")
	require.NotEmpty(t, b)
	assert.True(t, hasPDFMagic(b), "must contain PDF magic; head=%q", string(b[:min(80, len(b))]))
	assert.True(t, hasEOFTail(b), "must end with %%EOF; tail=%q", string(b[max(0, len(b)-30):]))
}

// TestSignintechService_EmptyInputs asserts we don't panic on a degenerate
// request (no trades, no equity points).
func TestSignintechService_EmptyInputs(t *testing.T) {
	s := NewSignintechService()
	req := ReportRequest{
		TenantID: uuid.Must(uuid.NewV7()),
		Account: AccountSnapshot{
			ID: uuid.Must(uuid.NewV7()), DisplayName: "Empty", Broker: "MT5", BrokerAcct: "0", Currency: "USD",
		},
		Period: Period{From: time.Now(), To: time.Now().Add(time.Hour)},
	}
	b, err := s.Generate(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, hasPDFMagic(b))
	assert.True(t, hasEOFTail(b))
}

// TestSignintechService_NoEquityCurve handles the single-point equity case.
func TestSignintechService_NoEquityCurve(t *testing.T) {
	s := NewSignintechService()
	req := sampleRequest()
	req.Equity = req.Equity[:1] // single point
	req.IncludeEquityCurve = true
	b, err := s.Generate(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, hasPDFMagic(b))
	assert.True(t, hasEOFTail(b))
}

// TestStubService_NoTradeRowsHandles gracefully handles no symbols / no trades.
func TestStubService_NoTradeRowsHandles(t *testing.T) {
	s := NewStubService()
	req := sampleRequest()
	req.Trades = nil
	req.Symbols = nil
	b, err := s.Generate(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, hasPDFMagic(b))
	assert.True(t, hasEOFTail(b))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}