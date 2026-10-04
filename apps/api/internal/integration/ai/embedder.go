// Package ai — embedding provider abstraction (Phase 9 RAG).
//
// Embedder is the contract for converting text into fixed-dim vectors for
// semantic search over trade history. Two implementations ship:
//   - StubEmbedder: deterministic FNV-1a hash → 1536-dim sparse-ish vector
//     (zero deps; used when OPENAI_API_KEY is empty or EMBED_PROVIDER=stub).
//   - OpenAIEmbedder: text-embedding-3-small via stdlib net/http (no SDK).
//
// Router picks one via EMBED_PROVIDER env (default stub).
package ai

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
)

// Embedder is the LLM embedding contract used by RAG pipelines.
type Embedder interface {
	// Name returns the canonical embedder name ("stub" | "openai").
	Name() string

	// Dimensions returns the output vector size (1536 for OpenAI).
	Dimensions() int

	// Embed converts a batch of texts into vectors of length Dimensions().
	// All vectors are L2-normalized so cosine distance is equivalent to
	// dot product (cheap for pgvector's `<=>` operator).
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// ErrEmbedderNotConfigured is returned when a real embedder is constructed
// without its required configuration (e.g. missing OPENAI_API_KEY).
var ErrEmbedderNotConfigured = errors.New("ai: embedder not configured")

// ErrEmbedderInputEmpty is returned when callers pass an empty batch.
var ErrEmbedderInputEmpty = errors.New("ai: embedder input empty")

// EmbedderFactory is the signature used by the router to build an embedder
// from env config.
type EmbedderFactory func() (Embedder, error)

// ── StubEmbedder ──────────────────────────────────────────────────────

// StubEmbedder produces deterministic 1536-dim vectors from FNV-1a hashes
// of the input tokens. Two semantically unrelated texts → orthogonal-ish
// vectors; same text → identical vector. Quality is poor (no real semantics)
// but it lets the RAG pipeline run end-to-end with zero external deps.
type StubEmbedder struct{}

// NewStubEmbedder builds a stub embedder.
func NewStubEmbedder() *StubEmbedder { return &StubEmbedder{} }

// Name implements Embedder.
func (s *StubEmbedder) Name() string { return "stub" }

// Dimensions implements Embedder. Fixed at 1536 to match OpenAIEmbedder.
func (s *StubEmbedder) Dimensions() int { return 1536 }

// Embed implements Embedder. For each text:
//   1. Tokenize on whitespace
//   2. For each token: FNV-1a 32-bit hash → map to a dimension → +1/-1
//   3. L2-normalize the resulting vector
//
// Same input → identical vector (deterministic). Different inputs → different
// vectors but not semantically meaningful — that's the cost of zero-config.
func (s *StubEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, ErrEmbedderInputEmpty
	}
	const dim = 1536
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, dim)
		for pos, tok := range tokenize(t) {
			h := fnv.New32a()
			h.Write([]byte(tok))
			sum := h.Sum32()
			// Mix position into the hash so token order matters.
			idx := int(sum+uint32(pos)*2654435761) % dim
			if idx < 0 {
				idx += dim
			}
			sign := float32(1)
			if sum&(1<<31) != 0 {
				sign = -1
			}
			v[idx] += sign
		}
		// L2-normalize so cosine distance = 1 - dot product.
		var norm float32
		for _, x := range v {
			norm += x * x
		}
		if norm > 0 {
			inv := 1 / sqrt32(norm)
			for k := range v {
				v[k] *= inv
			}
		}
		out[i] = v
	}
	return out, nil
}

// ── tokenize ──────────────────────────────────────────────────────────

// tokenize splits text on whitespace and lowercases. Good enough for stub.
func tokenize(s string) []string {
	var out []string
	cur := 0
	for i, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '.' || r == ',' || r == ':' || r == ';' {
			if cur < i {
				out = append(out, lower(s[cur:i]))
			}
			cur = i + 1
		}
	}
	if cur < len(s) {
		out = append(out, lower(s[cur:]))
	}
	return out
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// sqrt32 computes √x via Newton's method. Avoids importing math just for Sqrt.
func sqrt32(x float32) float32 {
	if x <= 0 {
		return 0
	}
	z := x / 2
	for i := 0; i < 8; i++ {
		z -= (z*z - x) / (2 * z)
	}
	return z
}

// ── Format helpers ────────────────────────────────────────────────────

// FormatVec converts a vector to the pgvector literal `{0.1,0.2,...}` form.
func FormatVec(v []float32) string {
	if len(v) == 0 {
		return "{}"
	}
	// Build without fmt to avoid allocation overhead for large vectors.
	b := make([]byte, 0, len(v)*8)
	b = append(b, '{')
	for i, x := range v {
		if i > 0 {
			b = append(b, ',')
		}
		b = appendFloat(b, x)
	}
	b = append(b, '}')
	return string(b)
}

func appendFloat(b []byte, x float32) []byte {
	// Use fmt for clarity — small fraction of total CPU at call sites.
	return append(b, []byte(fmt.Sprintf("%g", x))...)
}