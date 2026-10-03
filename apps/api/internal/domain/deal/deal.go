package deal

import (
	"time"

	"github.com/gofrs/uuid"
)

type Type string
type EntryType string

const (
	TypeBuy       Type = "buy"
	TypeSell      Type = "sell"
	TypeBalance   Type = "balance"
	TypeCredit    Type = "credit"
	TypeCommission Type = "commission"
	TypeCorrection Type = "correction"

	EntryEntry     EntryType = "entry"
	EntryExit      EntryType = "exit"
	EntryEntryInOut EntryType = "entry_inout"
	EntryExitInOut  EntryType = "exit_inout"
	EntryInOut      EntryType = "inout"
)

type Deal struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	TradingAccountID uuid.UUID
	ImportJobID      *uuid.UUID
	ProviderDealID   string
	Symbol           string
	Type             Type
	EntryType        EntryType
	Volume           float64
	Price            float64
	Commission       float64
	Swap             float64
	Profit           float64
	Magic            *int64
	Comment          *string
	ExecutedAt       time.Time
	Metadata         map[string]any
	CreatedAt        time.Time
}