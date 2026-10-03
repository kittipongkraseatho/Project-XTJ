package broker

import (
	"sync"

	"github.com/trading-journal/api/internal/domain/broker_connection"
)

// Registry holds the registered providers by name.
//
// Single-writer / multi-reader: the Set method is guarded by mu; Get is racy
// but the map is only mutated at boot time and then reads as a plain map (no
// goroutine writes after server start).
type Registry struct {
	mu sync.RWMutex
	m  map[broker_connection.Provider]Provider
}

// NewRegistry builds a Registry from a map literal. The map is copied to
// decouple the registry from the caller's reference.
func NewRegistry(providers map[broker_connection.Provider]Provider) *Registry {
	r := &Registry{m: make(map[broker_connection.Provider]Provider, len(providers))}
	for k, v := range providers {
		r.m[k] = v
	}
	return r
}

// Get returns the Provider for the given name, or ErrProviderNotConfigured.
func (r *Registry) Get(name broker_connection.Provider) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.m[name]
	if !ok {
		return nil, ErrProviderNotConfigured
	}
	return p, nil
}

// Has reports whether a provider is registered. Used by HTTP handlers to
// validate the `provider` body field before enqueuing work.
func (r *Registry) Has(name broker_connection.Provider) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.m[name]
	return ok
}

// Names returns the list of registered provider names. Drives the FE dropdown.
func (r *Registry) Names() []broker_connection.Provider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]broker_connection.Provider, 0, len(r.m))
	for k := range r.m {
		out = append(out, k)
	}
	return out
}

// Set swaps in a provider. Production should call this only at boot.
func (r *Registry) Set(name broker_connection.Provider, p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[name] = p
}