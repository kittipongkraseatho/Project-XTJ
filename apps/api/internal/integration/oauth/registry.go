package oauth

import (
	"fmt"
	"sync"

	"github.com/trading-journal/api/internal/store"
)

// Registry holds the OAuth providers enabled at boot.
// Providers with missing env vars are NOT registered.
type Registry struct {
	mu      sync.RWMutex
	byName  map[store.IdentityProvider]Provider
}

func NewRegistry() *Registry {
	return &Registry{byName: map[store.IdentityProvider]Provider{}}
}

func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.byName[p.Name()] = p
}

func (r *Registry) Get(name store.IdentityProvider) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.byName[name]
	if !ok {
		return nil, fmt.Errorf("provider %q not configured", name)
	}
	return p, nil
}

// List returns the names of every registered provider (used by FE to render buttons).
func (r *Registry) List() []store.IdentityProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]store.IdentityProvider, 0, len(r.byName))
	for n := range r.byName {
		out = append(out, n)
	}
	return out
}