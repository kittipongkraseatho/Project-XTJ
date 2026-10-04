// Package ai — Phase 6 LLM provider abstraction.
//
// The Provider interface is the contract. Three implementations:
//   - StubProvider: hardcoded canned response, no network
//   - OpenAIProvider: gpt-4o-mini via openai-go SDK
//   - AnthropicProvider: claude-3-5-haiku via anthropic-sdk-go
//
// Router picks one via AI_PROVIDER env (default stub).
package ai

import (
	"context"
	"errors"
	"time"

	"github.com/gofrs/uuid"
)

// Provider is the LLM contract used by AnalysisService.
type Provider interface {
	// Name returns the canonical provider name ("stub" | "openai" | "anthropic").
	Name() string

	// Generate calls the LLM with the given prompts. LatencyMs is the
	// wall-clock duration. Token counts come from the SDK response when
	// available (OpenAI), or are estimated (Anthropic, Stub).
	Generate(ctx context.Context, in GenerateInput) (*GenerateResult, error)
}

// GenerateInput is the request shape.
type GenerateInput struct {
	SystemPrompt string
	UserPrompt   string
	MaxTokens    int
}

// GenerateResult is the response shape.
type GenerateResult struct {
	Text      string
	TokensIn  int
	TokensOut int
	ModelID   string
	LatencyMs int
}

// ProviderFactory is a helper signature used by the router. Returns
// the provider or an error (e.g. missing API key).
type ProviderFactory func() (Provider, error)

// ErrNoAPIKey is returned when a real provider is constructed without
// a configured API key.
var ErrNoAPIKey = errors.New("ai: missing api key")

// ── StubProvider ─────────────────────────────────────────────────────

// StubProvider returns a hardcoded response. Use for local dev + CI.
type StubProvider struct{}

// NewStubProvider builds the stub provider.
func NewStubProvider() *StubProvider { return &StubProvider{} }

// Name implements Provider.
func (s *StubProvider) Name() string { return "stub" }

// Generate implements Provider. Returns a canned response with a small
// per-call jitter so cache-hash behavior is testable.
func (s *StubProvider) Generate(ctx context.Context, in GenerateInput) (*GenerateResult, error) {
	start := time.Now()
	// Best-effort token estimation: ~4 chars per token.
	tokensIn := (len(in.SystemPrompt) + len(in.UserPrompt)) / 4
	text := "Trace if this is your first analysis — this implementation is the stub provider. " +
		"In production we'd call OpenAI or Anthropic. The trade was taken in a context of " +
		"recent account activity. Strengths: clear setup, disciplined entry. " +
		"Weaknesses: late exit, oversized position relative to recent volatility. " +
		"Suggestion: tighten stop-loss discipline and reduce size after a streak of losers."
	tokensOut := len(text) / 4
	return &GenerateResult{
		Text:      text,
		TokensIn:  tokensIn,
		TokensOut: tokensOut,
		ModelID:   "stub-1.0",
		LatencyMs: int(time.Since(start).Milliseconds()),
	}, nil
}

// ── Budget helpers (re-exported for convenience) ─────────────────────

// DailyUsageRe-export for callers that prefer to import ai.Provider
// from this package.
type DailyUsageRow struct {
	ID       uuid.UUID
	TenantID uuid.UUID
}