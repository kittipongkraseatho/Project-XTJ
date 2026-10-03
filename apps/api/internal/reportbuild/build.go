// Package reportbuild is the shared helper that turns trade rows + an
// account snapshot into a pdfreport.ReportRequest. Lives in its own package
// to avoid the import cycle between phase3http (handler) and
// report_schedule (service) — both call BuildReportRequest.
package reportbuild

import (
	"context"
	"time"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/trade"
	"github.com/trading-journal/api/internal/integration/pdfreport"
	"github.com/trading-journal/api/internal/store"
)

// BuildReportRequest populates a pdfreport.ReportRequest from trade rows.
// Pulls trades in [from, to) for the account, computes overview + symbol
// breakdown + equity curve, and returns the embed.
//
// Exposed at package level so phase3http and report_schedule can share the
// single source of truth for "what goes in the PDF".
func BuildReportRequest(
	ctx context.Context,
	st *store.Store,
	tenantID uuid.UUID,
	accountID uuid.UUID,
	from, to time.Time,
	account pdfreport.AccountSnapshot,
	includeEquity bool,
) (pdfreport.ReportRequest, error) {
	accID := accountID
	trades, err := st.Trades.ListByTenant(ctx, tenantID, &accID, 5000, 0)
	if err != nil {
		return pdfreport.ReportRequest{}, err
	}

	req := pdfreport.ReportRequest{
		TenantID:           tenantID,
		IncludeEquityCurve: includeEquity,
		Account:            account,
		Period: pdfreport.Period{
			From: from,
			To:   to.Add(24 * time.Hour),
		},
	}

	var wins, losses int64
	var netPL, grossProfit, grossLoss float64
	for _, t := range trades {
		if t.CloseTime == nil {
			continue
		}
		if t.CloseTime.Before(from) || !t.CloseTime.Before(to) {
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

	symAgg := map[string]*pdfreport.SymbolRow{}
	for _, t := range trades {
		if t.CloseTime == nil {
			continue
		}
		if t.CloseTime.Before(from) || !t.CloseTime.Before(to) {
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

	_ = (*trade.Trade)(nil) // link guard; reportbuild touches trade.Stock through but no methods
	return req, nil
}