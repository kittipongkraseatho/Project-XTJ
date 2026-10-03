package pdfreport

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
)

// signintechService implements Service using go-pdf/fpdf (pure Go, MIT).
//
// We use the built-in PDF "Helvetica" core font — every PDF reader renders
// these without needing a TTF payload embedded in the file. (signintech/gopdf
// forces you to bundle a TTF, which bloats the binary and trips a subset
// bug in v0.38.1. go-pdf/fpdf has no such constraint.)
//
// Pages: A4 portrait, 210×297 mm. Margins: 15mm on every side.
type signintechService struct{}

// NewSignintechService returns a Service backed by go-pdf/fpdf.
func NewSignintechService() Service { return &signintechService{} }

func (s *signintechService) Generate(_ context.Context, req ReportRequest) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(15, 15, 15)
	pdf.SetAutoPageBreak(true, 15)

	pdf.AddPage()
	pdf.SetFont("Helvetica", "", 14)

	// ── Header ───────────────────────────────────────────────────────
	pdf.SetXY(15, 20)
	pdf.Cell(180, 8, "Trading Journal - Trade Report")

	pdf.SetFont("Helvetica", "", 10)
	pdf.SetXY(15, 30)
	pdf.CellFormat(180, 5, fmt.Sprintf("Account: %s  |  Broker: %s (%s)  |  Currency: %s",
		sanitize(req.Account.DisplayName), sanitize(req.Account.Broker), sanitize(req.Account.BrokerAcct), req.Account.Currency),
		"", 0, "", false, 0, "")
	pdf.SetXY(15, 37)
	pdf.CellFormat(180, 5, fmt.Sprintf("Period: %s - %s    Generated: %s",
		req.Period.From.Format("2006-01-02"),
		req.Period.To.Format("2006-01-02"),
		time.Now().UTC().Format("2006-01-02 15:04 UTC")),
		"", 0, "", false, 0, "")

	// Separator
	pdf.SetDrawColor(180, 180, 180)
	pdf.SetLineWidth(0.4)
	pdf.Line(15, 45, 195, 45)

	// ── Summary 4-cell ───────────────────────────────────────────────
	y := 55.0
	pdf.SetFont("Helvetica", "B", 10)
	drawSummaryCell(pdf, 15, y, "Net P/L", fmtMoney(req.Overview.NetPL, req.Account.Currency))
	drawSummaryCell(pdf, 60, y, "Win Rate", fmtPct(req.Overview.WinRate))
	drawSummaryCell(pdf, 105, y, "Profit Factor", fmtFloat(req.Overview.ProfitFactor, 2))
	drawSummaryCell(pdf, 150, y, "Trades", fmtInt(req.Overview.TradeCount))

	// ── Equity curve ────────────────────────────────────────────────
	y = 85.0
	if req.IncludeEquityCurve && len(req.Equity) > 1 {
		y = drawEquityCurve(pdf, req.Equity, y)
	}

	// ── Symbol breakdown ────────────────────────────────────────────
	y += 5
	pdf.SetFont("Helvetica", "B", 11)
	pdf.SetXY(15, y)
	pdf.Cell(180, 6, "Symbol Breakdown (top 10)")
	y += 10
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetXY(15, y)
	pdf.Cell(50, 5, "Symbol")
	pdf.Cell(35, 5, "Trades")
	pdf.Cell(50, 5, "Net P/L")
	pdf.Cell(45, 5, "Win Rate")
	y += 6
	pdf.SetLineWidth(0.2)
	pdf.Line(15, y-1, 195, y-1)

	maxRows := len(req.Symbols)
	if maxRows > 10 {
		maxRows = 10
	}
	for i := 0; i < maxRows; i++ {
		s := req.Symbols[i]
		pdf.SetXY(15, y)
		pdf.Cell(50, 5, s.Symbol)
		pdf.Cell(35, 5, fmtInt(s.TradeCount))
		pdf.Cell(50, 5, fmtMoney(s.NetPL, req.Account.Currency))
		pdf.Cell(45, 5, fmtPct(s.WinRate))
		y += 6
	}

	// ── Trade list (last 50) ─────────────────────────────────────────
	y += 8
	pdf.SetFont("Helvetica", "B", 11)
	pdf.SetXY(15, y)
	pdf.Cell(180, 6, fmt.Sprintf("Trade List (%d most recent)", minInt(len(req.Trades), 50)))
	y += 10
	pdf.SetFont("Helvetica", "", 8)
	pdf.SetXY(15, y)
	pdf.Cell(28, 4, "Open")
	pdf.Cell(30, 4, "Symbol")
	pdf.Cell(25, 4, "Dir")
	pdf.Cell(45, 4, "Net P/L")
	pdf.Cell(25, 4, "Outcome")
	pdf.Cell(27, 4, "Close")
	y += 6
	pdf.Line(15, y-2, 195, y-2)

	maxTrades := len(req.Trades)
	if maxTrades > 50 {
		maxTrades = 50
	}
	// Show latest first.
	for i := 0; i < maxTrades/2; i++ {
		j := maxTrades - 1 - i
		req.Trades[i], req.Trades[j] = req.Trades[j], req.Trades[i]
	}
	for i := 0; i < maxTrades; i++ {
		tr := req.Trades[i]
		pdf.SetXY(15, y)
		pdf.Cell(28, 4, tr.OpenTime.Format("2006-01-02"))
		pdf.Cell(30, 4, tr.Symbol)
		pdf.Cell(25, 4, tr.Direction)
		pdf.Cell(45, 4, fmtMoney(tr.NetPL, req.Account.Currency))
		pdf.Cell(25, 4, tr.Outcome)
		pdf.Cell(27, 4, tr.CloseTime.Format("2006-01-02"))
		y += 5
		if y > 280 {
			break
		}
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("pdf render: %w", err)
	}
	return buf.Bytes(), nil
}

