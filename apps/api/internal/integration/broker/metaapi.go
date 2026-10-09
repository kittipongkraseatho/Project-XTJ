// Package broker — MetaApiAdapter talks to the MetaApi Cloud REST API
// directly (no third-party SDK). Phase 4 shipped a stub returning
// ErrProviderNotConfigured; Phase 8 wires the real handshake.
//
// Production checklist (per https://metaapi.cloud/docs/client/):
//
//   - Provision an account: POST /users/current/accounts
//     { login, server, type, ... }
//   - Poll until state=DEPLOYED (server is provisioned).
//   - Read deal history: GET /users/current/accounts/{id}/history/deals/{ticket}
//
// Auth: bearer token in `auth-token` header.
package broker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/broker_connection"
)

// ── Errors ───────────────────────────────────────────────────────────

var (
	// ErrDeployTimeout is returned when the account doesn't reach
	// DEPLOYED state within the configured timeout.
	ErrDeployTimeout = errors.New("broker: metaapi account deploy timeout")

	// ErrNotDeployed is returned by Ping when the account state isn't DEPLOYED.
	ErrNotDeployed = errors.New("broker: metaapi account not deployed")

	// ErrUpstream is returned for 5xx responses.
	ErrUpstream = errors.New("broker: metaapi upstream error")

	// ErrNotFound mirrors the standard "resource not found" reply.
	ErrNotFound = errors.New("broker: metaapi not found")
)

// ── Config ───────────────────────────────────────────────────────────

// MetaApiConfig is the wire-up.
type MetaApiConfig struct {
	Token         string
	Region        string
	AccountID     string
	BaseURL       string
	DeployTimeout time.Duration // default 10s
	HTTPTimeout   time.Duration // default 15s
}

// NewMetaApiAdapter builds an adapter.
func NewMetaApiAdapter(cfg MetaApiConfig) *MetaApiAdapter {
	if cfg.Region == "" {
		cfg.Region = "ap-southeast"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = fmt.Sprintf("https://mt-client-api-v1.%s.agiliumtrade.ai", cfg.Region)
	}
	if cfg.DeployTimeout == 0 {
		cfg.DeployTimeout = 10 * time.Second
	}
	if cfg.HTTPTimeout == 0 {
		cfg.HTTPTimeout = 15 * time.Second
	}
	return &MetaApiAdapter{
		Token:         cfg.Token,
		Region:        cfg.Region,
		AccountID:     cfg.AccountID,
		BaseURL:       cfg.BaseURL,
		DeployTimeout: cfg.DeployTimeout,
		HTTPTimeout:   cfg.HTTPTimeout,
	}
}

// MetaApiAdapter is the production broker.Provider impl.
type MetaApiAdapter struct {
	Token         string
	Region        string
	AccountID     string
	BaseURL       string
	DeployTimeout time.Duration
	HTTPTimeout   time.Duration
	http          *http.Client
}

// Name implements Provider.
func (m *MetaApiAdapter) Name() broker_connection.Provider {
	return broker_connection.ProviderMetaApi
}

// ── HTTP helpers ────────────────────────────────────────────────────

// httpClient returns the adapter's http.Client (lazy).
func (m *MetaApiAdapter) httpClient() *http.Client {
	if m.http != nil {
		return m.http
	}
	m.http = &http.Client{Timeout: m.HTTPTimeout}
	return m.http
}

