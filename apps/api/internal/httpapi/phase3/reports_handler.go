// Package phase3http hosts HTTP handlers for Phase 3 endpoints
// (/v1/reports/*).
//
// Phase 3 ships a single Trade Report PDF generator. Future endpoints
// (e.g. /v1/admin/reports, /v1/reports/email-send) belong here too.
package phase3http

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gofrs/uuid"
	"github.com/labstack/echo/v4"

	"github.com/trading-journal/api/internal/auth"
	"github.com/trading-journal/api/internal/domain/trade"
	"github.com/trading-journal/api/internal/domain/trading_account"
	"github.com/trading-journal/api/internal/integration/pdfreport"
	"github.com/trading-journal/api/internal/observability"
	"github.com/trading-journal/api/internal/store"
)

// Handler is the Phase 3 HTTP handler container.
type Handler struct {
	store *store.Store
	pdf   pdfreport.Service
	log   *slog.Logger
}

// New wires the dependencies.
func New(st *store.Store, pdf pdfreport.Service, log *slog.Logger) *Handler {
	return &Handler{store: st, pdf: pdf, log: log}
}

// ── helpers ──────────────────────────────────────────────────────────

func idsFromCtx(c echo.Context) (uuid.UUID, uuid.UUID, bool) {
	uid, ok := auth.UserIDFromContext(c.Request().Context())
	if !ok {
		return uuid.Nil, uuid.Nil, false
	}
	v, has := c.Get("tenant_id").(string)
	if !has || v == "" {
		return uuid.Nil, uuid.Nil, false
	}
	tid, err := uuid.FromString(v)
	if err != nil {
		return uuid.Nil, uuid.Nil, false
	}
	return uid, tid, true
}

// ── PDF report ────────────────────────────────────────────────────────

// GeneratePDF — POST /v1/reports/pdf
//
// Body:
//
//	{
//	  "trading_account_id": "<uuid>",
//	  "from": "2025-01-01",
//	  "to":   "2025-01-31",
//	  "include_equity_curve": true
//	}
//
// Returns application/pdf binary. Synchronous: typical <1s for ≤1000 trades.
func (h *Handler) GeneratePDF(c echo.Context) error {
	_, tid, ok := idsFromCtx(c)
	if !ok {
		return c.JSON(http.StatusUnauthorized, observability.FromError(observability.ErrUnauthorized))
	}
	var body struct {
		TradingAccountID   string `json:"trading_account_id"`
		From               string `json:"from"`
		To                 string `json:"to"`
		IncludeEquityCurve *bool  `json:"include_equity_curve"`
	}
	if err := c.Bind(&body); err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithCause(err)))
	}
	if body.TradingAccountID == "" || body.From == "" || body.To == "" {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "missing required field"})))
	}
	accountID, err := uuid.FromString(body.TradingAccountID)
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid trading_account_id"})))
	}
	from, err := time.Parse("2006-01-02", body.From)
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid from"})))
	}
	to, err := time.Parse("2006-01-02", body.To)
	if err != nil {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "invalid to"})))
	}
	if to.Before(from) {
		return c.JSON(http.StatusUnprocessableEntity, observability.FromError(
			observability.ErrValidation.WithDetails(map[string]any{"reason": "to < from"})))
	}
	includeEquity := true
	if body.IncludeEquityCurve != nil {
		includeEquity = *body.IncludeEquityCurve
	}

	// Tenant-scoped account lookup.
	acc, err := h.store.TradingAccounts.GetByID(c.Request().Context(), accountID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
		}
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	if acc.TenantID != tid {
		return c.JSON(http.StatusNotFound, observability.FromError(observability.ErrUserNotFound))
	}

	// Fetch trades in [from, to) for this account. Use a high limit — typical
	// monthly report is <500 trades. For Phase 3 the cap is 5000.
	trades, err := h.store.Trades.ListByTenant(c.Request().Context(), tid, &accountID, 5000, 0)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}
	trades = filterByRange(trades, from, to)

	// Build the embed.
	req := h.buildRequest(tid, acc, from, to, trades, includeEquity)

	pdfBytes, err := h.pdf.Generate(c.Request().Context(), req)
	if err != nil {
		h.log.Error("reports.pdf.generate_failed", "tenant_id", tid, "account_id", accountID, "err", err)
		return c.JSON(http.StatusInternalServerError, observability.FromError(err))
	}

	// Audit
	_ = h.store.Audit.Insert(c.Request().Context(), store.AuditEntry{
		ActorTenantID: &tid,
		Action:        "report.pdf.generated",
		ResourceType:  strPtr("trading_account"),
		ResourceID:    &accountID,
		Metadata: map[string]any{
			"from":                from.Format("2006-01-02"),
			"to":                  to.Format("2006-01-02"),
			"include_equity_curve": includeEquity,
			"trade_count":          len(trades),
			"pdf_bytes":            len(pdfBytes),
		},
	})

	// Stream as application/pdf with a descriptive filename.
	filename := fmt.Sprintf("trading-journal_%s_%s_to_%s.pdf",
		sanitizeFilename(acc.DisplayName),
		from.Format("2006-01-02"), to.Format("2006-01-02"))
	c.Response().Header().Set("Content-Type", "application/pdf")
	c.Response().Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Response().Header().Set("Content-Length", fmt.Sprintf("%d", len(pdfBytes)))
	return c.Blob(http.StatusOK, "application/pdf", pdfBytes)
}

