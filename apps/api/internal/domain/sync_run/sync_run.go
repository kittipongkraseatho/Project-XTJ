package sync_run

import (
	"time"

	"github.com/gofrs/uuid"
)

type Status string
type Trigger string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"

	TriggerManual     Trigger = "manual"
	TriggerScheduled  Trigger = "scheduled"
	TriggerReconnect  Trigger = "reconnect"
)

var AllStatuses = []Status{
	StatusQueued,
	StatusRunning,
	StatusSucceeded,
	StatusFailed,
}

var AllTriggers = []Trigger{
	TriggerManual,
	TriggerScheduled,
	TriggerReconnect,
}

func (s Status) IsValid() bool {
	for _, v := range AllStatuses {
		if v == s {
			return true
		}
	}
	return false
}

func (t Trigger) IsValid() bool {
	for _, v := range AllTriggers {
		if v == t {
			return true
		}
	}
	return false
}

type SyncRun struct {
	ID                 uuid.UUID
	TenantID           uuid.UUID
	BrokerConnectionID uuid.UUID
	Status             Status
	Trigger            Trigger
	StartedAt          *time.Time
	FinishedAt         *time.Time
	DealsFetched       int
	DealsImported      int
	DealsSkipped       int
	Error              *string
	Metadata           map[string]any
	CreatedAt          time.Time
}