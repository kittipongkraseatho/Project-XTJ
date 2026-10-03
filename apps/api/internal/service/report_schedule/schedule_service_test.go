package report_schedule

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/trading-journal/api/internal/domain/report_schedule"
)

func TestComputeNextSend_Daily_NowBeforeHour(t *testing.T) {
	// Now is 09:00 UTC, schedule at 14:00 → next is today 14:00.
	now := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	got := ComputeNextSend(report_schedule.FrequencyDaily, 14, nil, nil, now)
	assert.Equal(t, time.Date(2026, 6, 1, 14, 0, 0, 0, time.UTC), got)
}

func TestComputeNextSend_Daily_NowAfterHour(t *testing.T) {
	// Now is 16:00 UTC, schedule at 14:00 → next is tomorrow 14:00.
	now := time.Date(2026, 6, 1, 16, 0, 0, 0, time.UTC)
	got := ComputeNextSend(report_schedule.FrequencyDaily, 14, nil, nil, now)
	assert.Equal(t, time.Date(2026, 6, 2, 14, 0, 0, 0, time.UTC), got)
}

func TestComputeNextSend_Daily_NowExactlyHour(t *testing.T) {
	// At exactly the scheduled hour, "after now" is false, so tomorrow.
	now := time.Date(2026, 6, 1, 14, 0, 0, 0, time.UTC)
	got := ComputeNextSend(report_schedule.FrequencyDaily, 14, nil, nil, now)
	assert.Equal(t, time.Date(2026, 6, 2, 14, 0, 0, 0, time.UTC), got)
}

func TestComputeNextSend_Weekly(t *testing.T) {
	// Now is Sunday (Weekday=0). Want Wednesday (Weekday=3) at 09:00 UTC.
	now := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC) // Sunday
	w := 3
	got := ComputeNextSend(report_schedule.FrequencyWeekly, 9, &w, nil, now)
	assert.Equal(t, time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC), got) // Wed
}

func TestComputeNextSend_Weekly_NextWeek(t *testing.T) {
	// Now is Thursday 12:00 UTC (Weekday=4). Want Wednesday (Weekday=3) at 09:00 UTC.
	// Wed was 09:00 — already past. So next Wed is +7 days.
	now := time.Date(2026, 6, 4, 12, 0, 0, 0, time.UTC) // Thursday
	w := 3
	got := ComputeNextSend(report_schedule.FrequencyWeekly, 9, &w, nil, now)
	assert.Equal(t, time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC), got)
}

func TestComputeNextSend_Monthly_DayInFuture(t *testing.T) {
	// Now is June 10, want day 25. Next: June 25.
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)
	d := 25
	got := ComputeNextSend(report_schedule.FrequencyMonthly, 9, nil, &d, now)
	assert.Equal(t, time.Date(2026, 6, 25, 9, 0, 0, 0, time.UTC), got)
}

func TestComputeNextSend_Monthly_DayInPast(t *testing.T) {
	// Now is June 26, want day 10. Next: July 10.
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	d := 10
	got := ComputeNextSend(report_schedule.FrequencyMonthly, 9, nil, &d, now)
	assert.Equal(t, time.Date(2026, 7, 10, 9, 0, 0, 0, time.UTC), got)
}

func TestFrequency_IsValid(t *testing.T) {
	for _, f := range []report_schedule.Frequency{
		report_schedule.FrequencyDaily,
		report_schedule.FrequencyWeekly,
		report_schedule.FrequencyMonthly,
	} {
		assert.True(t, f.IsValid())
	}
	assert.False(t, report_schedule.Frequency("hourly").IsValid())
}