func (h *Handler) buildRequest(tid uuid.UUID, acc *trading_account.TradingAccount, from, to time.Time, trades []*trade.Trade, includeEquity bool) pdfreport.ReportRequest {
	req := pdfreport.ReportRequest{
		TenantID:           tid,
		IncludeEquityCurve: includeEquity,
		Account: pdfreport.AccountSnapshot{
			ID:          acc.ID,
			DisplayName: acc.DisplayName,
			Broker:      string(acc.Broker),
			BrokerAcct:  acc.BrokerAccountID,
			Currency:    acc.Currency,
		},
		Period: pdfreport.Period{From: from, To: to.Add(24 * time.Hour)},
	}

	// Overview stats
	var wins, losses int64
	var netPL, grossProfit, grossLoss float64
	for _, t := range trades {
		if t.CloseTime == nil {
			continue
		}
		netPL += t.NetPL
		switch string(t.Outcome) {
		case "win":
			wins++
			grossProfit += t.GrossProfit
		case "loss":
			losses++
			grossLoss += t.GrossLoss
		}
	}
	req.Overview.TradeCount = int64(len(trades))
	req.Overview.NetPL = netPL
	if req.Overview.TradeCount > 0 {
		req.Overview.WinRate = float64(wins) / float64(req.Overview.TradeCount)
	}
	if grossLoss < 0 {
		req.Overview.ProfitFactor = grossProfit / (-grossLoss)
	}

	// Symbol breakdown
	symAgg := map[string]*pdfreport.SymbolRow{}
	for _, t := range trades {
		if t.CloseTime == nil {
			continue
		}
		r, ok := symAgg[t.Symbol]
		if !ok {
			r = &pdfreport.SymbolRow{Symbol: t.Symbol}
			symAgg[t.Symbol] = r
		}
		r.TradeCount++
		r.NetPL += t.NetPL
		if string(t.Outcome) == "win" {
			r.WinRate++
		}
	}
	for _, r := range symAgg {
		if r.TradeCount > 0 {
			r.WinRate = r.WinRate / float64(r.TradeCount)
		}
		req.Symbols = append(req.Symbols, *r)
	}

	// Trade list — show most recent 50 of the in-range set.
	end := len(trades)
	for i, j := 0, end-1; i < j; i, j = i+1, j-1 {
		trades[i], trades[j] = trades[j], trades[i]
	}
	max := 50
	if end < max {
		max = end
	}
	for i := 0; i < max; i++ {
		t := trades[i]
		if t.CloseTime == nil {
			continue
		}
		req.Trades = append(req.Trades, pdfreport.TradeRow{
			OpenTime:  t.OpenTime,
			CloseTime: *t.CloseTime,
			Symbol:    t.Symbol,
			Direction: string(t.Direction),
			NetPL:     t.NetPL,
			Outcome:   string(t.Outcome),
		})
	}

	// Equity curve points — running cumulative net P/L in trade order.
	if includeEquity {
		var running float64
		for _, t := range trades {
			if t.CloseTime == nil {
				continue
			}
			running += t.NetPL
			req.Equity = append(req.Equity, pdfreport.EquityPoint{
				Time: *t.CloseTime, Equity: running,
			})
		}
	}

	return req
}

func filterByRange(trades []*trade.Trade, from, to time.Time) []*trade.Trade {
	out := make([]*trade.Trade, 0, len(trades))
	for _, t := range trades {
		if t.CloseTime == nil {
			continue
		}
		if t.CloseTime.Before(from) {
			continue
		}
		if !t.CloseTime.Before(to) {
			continue
		}
		out = append(out, t)
	}
	return out
}

func sanitizeFilename(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '.':
			out = append(out, c)
		case c == ' ':
			out = append(out, '_')
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}

func strPtr(s string) *string { return &s }