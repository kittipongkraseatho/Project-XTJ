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
	"time"
)

// OpenAIEmbedder calls the OpenAI Embeddings API directly via JSON over HTTPS.
// No third-party SDK to keep the binary lean (consistent with Phase 6
// OpenAIProvider and Phase 8 MetaApiAdapter).
type OpenAIEmbedder struct {
	apiKey string
	model  string
	http   *http.Client
	log    *slog.Logger
}

// NewOpenAIEmbedder constructs an OpenAI-backed embedder. apiKey and model
// must both be non-empty; returns ErrEmbedderNotConfigured otherwise.
func NewOpenAIEmbedder(apiKey, model string, log *slog.Logger) (*OpenAIEmbedder, error) {
	if apiKey == "" || model == "" {
		return nil, ErrEmbedderNotConfigured
	}
	if log == nil {
		log = slog.Default()
	}
	return &OpenAIEmbedder{
		apiKey: apiKey,
		model:  model,
		http:   &http.Client{Timeout: 30 * time.Second},
		log:    log,
	}, nil
}

// Name implements Embedder.
func (o *OpenAIEmbedder) Name() string { return "openai" }

// Dimensions implements Embedder. text-embedding-3-small = 1536.
func (o *OpenAIEmbedder) Dimensions() int { return 1536 }

// Embed implements Embedder. Batched (one HTTP call per batch). OpenAI's
// `/v1/embeddings` endpoint accepts an `input` array and returns one vector
// per element in the same order.
func (o *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, ErrEmbedderInputEmpty
	}

	reqBody := map[string]any{
		"model": o.model,
		"input": texts,
	}
	bs, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("embed: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/embeddings", bytes.NewReader(bs))
	if err != nil {
		return nil, fmt.Errorf("embed: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := o.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed: do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embed: read body: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, ErrEmbedderNotConfigured
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("embed: upstream %d: %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
		Model string `json:"model"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("embed: decode response: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("embed: response has %d vectors, expected %d", len(parsed.Data), len(texts))
	}

	out := make([][]float32, len(texts))
	for _, d := range parsed.Data {
		out[d.Index] = d.Embedding
	}
	for i, v := range out {
		if len(v) != o.Dimensions() {
			return nil, fmt.Errorf("embed: vector %d has %d dims, expected %d", i, len(v), o.Dimensions())
		}
	}
	return out, nil
}

// Ensure interface is satisfied at compile time.
var _ Embedder = (*OpenAIEmbedder)(nil)

// Sentinel for tests + router wiring.
var ErrEmbedUpstream = errors.New("ai: embed upstream error")