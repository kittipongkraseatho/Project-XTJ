package subscription

import (
	"time"

	"github.com/gofrs/uuid"
)

type Status string

const (
	StatusTrialing Status = "trialing"
	StatusActive   Status = "active"
	StatusPastDue  Status = "past_due"
	StatusCanceled Status = "canceled"
	StatusExpired  Status = "expired"
)

type Subscription struct {
	ID                    uuid.UUID
	TenantID              uuid.UUID
	PlanID                uuid.UUID
	Status                Status
	StartedAt             time.Time
	CurrentPeriodStart    time.Time
	CurrentPeriodEnd      time.Time
	CancelAt              *time.Time
	CanceledAt            *time.Time
	ExternalProvider      *string
	ExternalSubscriptionID *string
	Metadata              map[string]any
	CreatedAt             time.Time
	UpdatedAt             time.Time
}