// ── helpers ──────────────────────────────────────────────────────────

func drawSummaryCell(pdf *fpdf.Fpdf, x0, y0 float64, label, value string) {
	pdf.SetFont("Helvetica", "", 8)
	pdf.SetXY(x0, y0)
	pdf.Cell(45, 4, label)
	pdf.SetFont("Helvetica", "B", 12)
	pdf.SetXY(x0, y0+5)
	pdf.Cell(45, 6, value)
}

func drawEquityCurve(pdf *fpdf.Fpdf, points []EquityPoint, yTop float64) float64 {
	const (
		chartX = 15.0
		chartW = 180.0
		chartH = 50.0
	)
	yBase := yTop + chartH + 2

	var minE, maxE float64
	for i, p := range points {
		if i == 0 || p.Equity < minE {
			minE = p.Equity
		}
		if i == 0 || p.Equity > maxE {
			maxE = p.Equity
		}
	}
	if maxE == minE {
		maxE = minE + 1
	}

	// Title
	pdf.SetFont("Helvetica", "B", 11)
	pdf.SetXY(chartX, yTop)
	pdf.Cell(chartW, 5, "Equity Curve")

	// Box
	pdf.SetDrawColor(180, 180, 180)
	pdf.SetLineWidth(0.2)
	pdf.Line(chartX, yBase, chartX+chartW, yBase)
	pdf.Line(chartX, yTop+8, chartX, yBase)

	// Zero line
	if minE < 0 && maxE > 0 {
		zeroY := yBase - (0-minE)/(maxE-minE)*chartH
		pdf.SetDrawColor(200, 200, 200)
		pdf.Line(chartX, zeroY, chartX+chartW, zeroY)
	}

	// Data
	pdf.SetDrawColor(59, 130, 246)
	pdf.SetLineWidth(0.6)
	for i := 1; i < len(points); i++ {
		x1 := chartX + float64(i-1)/float64(len(points)-1)*chartW
		x2 := chartX + float64(i)/float64(len(points)-1)*chartW
		y1 := yBase - (points[i-1].Equity-minE)/(maxE-minE)*chartH
		y2 := yBase - (points[i].Equity-minE)/(maxE-minE)*chartH
		pdf.Line(x1, y1, x2, y2)
	}

	pdf.SetDrawColor(0, 0, 0)
	return yBase + 4
}

// ── formatting ─────────────────────────────────────────────────────

func fmtMoney(v float64, currency string) string {
	if currency == "" {
		currency = "USD"
	}
	return fmt.Sprintf("%s %.2f", currency, v)
}

func fmtPct(v float64) string {
	return fmt.Sprintf("%.1f%%", v*100)
}

func fmtFloat(v float64, prec int) string {
	return fmt.Sprintf("%.*f", prec, v)
}

func fmtInt(v int64) string {
	return fmt.Sprintf("%d", v)
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// sanitize replaces Unicode punctuation that fpdf's built-in Helvetica doesn't
// have (en-dash, em-dash) with ASCII so the text renders without missing glyphs.
func sanitize(s string) string {
	r := strings.NewReplacer("—", "-", "–", "-", "·", "|")
	return r.Replace(s)
}