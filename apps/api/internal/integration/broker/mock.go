package broker

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	mrand "math/rand"
	"sort"
	"time"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/broker_connection"
)

// MockProvider is a deterministic broker stub used in Phase 2 (and demos) to
// exercise the full connect→sync→dedup pipeline without external credentials.
//
// Determinism is the headline property:
//
//   - Same (externalAccountID, from, to) → same Set of deals.
//   - Same provider's deals on second sync → all deduped out (deals_skipped = N).
//
// Implementation:
//   - 0–3 deals per UTC day in [from, to).
//   - Symbol picked from a fixed pool, deterministic per (externalAccountID, day).
//   - Direction alternates per deal.
//   - Open/close times fall in 08:00–20:00 UTC (London+NY overlap).
//   - Open/close prices walk ±50 pips from a per-symbol reference.
//   - Volume 0.1–1.0 lot (10_000–100_000 micro lots).
//   - Profit = (close-open)*volume/100_000 (for 5-decimal pairs).
type MockProvider struct{}

// NewMockProvider returns a fresh MockProvider. Stateless, so callers may share
// one instance across goroutines.
func NewMockProvider() *MockProvider { return &MockProvider{} }

func (MockProvider) Name() broker_connection.Provider { return broker_connection.ProviderMock }

func (MockProvider) Connect(_ context.Context, req ConnectRequest) (ConnectResult, error) {
	ext, err := uuid.NewV7()
	if err != nil {
		return ConnectResult{}, fmt.Errorf("mock: mint external id: %w", err)
	}
	return ConnectResult{
		ExternalAccountID: ext.String(),
		Metadata: map[string]any{
			"server":  req.Server,
			"login":   req.Login,
			"kind":    "mock",
			"created": time.Now().UTC().Format(time.RFC3339),
		},
	}, nil
}

func (MockProvider) Disconnect(_ context.Context, _ string) error { return nil }

func (MockProvider) Ping(_ context.Context, _ string) error { return nil }

// FetchDeals generates a deterministic Set of deals in [from, to).
//
// We iterate day-by-day (UTC midnight → midnight) so the deal distribution
// is identical even when from/to span partial days. The PRNG seed is
// sha256(externalAccountID || dayBucket) so reconnecting the same account
// (different ExternalAccountID) will produce a different deal stream —
// matching reality where each broker account has its own history.
func (MockProvider) FetchDeals(_ context.Context, req FetchDealsRequest) ([]Deal, error) {
	if req.From.After(req.To) || req.From.Equal(req.To) {
		return nil, nil
	}
	if req.ExternalAccountID == "" {
		return nil, fmt.Errorf("mock: empty external_account_id")
	}

	// Snap from to UTC midnight, to to next UTC midnight.
	from := time.Date(req.From.Year(), req.From.Month(), req.From.Day(), 0, 0, 0, 0, time.UTC)
	to := time.Date(req.To.Year(), req.To.Month(), req.To.Day(), 0, 0, 0, 0, time.UTC)
	if !req.To.Equal(to) {
		// include the partial day `req.To` falls on
		to = to.AddDate(0, 0, 1)
	}

	symbols := []string{"EURUSD", "GBPUSD", "USDJPY", "XAUUSD", "US30"}
	// Per-symbol reference price (5-decimal minor units, i.e. ×100_000).
	refs := map[string]int64{
		"EURUSD": 1_080_000,
		"GBPUSD": 1_270_000,
		"USDJPY": 152_000_000,   // JPY pairs quoted to 3 decimals
		"XAUUSD": 2_350_000_000, // gold to 2 decimals × 100_000
		"US30":   39_000_000_000,
	}

	var out []Deal
	for day := from; day.Before(to); day = day.AddDate(0, 0, 1) {
		r := seededRNG(req.ExternalAccountID, day)
		n := r.Intn(4) // 0–3 deals per day
		for i := 0; i < n; i++ {
			symbol := symbols[r.Intn(len(symbols))]
			ref := refs[symbol]
			// 1–2 hours into London+NY window
			hour := 8 + r.Intn(12)
			minute := r.Intn(60)
			openAt := time.Date(day.Year(), day.Month(), day.Day(), hour, minute, 0, 0, time.UTC)
			// 30min – 4h hold
			holdMinutes := 30 + r.Intn(4*60)
			closeAt := openAt.Add(time.Duration(holdMinutes) * time.Minute)
			if closeAt.After(req.To) {
				continue
			}

			direction := "buy"
			if i%2 == 0 {
				direction = "sell"
			}

			// Walk ±50 pips from ref
			pips := int64(r.Intn(101) - 50) // -50..+50
			openPx := ref + pips*1_000      // 1 pip = 10 minor units (5-dec pair)
			// Win-branch only: 50% of deals on a given day are wins.
			winBranch := (i+r.Intn(2))%2 == 0
			deltaPips := int64(5 + r.Intn(36))
			closePx := openPx + deltaPips*1_000
			if direction == "sell" {
				closePx = openPx - deltaPips*1_000
			}
			if !winBranch {
				closePx = openPx - deltaPips*1_000
				if direction == "sell" {
					closePx = openPx + deltaPips*1_000
				}
			}
			closePx = ensurePositive(closePx)
			volume := int64(10_000 + r.Intn(91_000)) // 0.1..1.0 lot
			profit := (closePx - openPx) * volume / 100_000
			if direction == "sell" {
				profit = (openPx - closePx) * volume / 100_000
			}

			out = append(out, Deal{
				ProviderDealID: deriveDealID(req.ExternalAccountID, openAt, symbol, openPx, i),
				Symbol:         symbol,
				Direction:      direction,
				Volume:         volume,
				OpenPrice:      openPx,
				ClosePrice:     closePx,
				OpenTime:       openAt,
				CloseTime:      closeAt,
				Commission:     -int64(50 + r.Intn(50)), // -0.50 .. -1.00 USD
				Swap:           0,
				Profit:         profit,
			})
		}
	}

	// Sort by open time so the BE sees the same order on every call.
	sort.Slice(out, func(i, j int) bool {
		return out[i].OpenTime.Before(out[j].OpenTime)
	})

	if req.Limit > 0 && len(out) > req.Limit {
		out = out[:req.Limit]
	}
	return out, nil
}

// seededRNG returns a deterministic *mrand.Rand for the (externalAccountID, day) pair.
func seededRNG(externalAccountID string, day time.Time) *mrand.Rand {
	h := sha256.New()
	h.Write([]byte(externalAccountID))
	if err := binary.Write(h, binary.BigEndian, day.Unix()); err != nil {
		return nil
	}
	seed := int64(binary.BigEndian.Uint64(h.Sum(nil)[:8]))
	return mrand.New(mrand.NewSource(seed))
}

// deriveDealID builds a stable provider_deal_id from the natural keys. We
// use UUIDv5 over a domain-specific namespace so the same (account, time,
// symbol, price, slot) always maps to the same id — letting the unique
// constraint `UNIQUE(tenant_id, account_id, provider_deal_id)` dedupe
// across syncs without coordination.
func deriveDealID(externalAccountID string, openAt time.Time, symbol string, openPx int64, slot int) string {
	payload := fmt.Sprintf("%s|%d|%s|%d|%d", externalAccountID, openAt.UnixNano(), symbol, openPx, slot)
	return uuid.NewV5(DedupNamespace, payload).String()
}

func ensurePositive(v int64) int64 {
	if v < 1_000 {
		return 1_000
	}
	return v
}