// doJSON issues a request with auth + JSON body, decodes the response.
// For GET requests, pass body=nil.
func (m *MetaApiAdapter) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		bs, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		rdr = bytes.NewReader(bs)
	}
	u := strings.TrimRight(m.BaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("auth-token", m.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := m.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer res.Body.Close()
	bs, _ := io.ReadAll(res.Body)
	switch {
	case res.StatusCode == 401 || res.StatusCode == 403:
		return ErrProviderNotConfigured
	case res.StatusCode == 404:
		return ErrNotFound
	case res.StatusCode >= 500:
		return fmt.Errorf("%w: status %d", ErrUpstream, res.StatusCode)
	case res.StatusCode >= 400:
		return fmt.Errorf("metaapi: client error %d: %s", res.StatusCode, string(bs))
	}
	if out != nil && len(bs) > 0 {
		if err := json.Unmarshal(bs, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// ── Connect ─────────────────────────────────────────────────────────

// accountListItem mirrors /users/current/accounts items.
type accountListItem struct {
	ID     string `json:"_id"`
	Login  string `json:"login"`
	Server string `json:"server"`
	State  string `json:"state"`
	Type   string `json:"type"`
	Region string `json:"region"`
}

// accountStateBody mirrors /users/current/accounts/{id}.
type accountStateBody struct {
	ID    string `json:"_id"`
	State string `json:"state"`
}

// createAccountBody is the payload for POST /users/current/accounts.
type createAccountBody struct {
	Login    string `json:"login"`
	Server   string `json:"server"` // e.g. "Broker-Demo"
	Type     string `json:"type"`   // "cloud" | "self-hosted"
	Region   string `json:"region"` // matches adapter Region
	Platform string `json:"platform,omitempty"`
}

// Connect implements Provider.
func (m *MetaApiAdapter) Connect(ctx context.Context, req ConnectRequest) (ConnectResult, error) {
	if m.Token == "" {
		return ConnectResult{}, ErrProviderNotConfigured
	}
	if req.Login == "" || req.Server == "" {
		return ConnectResult{}, errors.New("metaapi: login + server required")
	}

	// 1. Try to find an existing account.
	var accounts []accountListItem
	listPath := "/users/current/accounts?limit=1000"
	if err := m.doJSON(ctx, http.MethodGet, listPath, nil, &accounts); err != nil {
		// 404 on list is treated as empty.
		if !errors.Is(err, ErrNotFound) {
			return ConnectResult{}, err
		}
	}
	var match *accountListItem
	for i := range accounts {
		if accounts[i].Login == req.Login && accounts[i].Server == req.Server {
			match = &accounts[i]
			break
		}
	}

	// 2. Create if missing.
	if match == nil {
		newAcc := createAccountBody{
			Login:    req.Login,
			Server:   req.Server,
			Type:     "cloud",
			Region:   m.Region,
			Platform: "mt5",
		}
		var created accountListItem
		if err := m.doJSON(ctx, http.MethodPost, "/users/current/accounts", newAcc, &created); err != nil {
			return ConnectResult{}, fmt.Errorf("create: %w", err)
		}
		match = &created
	}

	// 3. Poll until DEPLOYED.
	deployCtx, cancel := context.WithTimeout(ctx, m.DeployTimeout)
	defer cancel()
	for {
		var st accountStateBody
		err := m.doJSON(deployCtx, http.MethodGet, "/users/current/accounts/"+match.ID, nil, &st)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return ConnectResult{}, err
		}
		if st.State == "DEPLOYED" {
			return ConnectResult{
				ExternalAccountID: match.ID,
				Metadata: map[string]any{
					"state":  "DEPLOYED",
					"region": m.Region,
				},
			}, nil
		}
		select {
		case <-deployCtx.Done():
			return ConnectResult{}, ErrDeployTimeout
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// ── FetchDeals ──────────────────────────────────────────────────────

// dealTicketItem mirrors /history/deals/tickets.
type dealTicketItem struct {
	Ticket    string `json:"ticket"`
	OpenTime  string `json:"openTime"`
	CloseTime string `json:"closeTime"`
}

// dealDetailBody mirrors /history/deals/{ticket}.
type dealDetailBody struct {
	Ticket     string  `json:"ticket"`
	Symbol     string  `json:"symbol"`
	Type       string  `json:"type"` // "DEAL_TYPE_BUY" | "DEAL_TYPE_SELL"
	Volume     float64 `json:"volume"`
	OpenPrice  float64 `json:"openPrice"`
	ClosePrice float64 `json:"closePrice"`
	OpenTime   string  `json:"openTime"`
	CloseTime  string  `json:"closeTime"`
	Commission float64 `json:"commission"`
	Swap       float64 `json:"swap"`
	Profit     float64 `json:"profit"`
	Magic      int64   `json:"magic"`
	Comment    string  `json:"comment"`
}

// FetchDeals implements Provider.
func (m *MetaApiAdapter) FetchDeals(ctx context.Context, req FetchDealsRequest) ([]Deal, error) {
	if m.Token == "" {
		return nil, ErrProviderNotConfigured
	}
	if req.ExternalAccountID == "" {
		return nil, errors.New("metaapi: empty external_account_id")
	}
	start := req.From.UTC().Format("2006-01-02T15:04:05.000Z")
	end := req.To.UTC().Format("2006-01-02T15:04:05.000Z")

	// 1. List tickets.
	ticketsPath := fmt.Sprintf(
		"/users/current/accounts/%s/history/deals/tickets?startTime=%s&endTime=%s&limit=1000",
		url.PathEscape(req.ExternalAccountID), start, end)
	var tickets []dealTicketItem
	if err := m.doJSON(ctx, http.MethodGet, ticketsPath, nil, &tickets); err != nil {
		return nil, err
	}

	// 2. Fetch each deal.
	out := make([]Deal, 0, len(tickets))
	for _, t := range tickets {
		dealPath := fmt.Sprintf(
			"/users/current/accounts/%s/history/deals/%s",
			url.PathEscape(req.ExternalAccountID), url.PathEscape(t.Ticket))
		var detail dealDetailBody
		if err := m.doJSON(ctx, http.MethodGet, dealPath, nil, &detail); err != nil {
			return nil, fmt.Errorf("fetch deal %s: %w", t.Ticket, err)
		}
		out = append(out, mapMetaApiDeal(detail, req.ExternalAccountID))
	}
	return out, nil
}

// mapMetaApiDeal maps MetaApi deal shape → canonical Deal.
func mapMetaApiDeal(d dealDetailBody, externalAccountID string) Deal {
	openTime, _ := time.Parse(time.RFC3339Nano, d.OpenTime)
	closeTime, _ := time.Parse(time.RFC3339Nano, d.CloseTime)
	provID := uuid.NewV5(DedupNamespace, externalAccountID+":"+d.Ticket).String()
	return Deal{
		ProviderDealID: provID,
		Symbol:         d.Symbol,
		Direction:      directionFromMetaType(d.Type),
		Volume:         scaledInt64(d.Volume, 100_000),
		OpenPrice:      scaledInt64(d.OpenPrice, 100_000),
		ClosePrice:     scaledInt64(d.ClosePrice, 100_000),
		OpenTime:       openTime,
		CloseTime:      closeTime,
		Commission:     scaledInt64(d.Commission, 100),
		Swap:           scaledInt64(d.Swap, 100),
		Profit:         scaledInt64(d.Profit, 100),
	}
}

// scaledInt64 converts an external decimal number to the canonical integer
// unit using round-to-nearest. Direct int64(value*scale) truncates fractional
// units and can lose a unit because decimal JSON values are represented as
// binary floating point during decoding.
func scaledInt64(value float64, scale int64) int64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return int64(math.Round(value * float64(scale)))
}

func directionFromMetaType(t string) string {
	if strings.Contains(strings.ToUpper(t), "SELL") {
		return "sell"
	}
	return "buy"
}

// ── Ping ─────────────────────────────────────────────────────────────

// Ping implements Provider.
func (m *MetaApiAdapter) Ping(ctx context.Context, externalAccountID string) error {
	if m.Token == "" {
		return ErrProviderNotConfigured
	}
	if externalAccountID == "" {
		return ErrProviderNotConfigured
	}
	var st accountStateBody
	err := m.doJSON(ctx, http.MethodGet,
		"/users/current/accounts/"+url.PathEscape(externalAccountID), nil, &st)
	if err != nil {
		return err
	}
	if st.State != "DEPLOYED" {
		return ErrNotDeployed
	}
	return nil
}

// ── Disconnect ───────────────────────────────────────────────────────

// Disconnect implements Provider. Idempotent no-op for REST; streaming
// teardown lives in Phase 9+ (we don't use streaming in Phase 8).
func (m *MetaApiAdapter) Disconnect(_ context.Context, _ string) error {
	return nil
}

// ── helpers used by tests / future SDK adapter ───────────────────────

// MetaApiDealFromBroker maps MetaApi SDK shape → canonical Deal.
// Kept for future SDK adapter parity.
func MetaApiDealFromBroker(md struct {
	Ticket     string
	Symbol     string
	Type       string
	Volume     float64
	OpenPrice  float64
	ClosePrice float64
	OpenTime   time.Time
	CloseTime  time.Time
	Commission float64
	Swap       float64
	Profit     float64
	Magic      int64
	Comment    string
}, externalAccountID string) Deal {
	return Deal{
		ProviderDealID: uuid.NewV5(DedupNamespace, externalAccountID+":"+md.Ticket).String(),
		Symbol:         md.Symbol,
		Direction:      directionFromMetaType(md.Type),
		Volume:         scaledInt64(md.Volume, 100_000),
		OpenPrice:      scaledInt64(md.OpenPrice, 100_000),
		ClosePrice:     scaledInt64(md.ClosePrice, 100_000),
		OpenTime:       md.OpenTime,
		CloseTime:      md.CloseTime,
		Commission:     scaledInt64(md.Commission, 100),
		Swap:           scaledInt64(md.Swap, 100),
		Profit:         scaledInt64(md.Profit, 100),
	}
}

// unused helper to silence "import unused" if strconv is dropped.
var _ = strconv.Itoa
