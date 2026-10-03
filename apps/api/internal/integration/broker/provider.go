// Package broker is the abstraction layer over trading-broker APIs.
//
// Today the only registered implementation is the deterministic MockProvider
// (mock.go), which the SyncWorker polls to demo the full connect→sync→dedup
// flow with no external credentials. A real MetaApi (or cTrader) provider would
// implement the same Provider interface and be wired into Registry from config.
//
// Deal is the canonical in-memory shape. It is intentionally close to the
// MT5-parser output so the SyncService can reuse ImportService.WriteDeals as
// the single dedup pipeline for BOTH manual uploads and broker syncs.
package broker

import (
	"context"
	"errors"
	"time"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/broker_connection"
)

// Provider is the broker contract.
//
// All methods are safe for concurrent use. Each Provider is responsible for
// its own connection state (MetaApi uses a region-prefixed session; cTrader
// uses a long-lived token; the Mock uses an in-process UUID).
type Provider interface {
	// Name returns the canonical provider identifier — matches broker_connection.Provider.
	Name() broker_connection.Provider

	// Connect authenticates with the broker. For Mock, ignores credentials
	// and returns a freshly-minted UUIDv7 as the external_account_id.
	Connect(ctx context.Context, req ConnectRequest) (ConnectResult, error)

	// Disconnect releases any resources held for this external_account_id.
	// Idempotent: a second call on an already-disconnected id is a no-op.
	Disconnect(ctx context.Context, externalAccountID string) error

	// FetchDeals returns canonical deals in the inclusive-from, exclusive-to window.
	// Implementations MUST be deterministic for the same (externalAccountID, from, to)
	// so dedup tests can assert "second sync imports 0".
	FetchDeals(ctx context.Context, req FetchDealsRequest) ([]Deal, error)

	// Ping checks the connection is still up.
	// Used by the SyncWorker before a scheduled poll and by HTTP reconnect tests.
	Ping(ctx context.Context, externalAccountID string) error
}

// ConnectRequest is what the HTTP handler hands the Provider when the user
// submits a "Connect Broker" dialog.
type ConnectRequest struct {
	Login    string            // broker login (mock: ignored)
	Password string            // broker password (mock: NEVER store, NEVER log)
	Server   string            // broker server name (mock: stored in metadata)
	Extra    map[string]string // mock: ignored
}

// ConnectResult is what Connect returns.
type ConnectResult struct {
	ExternalAccountID string         // provider-side account id (mock: UUIDv7)
	Metadata          map[string]any // merged into broker_connections.metadata
}

// FetchDealsRequest is what the SyncWorker passes to FetchDeals.
type FetchDealsRequest struct {
	ExternalAccountID string
	From              time.Time // inclusive
	To                time.Time // exclusive
	Limit             int       // 0 = no cap
}

// Deal is the canonical shape both the MT5 parser and the broker API emit.
// Money is in minor units (cents or pips×volume) — never float.
type Deal struct {
	ProviderDealID string    // stable per broker; UUIDv5(dedup_namespace, id) recommended
	Symbol         string    // e.g. "EURUSD"
	Direction      string    // "buy" | "sell"
	Volume         int64     // micro lots (1.0 lot = 100_000)
	OpenPrice      int64     // minor units (×100_000 for 5-decimal pairs)
	ClosePrice     int64     // minor units
	OpenTime       time.Time // UTC
	CloseTime      time.Time // UTC
	Commission     int64
	Swap           int64
	Profit         int64
}

// DedupNamespace is the UUIDv5 namespace for stable provider deal id derivation
// from broker-side identifiers (deal ticket / broker order id).
var DedupNamespace = uuid.NewV5(uuid.NamespaceOID, "trading-journal/broker/deal-dedup/v1")

// ErrProviderNotConfigured is returned by Registry.Get when the requested
// provider is not registered (e.g. MetaApi in Phase 2 — env vars not set).
var ErrProviderNotConfigured = errors.New("broker provider not configured")

// ErrPingFailed is returned by Ping when the broker side reports the connection is dead.
var ErrPingFailed = errors.New("broker ping failed")