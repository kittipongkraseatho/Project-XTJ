package trade

import (
	"time"

	"github.com/gofrs/uuid"
)

type Direction string
type Outcome string

const (
	DirectionLong  Direction = "long"
	DirectionShort Direction = "short"

	OutcomeWin      Outcome = "win"
	OutcomeLoss     Outcome = "loss"
	OutcomeBreakeven Outcome = "breakeven"
)

type Trade struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	UserID           uuid.UUID
	TradingAccountID uuid.UUID
	PositionID       *uuid.UUID
	Symbol           string
	Direction        Direction
	Volume           float64
	OpenPrice        float64
	ClosePrice       *float64
	OpenTime         time.Time
	CloseTime        *time.Time
	HoldingSeconds   *int64
	GrossProfit      float64
	GrossLoss        float64
	NetPL            float64
	Commission       float64
	Swap             float64
	Magic            *int64
	Outcome          Outcome
	IsPartial        bool
	Metadata         map[string]any
	CreatedAt        time.Time
	UpdatedAt        time.Time
}