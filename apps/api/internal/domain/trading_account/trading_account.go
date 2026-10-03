package trading_account

import (
	"time"

	"github.com/gofrs/uuid"
)

type Broker string
type Kind string

const (
	BrokerMT5     Broker = "mt5"
	BrokerMT4     Broker = "mt4"
	BrokercTrader Broker = "ctrader"
	BrokerCustom  Broker = "custom"

	KindDemo Kind = "demo"
	KindReal Kind = "real"
)

type TradingAccount struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	TenantID        uuid.UUID
	Broker          Broker
	BrokerAccountID string
	DisplayName     string
	Currency        string
	Leverage        *float64
	Kind            Kind
	Metadata        map[string]any
	IsActive        bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}