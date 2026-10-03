// Package webhook — domain types for the Phase 5.2 outbound webhook system.
package webhook

import (
	"net/http"
	"time"

	"github.com/gofrs/uuid"
)

// EventType is the canonical identifier of a webhook event.
//
// Phase 5.2 ships 2 events; new ones are added to AllEventTypes as we
// add emitters. Subscription.EventTypes is checked against AllEventTypes
// before persisting.
type EventType string

const (
	EventImportCompleted EventType = "import.completed"
	EventTradeCreated    EventType = "trade.created"
)

// AllEventTypes lists every event the platform currently emits. Used by
// the FE selector and for validation on the server side.
var AllEventTypes = []EventType{
	EventImportCompleted,
	EventTradeCreated,
}

// IsValid reports whether e is in AllEventTypes.
func (e EventType) IsValid() bool {
	for _, v := range AllEventTypes {
		if v == e {
			return true
		}
	}
	return false
}

// Subscription is one row of webhook_subscriptions.
//
// Secret is plaintext (DB at-rest encryption). The HMAC signing code is
// the only consumer; never log it.
type Subscription struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	URL             string
	EventTypes      []EventType
	Secret          string
	Description     string
	IsActive        bool
	CreatedByUserID uuid.UUID
	CreatedAt       time.Time
	UpdatedAt       time.Time
	RevokedAt       *time.Time
}

// Delivery status.
const (
	DeliveryStatusPending   = "pending"
	DeliveryStatusSuccess   = "success"
	DeliveryStatusFailed    = "failed"    // non-retryable client error
	DeliveryStatusExhausted = "exhausted" // retryable but out of attempts
)

// Delivery is one row of webhook_deliveries.
type Delivery struct {
	ID             uuid.UUID
	TenantID       uuid.UUID
	SubscriptionID uuid.UUID
	EventType      EventType
	EventID        string // unique idempotency token
	Payload        []byte // raw JSON body
	Attempt        int
	MaxAttempts    int
	Status         string
	ResponseCode   *int
	ResponseBody   *string
	NextAttemptAt  time.Time
	LastAttemptedAt *time.Time
	CompletedAt    *time.Time
	CreatedAt      time.Time
}

// BackoffSchedule is the exponential-backoff schedule for retries.
// Index = attempt number (0-based). After MaxAttempts attempts the row
// goes to "exhausted".
//
//	attempt 1 → 1 minute
//	attempt 2 → 5 minutes
//	attempt 3 → 30 minutes
//	attempt 4 → 2 hours
//	attempt 5 → 12 hours
var BackoffSchedule = []time.Duration{
	1 * time.Minute,
	5 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
	12 * time.Hour,
}

// NextBackoff returns the wait duration before the next attempt.
//
//	currentAttempt = 1 (just made attempt 1) → wait Backoff[0] = 1 minute
//	currentAttempt = 5 → wait Backoff[4] = 12 hours
//	currentAttempt = 6 → out of schedule, ok=false
//	currentAttempt = 0 → invalid, ok=false
func NextBackoff(currentAttempt int) (time.Duration, bool) {
	if currentAttempt < 1 {
		return 0, false
	}
	idx := currentAttempt - 1
	if idx >= len(BackoffSchedule) {
		return 0, false
	}
	return BackoffSchedule[idx], true
}

// IsRetryableResponse classifies an HTTP status code.
//
//	2xx → no retry (success)
//	408 / 429 / 5xx → retry
//	other 4xx → no retry (client bug)
func IsRetryableResponse(statusCode int) bool {
	if statusCode >= 200 && statusCode < 300 {
		return false
	}
	if statusCode == http.StatusRequestTimeout || statusCode == http.StatusTooManyRequests {
		return true
	}
	if statusCode >= 500 && statusCode < 600 {
		return true
	}
	return false
}