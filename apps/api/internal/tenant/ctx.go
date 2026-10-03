// Package tenant provides the typed context key for tenant_id propagation.
//
// Every service / store call that touches a tenant-scoped table MUST read
// tenant_id via TenantIDFromContext. Missing ctx → ErrTenantMissingFromContext
// (programmer error; surfaces as 500 INTERNAL_ERROR).
package tenant

import (
	"context"
	"errors"

	"github.com/gofrs/uuid"
)

type ctxKey int

const tenantKey ctxKey = iota

// WithTenantID returns a new context carrying the given tenant ID.
func WithTenantID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, tenantKey, id)
}

// TenantIDFromContext returns the tenant ID, or ErrTenantMissingFromContext if absent.
var ErrTenantMissingFromContext = errors.New("tenant id missing from context")

func TenantIDFromContext(ctx context.Context) (uuid.UUID, error) {
	v, ok := ctx.Value(tenantKey).(uuid.UUID)
	if !ok {
		return uuid.Nil, ErrTenantMissingFromContext
	}
	return v, nil
}

// TenantIDFromContextOrZero returns the tenant ID, or uuid.Nil if absent.
// Use ONLY in places where you can defensively recover (e.g. background jobs
// where you have an explicit tenant ID). Prefer TenantIDFromContext in HTTP paths.
func TenantIDFromContextOrZero(ctx context.Context) uuid.UUID {
	v, ok := ctx.Value(tenantKey).(uuid.UUID)
	if !ok {
		return uuid.Nil
	}
	return v
}