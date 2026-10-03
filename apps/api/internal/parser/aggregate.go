package parser

import (
	"sort"
	"time"

	"github.com/gofrs/uuid"
)

// Aggregate groups deals by `provider_deal_id` + `comment` heuristics into
// derived trades for analytics.
//
// MetaQuotes MT5 model:
//   - "entry" deal opens a position
//   - "exit" deal closes it (or partial closes it)
//   - For the same line, an `entry` may pair with multiple `exit` deals (partial close)
//   - A position has an `open_time` and a `close_time` (latest exit)
//
// Phase 1 simplification: group by Magic + Symbol, ordered by ExecutedAt,
// then walk through: open position on first 'entry', close on first 'exit'.
// Partial closes create multiple trades per symbol+magic group.
//
// For hedge accounts, this is a known limitation — Phase 2 adds MetaApi
// for proper position tracking (where each deal carries position_id).
type Trade struct {
	ID            uuid.UUID
	Symbol         string
	Direction     string // "long" | "short"
	Volume        float64
	OpenPrice     float64
	ClosePrice    float64
	OpenTime      time.Time
	CloseTime     time.Time
	HoldingSec    int64
	GrossProfit   float64
	GrossLoss     float64
	NetPL         float64
	Commission    float64
	Swap          float64
	Magic         *int64
	Outcome       string // "win" | "loss" | "breakeven"
	IsPartial     bool
	EntryDealID   string
	ExitDealIDs  []string
}

func Aggregate(deals []Deal) []Trade {
	// Sort by ExecutedAt
	sorted := make([]Deal, len(deals))
	copy(sorted, deals)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ExecutedAt.Before(sorted[j].ExecutedAt)
	})

	// Group by symbol + magic. Magic default — to "1" so models without magic
	// don't get cross-contaminated across symbols.
	groups := map[string][]Deal{}
	for _, d := range sorted {
		magicKey := "0"
		if d.Magic != nil {
			magicKey = formatInt(*d.Magic)
		}
		key := d.Symbol + "|" + magicKey
		groups[key] = append(groups[key], d)
	}

	var trades []Trade
	for _, group := range groups {
		trades = append(trades, aggregateGroup(group)...)
	}

	// Sort final trades by OpenTime
	sort.Slice(trades, func(i, j int) bool {
		return trades[i].OpenTime.Before(trades[j].OpenTime)
	})
	return trades
}

func aggregateGroup(group []Deal) []Trade {
	var out []Trade
	var openPos *openPosition

	for _, d := range group {
		switch d.EntryType {
		case "entry", "entry_inout":
			// Close previous if still open (defensive)
			if openPos != nil {
				out = append(out, closePosition(openPos, d))
				openPos = nil
			}
			dir := "long"
			if d.Type == "sell" {
				dir = "short"
			}
			openPos = &openPosition{
				Symbol:     d.Symbol,
				Direction:  dir,
				Volume:     d.Volume,
				OpenPrice:  d.Price,
				OpenTime:   d.ExecutedAt,
				Magic:      d.Magic,
				EntryDealID: d.ProviderDealID,
				Commission: d.Commission,
				Swap:       d.Swap,
			}
		case "exit", "exit_inout":
			if openPos == nil {
				// Orphan exit — skip with no associated open.
				continue
			}
			// Close current position; if same volume+profit, single close;
			// otherwise treat as partial close (open a new trade with the remainder).
			out = append(out, closePosition(openPos, d))
			openPos = nil
		case "inout":
			// inout reverses position — for Phase 1 simplification, treat as close+open
			if openPos != nil {
				out = append(out, closePosition(openPos, d))
				openPos = nil
			}
			dir := "long"
			if d.Type == "sell" {
				dir = "short"
			}
			openPos = &openPosition{
				Symbol: d.Symbol, Direction: dir,
				Volume: d.Volume, OpenPrice: d.Price, OpenTime: d.ExecutedAt,
				Magic: d.Magic, EntryDealID: d.ProviderDealID,
				Commission: d.Commission, Swap: d.Swap,
			}
		default:
			// balance / credit / commission — skip from trade aggregation
		}
	}
	// Position never closed — leave out (Phase 2 will surface as open positions).
	return out
}

type openPosition struct {
	Symbol      string
	Direction   string
	Volume      float64
	OpenPrice   float64
	OpenTime    time.Time
	Magic       *int64
	EntryDealID string
	Commission  float64
	Swap        float64
}

func closePosition(op *openPosition, exit Deal) Trade {
	net := exit.Profit + exit.Commission + exit.Swap + op.Commission + op.Swap
	grossProfit, grossLoss := 0.0, 0.0
	if net > 0 {
		grossProfit = net
	} else if net < 0 {
		grossLoss = net
	}
	outcome := "breakeven"
	if net > 0 {
		outcome = "win"
	} else if net < 0 {
		outcome = "loss"
	}
	return Trade{
		ID:          uuid.Must(uuid.NewV7()),
		Symbol:      op.Symbol,
		Direction:   op.Direction,
		Volume:      op.Volume,
		OpenPrice:   op.OpenPrice,
		ClosePrice:  exit.Price,
		OpenTime:    op.OpenTime,
		CloseTime:   exit.ExecutedAt,
		HoldingSec:  int64(exit.ExecutedAt.Sub(op.OpenTime).Seconds()),
		GrossProfit: grossProfit,
		GrossLoss:   grossLoss,
		NetPL:       net,
		Commission:  op.Commission + exit.Commission,
		Swap:        op.Swap + exit.Swap,
		Magic:       op.Magic,
		Outcome:     outcome,
		EntryDealID: op.EntryDealID,
		ExitDealIDs: []string{exit.ProviderDealID},
	}
}

func formatInt(n int64) string {
	const digits = "0123456789"
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{digits[n%10]}, b...)
		n /= 10
	}
	return string(b)
}