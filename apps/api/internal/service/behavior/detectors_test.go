package behavior

import (
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"

	domainbehavior "github.com/trading-journal/api/internal/domain/behavior"
)

func TestDetectorsUseExactBoundaries(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	trades := make([]domainbehavior.InputTrade, 0, 21)
	for i := 0; i < 21; i++ {
		trades = append(trades, inputTrade(i+1, start.Add(time.Duration(i)*time.Minute), "EURUSD", "win", 1, 1))
	}
	trades[0] = inputTrade(1, start, "EURUSD", "loss", 1, -1)
	trades[1] = inputTrade(2, start.Add(10*time.Minute), "EURUSD", "win", 1.5, 1)
	trades[5] = inputTrade(6, start.Add(23*time.Minute), "GBPUSD", "win", 1.5, 1)
	trades[18] = inputTrade(19, start.Add(20*time.Minute), "GBPUSD", "loss", 1, -1)
	trades[19] = inputTrade(20, start.Add(21*time.Minute), "GBPUSD", "loss", 1, -1)
	trades[20] = inputTrade(21, start.Add(22*time.Minute), "GBPUSD", "loss", 1, -1)

	profile := NewCalculator().Calculate(uuid.Nil, start, start.Add(24*time.Hour), trades)

	require.Equal(t, domainbehavior.ProfileStatusReady, profile.Status)
	findingCodes := make(map[domainbehavior.FindingCode]bool)
	for _, finding := range profile.Findings {
		findingCodes[finding.Code] = true
	}
	require.True(t, findingCodes[domainbehavior.FindingOvertradingFrequency])
	require.True(t, findingCodes[domainbehavior.FindingRevengeAfterLoss])
	require.True(t, findingCodes[domainbehavior.FindingLossStreak])
	require.True(t, findingCodes[domainbehavior.FindingRiskEscalation])
	require.True(t, findingCodes[domainbehavior.FindingSymbolConcentration])
}

func TestDetectorsDoNotFlagTwentyTradesOrThirtyOneMinuteRevenge(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	trades := make([]domainbehavior.InputTrade, 0, 20)
	for i := 0; i < 20; i++ {
		trades = append(trades, inputTrade(i+1, start.Add(time.Duration(i)*time.Hour), "EURUSD", "win", 1, 1))
	}
	trades[0] = inputTrade(1, start, "EURUSD", "loss", 1, -1)
	trades[1] = inputTrade(2, start.Add(31*time.Minute), "EURUSD", "win", 1.49, 1)

	profile := NewCalculator().Calculate(uuid.Nil, start, start.Add(24*time.Hour), trades)

	for _, finding := range profile.Findings {
		require.NotEqual(t, domainbehavior.FindingOvertradingFrequency, finding.Code)
		require.NotEqual(t, domainbehavior.FindingRevengeAfterLoss, finding.Code)
	}
}
