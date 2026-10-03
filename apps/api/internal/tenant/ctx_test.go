package tenant

import (
	"context"
	"errors"
	"testing"

	"github.com/gofrs/uuid"
)

func TestTenantRoundTrip(t *testing.T) {
	id, _ := uuid.NewV7()
	ctx := WithTenantID(context.Background(), id)

	got, err := TenantIDFromContext(ctx)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != id {
		t.Errorf("got %s want %s", got, id)
	}
}

func TestTenantMissingFromContext(t *testing.T) {
	_, err := TenantIDFromContext(context.Background())
	if !errors.Is(err, ErrTenantMissingFromContext) {
		t.Errorf("expected ErrTenantMissingFromContext, got %v", err)
	}
}

func TestTenantOrZero(t *testing.T) {
	if got := TenantIDFromContextOrZero(context.Background()); got != uuid.Nil {
		t.Errorf("expected uuid.Nil, got %s", got)
	}
	id, _ := uuid.NewV7()
	ctx := WithTenantID(context.Background(), id)
	if got := TenantIDFromContextOrZero(ctx); got != id {
		t.Errorf("got %s want %s", got, id)
	}
}