// Package ai — domain types for the Phase 6 + Phase 9 AI system.
package ai

import (
	"time"

	"github.com/gofrs/uuid"
)

// Analysis status (Phase 6 — reused for recap + chat messages).
const (
	StatusPending    = "pending"
	StatusGenerating = "generating"
	StatusSuccess    = "success"
	StatusFailed     = "failed"
)

// AllStatuses lists every valid status (used by the FE badge).
var AllStatuses = []string{StatusPending, StatusGenerating, StatusSuccess, StatusFailed}

// Role is the chat message role (Phase 9).
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

// Analysis is one row of trade_analyses (Phase 6).
type Analysis struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	TradeID           uuid.UUID
	PromptHash        string
	Content           *string
	TokensIn          int
	TokensOut         int
	ModelID           string
	Provider          string // "stub" | "openai" | "anthropic"
	Status            string
	Error             *string
	RequestedByUserID uuid.UUID
	RequestedAt       time.Time
	CompletedAt       *time.Time
}

// Recap is one row of ai_recaps (Phase 9 — weekly summary).
type Recap struct {
	ID                uuid.UUID
	TenantID          uuid.UUID
	AccountID         uuid.UUID
	WeekStart         time.Time // Monday UTC
	WeekEnd           time.Time // Sunday UTC
	PromptHash        string
	Content           *string
	TokensIn          int
	TokensOut         int
	ModelID           string
	Provider          string
	Status            string
	Error             *string
	RequestedByUserID uuid.UUID
	RequestedAt       time.Time
	CompletedAt       *time.Time
}

// ChatThread is one row of ai_chat_threads (Phase 9 — multi-turn Q&A head).
type ChatThread struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	UserID        uuid.UUID
	AccountID     *uuid.UUID // nil = tenant-wide
	Title         string
	CreatedAt     time.Time
	LastMessageAt time.Time
}

// ChatMessage is one row of ai_chat_messages (Phase 9).
type ChatMessage struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	ThreadID        uuid.UUID
	Role            string // "user" | "assistant"
	Content         string
	TokensIn        int
	TokensOut       int
	ModelID         *string
	Provider        *string
	ContextTradeIDs []uuid.UUID
	RequestedAt     time.Time
}

// TradeEmbedding is one row of trade_embeddings (Phase 9 — RAG index).
type TradeEmbedding struct {
	ID          uuid.UUID
	TenantID    uuid.UUID
	TradeID     uuid.UUID
	Embedding   []float32 // length = embedder.Dimensions()
	ContextText string
	ModelID     string
	EmbeddedAt  time.Time
}

// DailyUsage is one row of ai_usage_daily + the budget ceiling for the plan.
type DailyUsage struct {
	Day             time.Time
	Requests        int    // per-trade analyses
	RecapRequests   int    // weekly recaps
	ChatRequests    int    // Q&A chat messages
	TokensIn        int64
	TokensOut       int64
	EmbeddingTokens int64
	DailyBudget     int
	HardDailyCap    int
}

// TotalRequests returns the sum across all AI surfaces (per-trade + recap + chat).
func (u DailyUsage) TotalRequests() int {
	return u.Requests + u.RecapRequests + u.ChatRequests
}

// Budget ceilings (unchanged from Phase 6 — shared by all surfaces).
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

// BudgetHeadroom returns how many AI kicks are still allowed today
// (0 if exhausted). Counts all surfaces (per-trade + recap + chat).
func BudgetHeadroom(u DailyUsage) int {
	budget := u.DailyBudget
	if budget > HardDailyCap {
		budget = HardDailyCap
	}
	remain := budget - u.TotalRequests()
	if remain < 0 {
		return 0
	}
	return remain
}