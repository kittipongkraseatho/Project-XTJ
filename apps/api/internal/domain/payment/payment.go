package payment

import (
	"time"

	"github.com/gofrs/uuid"
)

type Status string

const (
	StatusOpen     Status = "open"
	StatusPaid     Status = "paid"
	StatusFailed   Status = "failed"
	StatusRefunded Status = "refunded"
)

var AllStatuses = []Status{StatusOpen, StatusPaid, StatusFailed, StatusRefunded}

func (s Status) IsValid() bool {
	for _, v := range AllStatuses {
		if v == s {
			return true
		}
	}
	return false
}

type Payment struct {
	ID                 uuid.UUID
	TenantID           uuid.UUID
	SubscriptionID     uuid.UUID
	Provider           string
	ExternalInvoiceID  string
	AmountCents        int64
	Currency           string
	Status             Status
	PaidAt             *time.Time
	Metadata           map[string]any
	CreatedAt          time.Time
}