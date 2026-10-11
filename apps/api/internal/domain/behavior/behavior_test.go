package behavior

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProfileStatusAndFindingCodesAreStable(t *testing.T) {
	require.Equal(t, ProfileStatus("insufficient_data"), ProfileStatusInsufficientData)
	require.Equal(t, ProfileStatus("ready"), ProfileStatusReady)
	require.Equal(t, FindingCode("revenge_after_loss"), FindingRevengeAfterLoss)
	require.Equal(t, FindingCode("risk_escalation"), FindingRiskEscalation)
}

func TestEmptyProfileHasNoFindings(t *testing.T) {
	p := Profile{Findings: []Finding{}}
	require.Equal(t, 0, p.SampleSize)
	require.Empty(t, p.Findings)
}
