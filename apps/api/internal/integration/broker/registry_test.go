package broker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/trading-journal/api/internal/domain/broker_connection"
)

func TestRegistry_Get_Names(t *testing.T) {
	r := NewRegistry(map[broker_connection.Provider]Provider{
		broker_connection.ProviderMock: NewMockProvider(),
	})

	p, err := r.Get(broker_connection.ProviderMock)
	require.NoError(t, err)
	assert.Equal(t, broker_connection.ProviderMock, p.Name())

	_, err = r.Get(broker_connection.ProviderMetaApi)
	assert.ErrorIs(t, err, ErrProviderNotConfigured)
}

func TestRegistry_Has(t *testing.T) {
	r := NewRegistry(map[broker_connection.Provider]Provider{
		broker_connection.ProviderMock: NewMockProvider(),
	})
	assert.True(t, r.Has(broker_connection.ProviderMock))
	assert.False(t, r.Has(broker_connection.ProviderMetaApi))
}

func TestRegistry_Names(t *testing.T) {
	r := NewRegistry(map[broker_connection.Provider]Provider{
		broker_connection.ProviderMock: NewMockProvider(),
	})
	names := r.Names()
	assert.Len(t, names, 1)
	assert.Contains(t, names, broker_connection.ProviderMock)
}

func TestRegistry_Set_Runtime(t *testing.T) {
	r := NewRegistry(nil)
	assert.False(t, r.Has(broker_connection.ProviderMock))
	r.Set(broker_connection.ProviderMock, NewMockProvider())
	assert.True(t, r.Has(broker_connection.ProviderMock))
}