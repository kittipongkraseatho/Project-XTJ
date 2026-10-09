package ai

import (
	"context"
	"log/slog"
	"time"

	"github.com/gofrs/uuid"
)

// ChatRAGRetriever is the production Retriever used by ChatService.
//
// Behavior:
//   - Embeds the user message with the embedder.
//   - If embedder is *not* the StubEmbedder → uses SearchSimilar (pgvector cosine).
//   - Otherwise (stub mode): falls back to "last 10 trades for the account"
//     (cheap SQL filter; quality low but zero external deps).
//
// The fallback is detected by the embedder's `Name()` returning "stub". If a
// future embedder also degrades quality, we can swap to a different signal.
type ChatRAGRetriever struct {
	embedder   Embedder
	embedding  EmbeddingSearcher
	recentList RecentTradeLister
	topK       int
	log        *slog.Logger
}

// EmbeddingSearcher is the contract for cosine search (delegated to Store
// via TradeEmbeddingRepo.SearchSimilar).
type EmbeddingSearcher interface {
	SearchSimilar(ctx context.Context, tenantID uuid.UUID, queryVec []float32, accountID *uuid.UUID, limit int) ([]SimilarTradeID, error)
}

// RecentTradeLister is the fallback for stub-mode retrieval.
type RecentTradeLister interface {
	ListRecentByAccount(ctx context.Context, tenantID, accountID uuid.UUID, limit int) ([]uuid.UUID, error)
}

// SimilarTradeID is re-exported here so callers don't have to import store.
type SimilarTradeID struct {
	TradeID  uuid.UUID
	Distance float32
}

// NewChatRAGRetriever builds the production retriever.
func NewChatRAGRetriever(emb Embedder, searcher EmbeddingSearcher, lister RecentTradeLister, topK int, log *slog.Logger) *ChatRAGRetriever {
	if topK <= 0 {
		topK = ChatRAGTopK
	}
	if log == nil {
		log = slog.Default()
	}
	return &ChatRAGRetriever{
		embedder:   emb,
		embedding:  searcher,
		recentList: lister,
		topK:       topK,
		log:        log,
	}
}

// Retrieve implements ChatRetriever.
func (r *ChatRAGRetriever) Retrieve(ctx context.Context, tenantID uuid.UUID, accountID *uuid.UUID, userMessage string, topK int) ([]uuid.UUID, error) {
	if topK <= 0 {
		topK = r.topK
	}

	// Stub mode → fallback to recent trades (cheap, deterministic).
	if r.embedder.Name() == "stub" {
		if r.recentList == nil {
			return nil, nil
		}
		// Stub fallback only works for account-scoped threads.
		if accountID == nil {
			return nil, nil
		}
		return r.recentList.ListRecentByAccount(ctx, tenantID, *accountID, topK)
	}

	// Real embedding path.
	vecs, err := r.embedder.Embed(ctx, []string{userMessage})
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 {
		return nil, nil
	}
	// pgvector query is bounded; add a short timeout to keep chat replies snappy.
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	hits, err := r.embedding.SearchSimilar(cctx, tenantID, vecs[0], accountID, topK)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, len(hits))
	for i, h := range hits {
		out[i] = h.TradeID
	}
	return out, nil
}
