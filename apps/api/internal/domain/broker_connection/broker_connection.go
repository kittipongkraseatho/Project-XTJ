package broker_connection

import (
	"time"

	"github.com/gofrs/uuid"
)

type Provider string
type Status string

const (
	ProviderMock    Provider = "mock"
	ProviderMetaApi Provider = "metaapi"
	ProviderCTrader Provider = "ctrader"
	ProviderIB      Provider = "ib"

	StatusPending      Status = "pending"
	StatusConnecting   Status = "connecting"
	StatusConnected    Status = "connected"
	StatusDisconnected Status = "disconnected"
	StatusError        Status = "error"
)

// AllStatuses lists every valid Status (used by handlers for input validation).
var AllStatuses = []Status{
	StatusPending,
	StatusConnecting,
	StatusConnected,
	StatusDisconnected,
	StatusError,
}

// AllProviders lists every valid Provider.
var AllProviders = []Provider{
	ProviderMock,
	ProviderMetaApi,
	ProviderCTrader,
	ProviderIB,
}

func (s Status) IsValid() bool {
	for _, v := range AllStatuses {
		if v == s {
			return true
		}
	}
	return false
}

func (p Provider) IsValid() bool {
	for _, v := range AllProviders {
		if v == p {
			return true
		}
	}
	return false
}

type BrokerConnection struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	TradingAccountID  uuid.UUID
	Provider          Provider
	Status            Status
	ExternalAccountID *string
	CredentialsEnc    []byte
	Metadata          map[string]any
	LastSyncAt        *time.Time
	LastError         *string
	SyncIntervalSecs  int
	CreatedAt         time.Time
	UpdatedAt         time.Time
}