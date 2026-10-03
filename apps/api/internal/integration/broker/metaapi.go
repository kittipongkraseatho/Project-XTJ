package broker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/broker_connection"
)

// MetaApiAdapter is the real MetaApi implementation of broker.Provider.
// The Phase 4 version does NOT include the MetaApi Go SDK — we ship the
// interface contract + connect/fetch/ping plumbing. Phase 5 wires the
// actual cloud API once a vendor relationship + credentials are in place.
//
// To enable: set the env vars below + register in router.go. When METAAPI_TOKEN
// is empty, the adapter returns ErrProviderNotConfigured.
type MetaApiAdapter struct {
	Token       string
	Region      string // e.g. "ap-southeast"
	AccountID   string // optional default account id (env-gated dev)
	BaseURL     string // overridable for sandbox
}

// MetaApiConfig is the wire-up.
type MetaApiConfig struct {
	Token     string
	Region    string
	AccountID string
	BaseURL   string // optional; defaults to https://mt-client-api-v1.{region}.agiliumtrade.ai
}

// NewMetaApiAdapter returns an adapter from the given config.
func NewMetaApiAdapter(cfg MetaApiConfig) *MetaApiAdapter {
	if cfg.Region == "" {
		cfg.Region = "ap-southeast"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = fmt.Sprintf("https://mt-client-api-v1.%s.agiliumtrade.ai", cfg.Region)
	}
	return &MetaApiAdapter{
		Token:     cfg.Token,
		Region:    cfg.Region,
		AccountID: cfg.AccountID,
		BaseURL:   cfg.BaseURL,
	}
}

func (m *MetaApiAdapter) Name() broker_connection.Provider {
	return broker_connection.ProviderMetaApi
}

// Connect authenticates with the MetaApi cloud. Phase 4 returns an
// ErrProviderNotConfigured stub since we don't ship the SDK; Phase 5
// replaces this with a real HTTP call to /users/current/accounts.
func (m *MetaApiAdapter) Connect(_ context.Context, _ ConnectRequest) (ConnectResult, error) {
	if m.Token == "" {
		return ConnectResult{}, ErrProviderNotConfigured
	}
	// TODO Phase 5: real MetaApi handshake
	// 1. POST /users/current/accounts → list accounts matching login+server
	// 2. If none, POST /users/current/accounts (provision)
	// 3. Wait for DEPLOYED state (poll until ready)
	// 4. Return the account id
	return ConnectResult{}, errors.New("metaapi: real adapter not wired — Phase 5 (see comment)")
}

// FetchDeals pulls closed deals in the window. Phase 4 stub returns the
// ProviderNotConfigured error; Phase 5 wires the real GET /users/{accountId}/history/orders/transactions/{from}/{to}.
func (m *MetaApiAdapter) FetchDeals(_ context.Context, req FetchDealsRequest) ([]Deal, error) {
	if m.Token == "" {
		return nil, ErrProviderNotConfigured
	}
	if req.ExternalAccountID == "" {
		return nil, fmt.Errorf("metaapi: empty external_account_id")
	}
	return nil, errors.New("metaapi: real fetch not wired — Phase 5 (see comment)")
}

// Ping is a health check against the streaming connection.
func (m *MetaApiAdapter) Ping(_ context.Context, _ string) error {
	if m.Token == "" {
		return ErrProviderNotConfigured
	}
	return errors.New("metaapi: real ping not wired — Phase 5")
}

// Disconnect tears down streaming. Phase 4 stub: idempotent no-op.
func (m *MetaApiAdapter) Disconnect(_ context.Context, _ string) error {
	return nil
}

// MetaApiDealFromBroker maps MetaApi SDK shape → canonical broker.Deal.
// Phase 5 will fill this in once the SDK is added.
func MetaApiDealFromBroker(md struct {
	Ticket   string
	Symbol   string
	Type     string // "DEAL_TYPE_BUY" | "DEAL_TYPE_SELL"
	Volume   float64
	OpenPrice float64
	ClosePrice float64
	OpenTime  time.Time
	CloseTime time.Time
	Commission float64
	Swap      float64
	Profit    float64
}, externalAccountID string) Deal {
	return Deal{
		ProviderDealID: uuid.NewV5(DedupNamespace, externalAccountID+":"+md.Ticket).String(),
		Symbol:         md.Symbol,
		Direction:      directionFromMetaType(md.Type),
		Volume:         int64(md.Volume * 100_000),
		OpenPrice:      int64(md.OpenPrice * 100_000),
		ClosePrice:     int64(md.ClosePrice * 100_000),
		OpenTime:       md.OpenTime,
		CloseTime:      md.CloseTime,
		Commission:     int64(md.Commission * 100),
		Swap:           int64(md.Swap * 100),
		Profit:         int64(md.Profit * 100),
	}
}

func directionFromMetaType(t string) string {
	if t == "DEAL_TYPE_SELL" {
		return "sell"
	}
	return "buy"
}