package report_schedule

import (
	"time"

	"github.com/gofrs/uuid"
)

type Frequency string

const (
	FrequencyDaily   Frequency = "daily"
	FrequencyWeekly  Frequency = "weekly"
	FrequencyMonthly Frequency = "monthly"
)

var AllFrequencies = []Frequency{FrequencyDaily, FrequencyWeekly, FrequencyMonthly}

func (f Frequency) IsValid() bool {
	for _, v := range AllFrequencies {
		if v == f {
			return true
		}
	}
	return false
}

type ReportSchedule struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	TradingAccountID  uuid.UUID
	Frequency         Frequency
	HourUTC           int
	Weekday           *int // 0..6, weekly only
	DayOfMonth        *int // 1..28, monthly only
	LastSentAt        *time.Time
	NextSendAt        time.Time
	Recipients        []string
	IsActive          bool
	Metadata          map[string]any
	CreatedAt         time.Time
	UpdatedAt         time.Time
}