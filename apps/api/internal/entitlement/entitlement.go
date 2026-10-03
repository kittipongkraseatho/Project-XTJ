// Package entitlement translates a tenant's plan code into concrete feature
// limits (max trading accounts, max broker connections).
//
// Phase 3 ships a static map; the values also live on the `plans.metadata`
// jsonb so admin tooling can override without a code change. Phase 4 (billing)
// will move this to a dedicated table driven by Stripe/Omise subscription state.
package entitlement

import (
	"context"

	"github.com/gofrs/uuid"

	"github.com/trading-journal/api/internal/domain/plan"
	"github.com/trading-journal/api/internal/store"
)

type Limits struct {
	MaxTradingAccounts   int
	MaxBrokerConnections int
}

var limitsByPlanCode = map[plan.Code]Limits{
	plan.CodeFree:          {MaxTradingAccounts: 1, MaxBrokerConnections: 1},
	plan.CodeUploadJournal: {MaxTradingAccounts: 1, MaxBrokerConnections: 1},
	plan.CodeStarter:       {MaxTradingAccounts: 3, MaxBrokerConnections: 2},
	plan.CodePro:           {MaxTradingAccounts: 5, MaxBrokerConnections: 3},
}

// Resolve returns the Limits for a plan code. Unknown plans get the free tier
// (safe default — least privilege).
func Resolve(code plan.Code) Limits {
	if l, ok := limitsByPlanCode[code]; ok {
		return l
	}
	return limitsByPlanCode[plan.CodeFree]
}

// MaxTradingAccounts is a convenience accessor.
func MaxTradingAccounts(code plan.Code) int { return Resolve(code).MaxTradingAccounts }

// MaxBrokerConnections is a convenience accessor.
func MaxBrokerConnections(code plan.Code) int { return Resolve(code).MaxBrokerConnections }

// LookupForTenant resolves a tenant's current plan code via the subscriptions
// + plans repos and returns the matching Limits. Returns free-tier limits if
// the tenant has no active subscription (newly-registered tenants default to
// 'free' per Phase 0 signup).
func LookupForTenant(ctx context.Context, st *store.Store, tenantID uuid.UUID) (plan.Code, Limits) {
	sub, err := st.Subscriptions.GetActiveByTenant(ctx, tenantID)
	if err != nil || sub == nil {
		return plan.CodeFree, Resolve(plan.CodeFree)
	}
	p, err := st.Plans.GetByID(ctx, sub.PlanID)
	if err != nil || p == nil {
		return plan.CodeFree, Resolve(plan.CodeFree)
	}
	return p.Code, Resolve(p.Code)
}