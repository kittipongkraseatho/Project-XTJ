package r2http

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseWindowNormalizesUTCAndUsesHalfOpenContract(t *testing.T) {
	from, to, err := parseWindow("2026-01-01T07:00:00+07:00", "2026-01-08T07:00:00+07:00")
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), from)
	require.Equal(t, time.Date(2026, 1, 8, 0, 0, 0, 0, time.UTC), to)
}

func TestParseWindowRejectsMissingAndReversedDates(t *testing.T) {
	_, _, err := parseWindow("", "2026-01-08T00:00:00Z")
	require.Error(t, err)
	_, _, err = parseWindow("2026-01-08T00:00:00Z", "2026-01-01T00:00:00Z")
	require.Error(t, err)
}
