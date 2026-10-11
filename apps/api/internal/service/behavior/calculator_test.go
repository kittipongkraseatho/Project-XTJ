package behavior

import (
	"fmt"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"

	domainbehavior "github.com/trading-journal/api/internal/domain/behavior"
)

func inputTrade(n int, opened time.Time, symbol, outcome string, volume, pnl float64) domainbehavior.InputTrade {
	closed := opened.Add(10 * time.Minute)
	return domainbehavior.InputTrade{
		ID:        uuid.Must(uuid.FromString("00000000-0000-7000-8000-" + formatID(n))),
		Symbol:    symbol,
		Direction: "long",
		Volume:    volume,
		NetPL:     pnl,
		Outcome:   outcome,
		OpenTime:  opened,
		CloseTime: &closed,
	}
}

func formatID(n int) string {
	return fmt.Sprintf("%012d", n)
}

func TestCalculatorReturnsInsufficientDataWithoutFabricatedMetrics(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	trades := []domainbehavior.InputTrade{inputTrade(1, start, "EURUSD", "win", 1, 10)}

	profile := NewCalculator().Calculate(uuid.Nil, start, start.Add(24*time.Hour), trades)

	require.Equal(t, domainbehavior.ProfileStatusInsufficientData, profile.Status)
	require.Equal(t, 1, profile.SampleSize)
	require.Zero(t, profile.Metrics.WinRate)
	require.Empty(t, profile.Findings)
}

func TestCalculatorComputesMetricsAtMinimumSample(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	trades := make([]domainbehavior.InputTrade, 0, 10)
	for i := 0; i < 8; i++ {
		trades = append(trades, inputTrade(i+1, start.Add(time.Duration(i)*time.Hour), "EURUSD", "win", 1, 10))
	}
	trades = append(trades, inputTrade(9, start.Add(8*time.Hour), "EURUSD", "loss", 1, -5))
	trades = append(trades, inputTrade(10, start.Add(9*time.Hour), "GBPUSD", "breakeven", 1, 0))

	profile := NewCalculator().Calculate(uuid.Nil, start, start.Add(24*time.Hour), trades)

	require.Equal(t, domainbehavior.ProfileStatusReady, profile.Status)
	require.Equal(t, 10, profile.SampleSize)
	require.InDelta(t, 0.8, profile.Metrics.WinRate, 0.0001)
	require.InDelta(t, 7.5, profile.Metrics.Expectancy, 0.0001)
	require.InDelta(t, 0.9, profile.Metrics.SymbolShare["EURUSD"], 0.0001)
}

func TestCalculatorUsesHalfOpenUTCWindow(t *testing.T) {
	from := time.Date(2026, 1, 1, 23, 0, 0, 0, time.UTC)
	trades := []domainbehavior.InputTrade{
		inputTrade(1, from, "EURUSD", "win", 1, 1),
		inputTrade(2, from.Add(time.Hour), "EURUSD", "win", 1, 1),
	}

	profile := NewCalculator().Calculate(uuid.Nil, from, from.Add(time.Hour), trades)

	require.Equal(t, 1, profile.SampleSize)
}
