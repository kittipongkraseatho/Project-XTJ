package broker_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/trading-journal/api/internal/integration/broker"
)

// newMetaApiMockServer spins up an httptest server that pretends to be the
// MetaApi REST surface. `handler` decides what each endpoint returns.
// The handler receives the full request URI (uri + query) so the test
// can match query-string params too.
func newMetaApiMockServer(t *testing.T, handler func(uri string) (status int, body string)) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		uri := r.URL.RequestURI()
		status, body := handler(uri)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, calls
}

func newAdapter(baseURL string) *broker.MetaApiAdapter {
	return broker.NewMetaApiAdapter(broker.MetaApiConfig{
		Token:         "sk_test_dummy",
		Region:        "ap-southeast",
		BaseURL:       baseURL,
		DeployTimeout: 3 * time.Second,
		HTTPTimeout:   2 * time.Second,
	})
}

// ── 1. Connect finds an existing account ─────────────────────────────

func TestMetaApi_Connect_FindsExistingAccount(t *testing.T) {
	srv, calls := newMetaApiMockServer(t, func(uri string) (int, string) {
		switch {
		case strings.HasPrefix(uri, "/users/current/accounts?limit="):
			return 200, `[
				{"_id":"acc_1","login":"12345","server":"Broker-Demo","state":"DEPLOYED","type":"cloud","region":"ap-southeast"},
				{"_id":"acc_2","login":"99999","server":"Other-Server","state":"DEPLOYED","type":"cloud","region":"ap-southeast"}
			]`
		case uri == "/users/current/accounts/acc_1":
			return 200, `{"_id":"acc_1","state":"DEPLOYED"}`
		}
		return 404, "{}"
	})

	a := newAdapter(srv.URL)
	res, err := a.Connect(context.Background(), broker.ConnectRequest{
		Login: "12345", Server: "Broker-Demo",
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if res.ExternalAccountID != "acc_1" {
		t.Errorf("want acc_1, got %s", res.ExternalAccountID)
	}
	if got := calls.Load(); got < 2 {
		t.Errorf("expected at least 2 calls, got %d", got)
	}
}

// ── 2. Connect creates when account is missing ──────────────────────

func TestMetaApi_Connect_CreatesWhenMissing(t *testing.T) {
	srv, _ := newMetaApiMockServer(t, func(uri string) (int, string) {
		switch {
		case strings.HasPrefix(uri, "/users/current/accounts?limit="):
			return 200, `[]`
		case uri == "/users/current/accounts" && false:
			// Match POST uri — but our handler doesn't differentiate by method,
			// so the next case captures both list+create on root uri.
			return 200, `{"_id":"acc_new","login":"12345","server":"Broker-Demo","state":"CREATED"}`
		case uri == "/users/current/accounts":
			return 200, `{"_id":"acc_new","login":"12345","server":"Broker-Demo","state":"CREATED"}`
		case uri == "/users/current/accounts/acc_new":
			return 200, `{"_id":"acc_new","state":"DEPLOYED"}`
		}
		return 404, "{}"
	})

	a := newAdapter(srv.URL)
	res, err := a.Connect(context.Background(), broker.ConnectRequest{
		Login: "12345", Server: "Broker-Demo",
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if res.ExternalAccountID != "acc_new" {
		t.Errorf("want acc_new, got %s", res.ExternalAccountID)
	}
}

// ── 3. Connect polls until DEPLOYED ────────────────────────────────

func TestMetaApi_Connect_PollsUntilDeployed(t *testing.T) {
	polls := &atomic.Int32{}
	srv, _ := newMetaApiMockServer(t, func(uri string) (int, string) {
		switch {
		case strings.HasPrefix(uri, "/users/current/accounts?limit="):
			return 200, `[{"_id":"acc_x","login":"42","server":"S","state":"DEPLOYED","type":"cloud","region":"ap-southeast"}]`
		case uri == "/users/current/accounts/acc_x":
			n := polls.Add(1)
			if n < 3 {
				return 200, `{"_id":"acc_x","state":"CREATED"}`
			}
			return 200, `{"_id":"acc_x","state":"DEPLOYED"}`
		}
		return 404, "{}"
	})

	a := newAdapter(srv.URL)
	_, err := a.Connect(context.Background(), broker.ConnectRequest{
		Login: "42", Server: "S",
	})
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if polls.Load() != 3 {
		t.Errorf("want 3 polls, got %d", polls.Load())
	}
}

// ── 4. FetchDeals maps to canonical ────────────────────────────────

func TestMetaApi_FetchDeals_MapsToCanonical(t *testing.T) {
	srv, _ := newMetaApiMockServer(t, func(uri string) (int, string) {
		switch {
		case strings.HasPrefix(uri, "/users/current/accounts/acc_1/history/deals/tickets"):
			return 200, `[{"ticket":"t1","openTime":"2024-01-01T00:00:00.000Z","closeTime":"2024-01-01T01:00:00.000Z"}]`
		case strings.HasPrefix(uri, "/users/current/accounts/acc_1/history/deals/t1"):
			return 200, `{
				"ticket":"t1","symbol":"EURUSD","type":"DEAL_TYPE_BUY",
				"volume":1.0,"openPrice":1.234567,"closePrice":1.234568,
				"openTime":"2024-01-01T00:00:00.000Z","closeTime":"2024-01-01T01:00:00.000Z",
				"commission":0.005,"swap":0,"profit":50.005,"magic":42,"comment":"x"
			}`
		}
		return 404, "{}"
	})

	a := newAdapter(srv.URL)
	deals, err := a.FetchDeals(context.Background(), broker.FetchDealsRequest{
		ExternalAccountID: "acc_1",
		From:              time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		To:                time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("FetchDeals: %v", err)
	}
	if len(deals) != 1 {
		t.Fatalf("want 1 deal, got %d", len(deals))
	}
	d := deals[0]
	if d.Symbol != "EURUSD" {
		t.Errorf("symbol: %s", d.Symbol)
	}
	if d.Direction != "buy" {
		t.Errorf("direction: %s", d.Direction)
	}
	if d.Volume != 100000 {
		t.Errorf("volume (1.0 lot in microlots): %d", d.Volume)
	}
	if d.OpenPrice != 123457 {
		t.Errorf("openPrice: %d", d.OpenPrice)
	}
	if d.Commission != 1 { // 0.005 * 100, rounded to the nearest cent
		t.Errorf("commission: %d", d.Commission)
	}
	if d.Profit != 5001 { // 50.005 * 100, rounded to the nearest cent
		t.Errorf("profit: %d", d.Profit)
	}
	if d.ProviderDealID == "" {
		t.Errorf("provider deal id should be set")
	}
}

// ── 5. FetchDeals empty ──────────────────────────────────────────────

func TestMetaApi_FetchDeals_Empty(t *testing.T) {
	srv, _ := newMetaApiMockServer(t, func(uri string) (int, string) {
		if strings.HasPrefix(uri, "/users/current/accounts/acc_1/history/deals/tickets") {
			return 200, `[]`
		}
		return 404, "{}"
	})

	a := newAdapter(srv.URL)
	deals, err := a.FetchDeals(context.Background(), broker.FetchDealsRequest{
		ExternalAccountID: "acc_1",
		From:              time.Now().Add(-time.Hour),
		To:                time.Now(),
	})
	if err != nil {
		t.Fatalf("FetchDeals: %v", err)
	}
	if len(deals) != 0 {
		t.Errorf("want 0 deals, got %d", len(deals))
	}
}

// ── 6. Ping OK ───────────────────────────────────────────────────────

func TestMetaApi_Ping_OK(t *testing.T) {
	srv, _ := newMetaApiMockServer(t, func(uri string) (int, string) {
		if uri == "/users/current/accounts/acc_1" {
			return 200, `{"_id":"acc_1","state":"DEPLOYED"}`
		}
		return 404, "{}"
	})

	a := newAdapter(srv.URL)
	if err := a.Ping(context.Background(), "acc_1"); err != nil {
		t.Errorf("Ping: %v", err)
	}
}

// ── 7. Ping NotDeployed ─────────────────────────────────────────────

func TestMetaApi_Ping_NotDeployed(t *testing.T) {
	srv, _ := newMetaApiMockServer(t, func(uri string) (int, string) {
		if uri == "/users/current/accounts/acc_1" {
			return 200, `{"_id":"acc_1","state":"CREATED"}`
		}
		return 404, "{}"
	})

	a := newAdapter(srv.URL)
	err := a.Ping(context.Background(), "acc_1")
	if !errors.Is(err, broker.ErrNotDeployed) {
		t.Errorf("want ErrNotDeployed, got %v", err)
	}
}

// ── 8. Unauthorized → ErrProviderNotConfigured ───────────────────────

func TestMetaApi_Connect_Unauthorized(t *testing.T) {
	srv, _ := newMetaApiMockServer(t, func(uri string) (int, string) {
		return 401, `{"error":"unauthorized"}`
	})

	a := newAdapter(srv.URL)
	_, err := a.Connect(context.Background(), broker.ConnectRequest{
		Login: "1", Server: "S",
	})
	if !errors.Is(err, broker.ErrProviderNotConfigured) {
		t.Errorf("want ErrProviderNotConfigured, got %v", err)
	}
}

// ── Bonus: empty token ─────────────────────────────────────────────

func TestMetaApi_EmptyToken_ErrProviderNotConfigured(t *testing.T) {
	a := broker.NewMetaApiAdapter(broker.MetaApiConfig{
		Token: "", BaseURL: "http://example",
	})
	if _, err := a.Connect(context.Background(), broker.ConnectRequest{}); !errors.Is(err, broker.ErrProviderNotConfigured) {
		t.Errorf("want ErrProviderNotConfigured on empty token, got %v", err)
	}
	if _, err := a.FetchDeals(context.Background(), broker.FetchDealsRequest{}); !errors.Is(err, broker.ErrProviderNotConfigured) {
		t.Errorf("FetchDeals want ErrProviderNotConfigured on empty token, got %v", err)
	}
}

// Helper to silence "import unused" if we drop one.
var _ = fmt.Sprintf
