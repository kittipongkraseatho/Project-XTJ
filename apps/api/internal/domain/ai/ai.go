// Package ai — domain types for the Phase 6 AI analysis system.
package ai

import (
	"time"

	"github.com/gofrs/uuid"
)

// Analysis status.
const (
	StatusPending   = "pending"
	StatusGenerating = "generating"
	StatusSuccess   = "success"
	StatusFailed    = "failed"
)

// AllStatuses lists every valid status (used by the FE badge).
var AllStatuses = []string{StatusPending, StatusGenerating, StatusSuccess, StatusFailed}

// Analysis is one row of trade_analyses.
type Analysis struct {
	ID                 uuid.UUID
	TenantID           uuid.UUID
	TradeID            uuid.UUID
	PromptHash         string
	Content            *string
	TokensIn           int
	TokensOut          int
	ModelID            string
	Provider           string // "stub" | "openai" | "anthropic"
	Status             string
	Error             *string
	RequestedByUserID  uuid.UUID
	RequestedAt        time.Time
	CompletedAt        *time.Time
}

// DailyUsage is one row of ai_usage_daily + the budget ceiling for the plan.
type DailyUsage struct {
	Day            time.Time
	Requests       int
	TokensIn       int64
	TokensOut      int64
	DailyBudget    int // based on plan code
	HardDailyCap   int // absolute ceiling regardless of plan
}

// Budget ceilings.
const (
	FreeDailyBudget = 5
	ProDailyBudget  = 50
	HardDailyCap    = 100
)

// BudgetForPlan returns the soft daily budget for a plan code.
// Free / starter / unknown plans all fall back to the free ceiling.
func BudgetForPlan(planCode string) int {
	switch planCode {
	case "pro":
		return ProDailyBudget
	default:
		return FreeDailyBudget
	}
}

// BudgetHeadroom returns how many analyses are still allowed today
// (0 if exhausted).
func BudgetHeadroom(u DailyUsage) int {
	budget := u.DailyBudget
	if budget > HardDailyCap {
		budget = HardDailyCap
	}
	remain := budget - u.Requests
	if remain < 0 {
		return 0
	}
	return remain
}