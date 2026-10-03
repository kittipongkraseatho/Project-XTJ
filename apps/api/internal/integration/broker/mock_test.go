package broker

import (
	"context"
	"testing"
	"time"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/trading-journal/api/internal/domain/broker_connection"
)

// TestMockProvider_Name guards the registry contract.
func TestMockProvider_Name(t *testing.T) {
	p := NewMockProvider()
	assert.Equal(t, broker_connection.ProviderMock, p.Name())
}

// TestMockProvider_Connect asserts Connect returns a non-empty external_account_id
// and propagates the server name into metadata.
func TestMockProvider_Connect(t *testing.T) {
	p := NewMockProvider()
	res, err := p.Connect(context.Background(), ConnectRequest{
		Login: "ignored-on-server", Server: "MetaQuotes-Demo",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, res.ExternalAccountID)
	_, err = uuid.FromString(res.ExternalAccountID)
	require.NoError(t, err, "external_account_id must be UUID-formatted")
	assert.Equal(t, "MetaQuotes-Demo", res.Metadata["server"])
}

// TestMockProvider_Disconnect is the idempotent no-op guard.
func TestMockProvider_Disconnect(t *testing.T) {
	p := NewMockProvider()
	assert.NoError(t, p.Disconnect(context.Background(), "anything"))
	assert.NoError(t, p.Disconnect(context.Background(), ""))
}

// TestMockProvider_Ping is the always-OK guard.
func TestMockProvider_Ping(t *testing.T) {
	p := NewMockProvider()
	assert.NoError(t, p.Ping(context.Background(), "any"))
}

// TestMockProvider_FetchDeals_Deterministic is the headline guarantee of Phase 2:
//
//   - Same (externalAccountID, from, to) twice → exactly the same Set of deals
//     (same provider_deal_id values, same symbols, same open times).
//
// This is what lets the unique constraint `UNIQUE(tenant_id, account_id,
// provider_deal_id)` dedupe all second-sync imports to 0 new deals.
func TestMockProvider_FetchDeals_Deterministic(t *testing.T) {
	p := NewMockProvider()
	ext := "01H7Z5YK9EXAMPLEEXTERNALID"
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 1, 8, 0, 0, 0, 0, time.UTC)

	first, err := p.FetchDeals(context.Background(), FetchDealsRequest{
		ExternalAccountID: ext, From: from, To: to,
	})
	require.NoError(t, err)

	second, err := p.FetchDeals(context.Background(), FetchDealsRequest{
		ExternalAccountID: ext, From: from, To: to,
	})
	require.NoError(t, err)

	require.Len(t, second, len(first), "deterministic count")
	for i := range first {
		assert.Equal(t, first[i].ProviderDealID, second[i].ProviderDealID, "deal_id[%d]", i)
		assert.Equal(t, first[i].Symbol, second[i].Symbol, "symbol[%d]", i)
		assert.Equal(t, first[i].OpenTime, second[i].OpenTime, "open_time[%d]", i)
	}
}

// TestMockProvider_FetchDeals_DifferentAccounts asserts distinct accounts
// generate distinct deal streams (mirror of broker reality).
func TestMockProvider_FetchDeals_DifferentAccounts(t *testing.T) {
	p := NewMockProvider()
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 1, 8, 0, 0, 0, 0, time.UTC)

	a, err := p.FetchDeals(context.Background(), FetchDealsRequest{
		ExternalAccountID: "account-A", From: from, To: to,
	})
	require.NoError(t, err)
	b, err := p.FetchDeals(context.Background(), FetchDealsRequest{
		ExternalAccountID: "account-B", From: from, To: to,
	})
	require.NoError(t, err)

	// Deal sets can occasionally overlap by chance, but external_account_id is
	// part of the deal_id derivation so provider_deal_ids MUST differ whenever
	// the external_account_id differs.
	idsA := make(map[string]bool, len(a))
	for _, d := range a {
		idsA[d.ProviderDealID] = true
	}
	for _, d := range b {
		assert.False(t, idsA[d.ProviderDealID], "deal_id %s leaked from account-A", d.ProviderDealID)
	}
}

// TestMockProvider_FetchDeals_EmptyOnEmptyWindow guards zero-length [from,to).
func TestMockProvider_FetchDeals_EmptyOnEmptyWindow(t *testing.T) {
	p := NewMockProvider()
	at := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	deals, err := p.FetchDeals(context.Background(), FetchDealsRequest{
		ExternalAccountID: "x", From: at, To: at,
	})
	require.NoError(t, err)
	assert.Empty(t, deals)
}

// TestMockProvider_FetchDeals_EmptyAccountID is the input-validation guard.
func TestMockProvider_FetchDeals_EmptyAccountID(t *testing.T) {
	p := NewMockProvider()
	_, err := p.FetchDeals(context.Background(), FetchDealsRequest{
		ExternalAccountID: "",
		From:              time.Now().Add(-24 * time.Hour),
		To:                time.Now(),
	})
	assert.Error(t, err)
}

// TestMockProvider_FetchDeals_SortedByOpenTime asserts the stable order that
// downstream ImportService.Enqueue assumes.
func TestMockProvider_FetchDeals_SortedByOpenTime(t *testing.T) {
	p := NewMockProvider()
	deals, err := p.FetchDeals(context.Background(), FetchDealsRequest{
		ExternalAccountID: "01H7Z5YK9EXAMPLE",
		From:              time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		To:                time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	for i := 1; i < len(deals); i++ {
		assert.False(t, deals[i].OpenTime.Before(deals[i-1].OpenTime), "deals must be sorted ascending by OpenTime; got %v before %v at idx %d", deals[i].OpenTime, deals[i-1].OpenTime, i)
	}
}

// TestMockProvider_FetchDeals_DealIDDerivation is the UUIDv5 stability test:
// same payload → same UUID. A change to the derivation breaks Phase 2 dedup.
func TestMockProvider_FetchDeals_DealIDDerivation(t *testing.T) {
	id1 := deriveDealID("01ABC", time.Unix(1735689600, 0).UTC(), "EURUSD", 1_080_000, 0)
	id2 := deriveDealID("01ABC", time.Unix(1735689600, 0).UTC(), "EURUSD", 1_080_000, 0)
	id3 := deriveDealID("01ABC", time.Unix(1735689601, 0).UTC(), "EURUSD", 1_080_000, 0)
	id4 := deriveDealID("01ABC", time.Unix(1735689600, 0).UTC(), "GBPUSD", 1_080_000, 0)
	id5 := deriveDealID("01ABC", time.Unix(1735689600, 0).UTC(), "EURUSD", 1_080_001, 0)
	id6 := deriveDealID("01ABC", time.Unix(1735689600, 0).UTC(), "EURUSD", 1_080_000, 1)

	assert.Equal(t, id1, id2, "identical payload must produce identical id")
	assert.NotEqual(t, id1, id3, "time differs → id differs")
	assert.NotEqual(t, id1, id4, "symbol differs → id differs")
	assert.NotEqual(t, id1, id5, "price differs → id differs")
	assert.NotEqual(t, id1, id6, "slot differs → id differs")
}