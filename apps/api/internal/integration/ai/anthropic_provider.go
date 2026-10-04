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

// AnthropicProvider calls the Anthropic Messages API directly — no SDK.
type AnthropicProvider struct {
	apiKey  string
	model   string
	http    *http.Client
	log     *slog.Logger
}

// NewAnthropicProvider constructs an Anthropic-backed provider.
func NewAnthropicProvider(apiKey, model string, log *slog.Logger) (*AnthropicProvider, error) {
	if apiKey == "" {
		return nil, ErrNoAPIKey
	}
	if model == "" {
		model = "claude-3-5-haiku-latest"
	}
	if log == nil {
		log = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	return &AnthropicProvider{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{Timeout: 30 * time.Second},
		log:    log,
	}, nil
}

// Name implements Provider.
func (a *AnthropicProvider) Name() string { return "anthropic" }

// Generate calls POST https://api.anthropic.com/v1/messages.
//
//	body: { model, max_tokens, system, messages: [{role:user, content}] }
//
//	header: x-api-key, anthropic-version: 2023-06-01
//
//	response.content[0].text  → Text
//	response.usage.{input_tokens, output_tokens}  → TokensIn/Out
func (a *AnthropicProvider) Generate(ctx context.Context, in GenerateInput) (*GenerateResult, error) {
	start := time.Now()
	reqBody := map[string]any{
		"model":      a.model,
		"max_tokens": in.MaxTokens,
		"system":     in.SystemPrompt,
		"messages": []map[string]string{
			{"role": "user", "content": in.UserPrompt},
		},
	}
	bs, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(bs))
	if err != nil {
		return nil, fmt.Errorf("build anthropic request: %w", err)
	}
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	res, err := a.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic http: %w", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("anthropic status %d: %s", res.StatusCode, string(body))
	}

	var parsed struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse anthropic response: %w", err)
	}
	if len(parsed.Content) == 0 {
		return nil, errors.New("anthropic returned no content")
	}
	modelID := parsed.Model
	if modelID == "" {
		modelID = a.model
	}
	return &GenerateResult{
		Text:      parsed.Content[0].Text,
		TokensIn:  parsed.Usage.InputTokens,
		TokensOut: parsed.Usage.OutputTokens,
		ModelID:   modelID,
		LatencyMs: int(time.Since(start).Milliseconds()),
	}, nil
}