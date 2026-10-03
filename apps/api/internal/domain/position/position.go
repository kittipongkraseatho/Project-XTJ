package position

import (
	"time"

	"github.com/gofrs/uuid"
)

type Direction string
type State string

const (
	DirectionLong  Direction = "long"
	DirectionShort Direction = "short"

	StateOpen   State = "open"
	StateClosed State = "closed"
)

type Position struct {
	ID                 uuid.UUID
	TenantID           uuid.UUID
	TradingAccountID   uuid.UUID
	ProviderPositionID string
	Direction          Direction
	Symbol             string
	Volume             float64
	OpenPrice          float64
	ClosePrice         *float64
	OpenTime           time.Time
	CloseTime          *time.Time
	State              State
	RealizedPL         float64
	Commission         float64
	Swap               float64
	Magic              *int64
	Metadata           map[string]any
	CreatedAt          time.Time
	UpdatedAt          time.Time
}