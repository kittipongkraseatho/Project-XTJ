package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"
)

// OpenAIProvider calls the OpenAI Chat Completions API directly via JSON
// over HTTPS — no third-party SDK to keep the binary lean.
type OpenAIProvider struct {
	apiKey  string
	model   string
	http    *http.Client
	log     *slog.Logger
}

// NewOpenAIProvider constructs an OpenAI-backed provider.
// apiKey must be non-empty; returns ErrNoAPIKey otherwise.
func NewOpenAIProvider(apiKey, model string, log *slog.Logger) (*OpenAIProvider, error) {
	if apiKey == "" {
		return nil, ErrNoAPIKey
	}
	if model == "" {
		model = "gpt-4o-mini"
	}
	if log == nil {
		log = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	return &OpenAIProvider{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{Timeout: 30 * time.Second},
		log:    log,
	}, nil
}

// Name implements Provider.
func (o *OpenAIProvider) Name() string { return "openai" }

// Generate implements Provider. Calls
//
//	POST https://api.openai.com/v1/chat/completions
//
//	body: { model, messages: [{role:system, content}, {role:user, content}],
//	        max_tokens, temperature }
//
//	response.choices[0].message.content   → Text
//	response.usage.{prompt_tokens, completion_tokens} → TokensIn/Out
func (o *OpenAIProvider) Generate(ctx context.Context, in GenerateInput) (*GenerateResult, error) {
	start := time.Now()
	reqBody := map[string]any{
		"model":       o.model,
		"messages":    []map[string]string{{"role": "system", "content": in.SystemPrompt}, {"role": "user", "content": in.UserPrompt}},
		"max_tokens":  in.MaxTokens,
		"temperature": 0.4,
	}
	bs, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal openai request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.openai.com/v1/chat/completions", bytes.NewReader(bs))
	if err != nil {
		return nil, fmt.Errorf("build openai request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := o.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai http: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("openai status %d: %s", res.StatusCode, string(body))
	}

	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse openai response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return nil, errors.New("openai returned no choices")
	}
	modelID := parsed.Model
	if modelID == "" {
		modelID = o.model
	}
	return &GenerateResult{
		Text:      parsed.Choices[0].Message.Content,
		TokensIn:  parsed.Usage.PromptTokens,
		TokensOut: parsed.Usage.CompletionTokens,
		ModelID:   modelID,
		LatencyMs: int(time.Since(start).Milliseconds()),
	}, nil
}