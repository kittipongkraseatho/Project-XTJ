package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/trading-journal/api/internal/domain/ai"
)

// ── RecapRepo ────────────────────────────────────────────────────────

// RecapRepo manages ai_recaps rows (Phase 9 — weekly summary).
type RecapRepo struct {
	pool DBTX
}

func (r *RecapRepo) WithTx(tx DBTX) *RecapRepo {
	return &RecapRepo{pool: tx}
}

// Create inserts a new recap row (status=pending).
func (r *RecapRepo) Create(ctx context.Context, x *ai.Recap) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ai_recaps
			(id, tenant_id, account_id, week_start, week_end, prompt_hash, content,
			 tokens_in, tokens_out, model_id, provider, status,
			 requested_by_user_id, requested_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		x.ID, x.TenantID, x.AccountID, x.WeekStart, x.WeekEnd, x.PromptHash, x.Content,
		x.TokensIn, x.TokensOut, x.ModelID, x.Provider, x.Status,
		x.RequestedByUserID, x.RequestedAt)
	return err
}

// FindRecentWithHash returns the most recent success recap with the same hash
// within sinceMinutes. Used for idempotent re-clicks.
func (r *RecapRepo) FindRecentWithHash(ctx context.Context, tenantID, accountID uuid.UUID, weekStart time.Time, hash string, sinceMinutes int) (*ai.Recap, error) {
	since := time.Now().UTC().Add(-time.Duration(sinceMinutes) * time.Minute)
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, account_id, week_start, week_end, prompt_hash, content,
		       tokens_in, tokens_out, model_id, provider, status, error,
		       requested_by_user_id, requested_at, completed_at
		FROM ai_recaps
		WHERE tenant_id = $1 AND account_id = $2 AND week_start = $3
		  AND prompt_hash = $4 AND status = 'success' AND completed_at >= $5
		ORDER BY completed_at DESC LIMIT 1`,
		tenantID, accountID, weekStart, hash, since)
	return scanRecap(row)
}

// GetByID returns the recap row.
func (r *RecapRepo) GetByID(ctx context.Context, id uuid.UUID) (*ai.Recap, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, account_id, week_start, week_end, prompt_hash, content,
		       tokens_in, tokens_out, model_id, provider, status, error,
		       requested_by_user_id, requested_at, completed_at
		FROM ai_recaps WHERE id = $1`, id)
	return scanRecap(row)
}

// ListByAccount returns the latest recaps for an account (newest first).
func (r *RecapRepo) ListByAccount(ctx context.Context, tenantID, accountID uuid.UUID, limit int) ([]*ai.Recap, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, account_id, week_start, week_end, prompt_hash, content,
		       tokens_in, tokens_out, model_id, provider, status, error,
		       requested_by_user_id, requested_at, completed_at
		FROM ai_recaps
		WHERE tenant_id = $1 AND account_id = $2
		ORDER BY week_start DESC, requested_at DESC
		LIMIT $3`, tenantID, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ai.Recap{}
	for rows.Next() {
		x, err := scanRecap(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// MarkGenerating transitions pending → generating.
func (r *RecapRepo) MarkGenerating(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE ai_recaps SET status = 'generating'
		WHERE id = $1 AND status IN ('pending','generating')`, id)
	return err
}

// MarkSuccess finalizes a successful generation.
func (r *RecapRepo) MarkSuccess(ctx context.Context, id uuid.UUID, content string, tokensIn, tokensOut int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE ai_recaps
		SET status = 'success', content = $2, tokens_in = $3, tokens_out = $4, completed_at = now(), error = NULL
		WHERE id = $1`, id, content, tokensIn, tokensOut)
	return err
}

// MarkFailed records a failure.
func (r *RecapRepo) MarkFailed(ctx context.Context, id uuid.UUID, errMsg string, tokensIn int) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE ai_recaps
		SET status = 'failed', error = $2, tokens_in = $3, completed_at = now()
		WHERE id = $1`, id, errMsg, tokensIn)
	return err
}

func scanRecap(s scanner) (*ai.Recap, error) {
	var x ai.Recap
	var content, errMsg *string
	var completedAt *time.Time
	if err := s.Scan(
		&x.ID, &x.TenantID, &x.AccountID, &x.WeekStart, &x.WeekEnd, &x.PromptHash, &content,
		&x.TokensIn, &x.TokensOut, &x.ModelID, &x.Provider, &x.Status, &errMsg,
		&x.RequestedByUserID, &x.RequestedAt, &completedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	x.Content = content
	x.Error = errMsg
	x.CompletedAt = completedAt
	return &x, nil
}

// ── ChatThreadRepo ───────────────────────────────────────────────────

// ChatThreadRepo manages ai_chat_threads rows (Phase 9 — multi-turn Q&A).
type ChatThreadRepo struct {
	pool DBTX
}

func (r *ChatThreadRepo) WithTx(tx DBTX) *ChatThreadRepo {
	return &ChatThreadRepo{pool: tx}
}

// FindOrCreate returns the existing thread for (user, account) when account
// is set, otherwise creates a new tenant-wide thread.
func (r *ChatThreadRepo) FindOrCreate(ctx context.Context, tenantID, userID uuid.UUID, accountID *uuid.UUID, title string) (*ai.ChatThread, error) {
	if accountID != nil {
		// Partial unique: (tenant, user, account) when account IS NOT NULL.
		var existing ai.ChatThread
		row := r.pool.QueryRow(ctx, `
			SELECT id, tenant_id, user_id, account_id, title, created_at, last_message_at
			FROM ai_chat_threads
			WHERE tenant_id = $1 AND user_id = $2 AND account_id = $3
			LIMIT 1`, tenantID, userID, *accountID)
		var acc *uuid.UUID
		if err := row.Scan(&existing.ID, &existing.TenantID, &existing.UserID, &acc, &existing.Title, &existing.CreatedAt, &existing.LastMessageAt); err == nil {
			existing.AccountID = acc
			return &existing, nil
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	id := uuid.Must(uuid.NewV7())
	now := time.Now().UTC()
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ai_chat_threads (id, tenant_id, user_id, account_id, title, created_at, last_message_at)
		VALUES ($1, $2, $3, $4, $5, $6, $6)`,
		id, tenantID, userID, accountID, title, now)
	if err != nil {
		return nil, err
	}
	return &ai.ChatThread{
		ID: id, TenantID: tenantID, UserID: userID, AccountID: accountID,
		Title: title, CreatedAt: now, LastMessageAt: now,
	}, nil
}

// GetByID returns the thread (returns ErrNotFound if missing or different tenant).
func (r *ChatThreadRepo) GetByID(ctx context.Context, tenantID, id uuid.UUID) (*ai.ChatThread, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, user_id, account_id, title, created_at, last_message_at
		FROM ai_chat_threads
		WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	var x ai.ChatThread
	var acc *uuid.UUID
	if err := row.Scan(&x.ID, &x.TenantID, &x.UserID, &acc, &x.Title, &x.CreatedAt, &x.LastMessageAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	x.AccountID = acc
	return &x, nil
}

// ListByUser returns the user's threads (most recent first, paginated).
func (r *ChatThreadRepo) ListByUser(ctx context.Context, tenantID, userID uuid.UUID, limit int) ([]*ai.ChatThread, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, user_id, account_id, title, created_at, last_message_at
		FROM ai_chat_threads
		WHERE tenant_id = $1 AND user_id = $2
		ORDER BY last_message_at DESC
		LIMIT $3`, tenantID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ai.ChatThread{}
	for rows.Next() {
		var x ai.ChatThread
		var acc *uuid.UUID
		if err := rows.Scan(&x.ID, &x.TenantID, &x.UserID, &acc, &x.Title, &x.CreatedAt, &x.LastMessageAt); err != nil {
			return nil, err
		}
		x.AccountID = acc
		out = append(out, &x)
	}
	return out, rows.Err()
}

// TouchLastMessage bumps last_message_at.
func (r *ChatThreadRepo) TouchLastMessage(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE ai_chat_threads SET last_message_at = now() WHERE id = $1`, id)
	return err
}

// Delete removes the thread (cascades messages).
func (r *ChatThreadRepo) Delete(ctx context.Context, tenantID, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM ai_chat_threads WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	return err
}

// ── ChatMessageRepo ──────────────────────────────────────────────────

// ChatMessageRepo manages ai_chat_messages rows.
type ChatMessageRepo struct {
	pool DBTX
}

func (r *ChatMessageRepo) WithTx(tx DBTX) *ChatMessageRepo {
	return &ChatMessageRepo{pool: tx}
}

// Insert appends a new message.
func (r *ChatMessageRepo) Insert(ctx context.Context, m *ai.ChatMessage) error {
	// pgx needs explicit pgtype for UUID[]; we marshal as a typed slice.
	_, err := r.pool.Exec(ctx, `
		INSERT INTO ai_chat_messages
			(id, tenant_id, thread_id, role, content, tokens_in, tokens_out,
			 model_id, provider, context_trade_ids, requested_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		m.ID, m.TenantID, m.ThreadID, m.Role, m.Content,
		m.TokensIn, m.TokensOut, m.ModelID, m.Provider,
		m.ContextTradeIDs, m.RequestedAt)
	return err
}

// ListByThread returns messages ASC by time. Caller bounds with limit.
func (r *ChatMessageRepo) ListByThread(ctx context.Context, tenantID, threadID uuid.UUID, limit int) ([]*ai.ChatMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, thread_id, role, content, tokens_in, tokens_out,
		       model_id, provider, context_trade_ids, requested_at
		FROM ai_chat_messages
		WHERE tenant_id = $1 AND thread_id = $2
		ORDER BY requested_at ASC
		LIMIT $3`, tenantID, threadID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ai.ChatMessage{}
	for rows.Next() {
		m, err := scanChatMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// TrimToLast50 keeps the last 50 messages per thread (deletes older excess).
func (r *ChatMessageRepo) TrimToLast50(ctx context.Context, tenantID, threadID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM ai_chat_messages
		WHERE tenant_id = $1 AND thread_id = $2
		  AND id IN (
		    SELECT id FROM ai_chat_messages
		    WHERE tenant_id = $1 AND thread_id = $2
		    ORDER BY requested_at DESC
		    OFFSET 50
		  )`, tenantID, threadID)
	return err
}

// GetByID returns a single message.
func (r *ChatMessageRepo) GetByID(ctx context.Context, id uuid.UUID) (*ai.ChatMessage, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, thread_id, role, content, tokens_in, tokens_out,
		       model_id, provider, context_trade_ids, requested_at
		FROM ai_chat_messages WHERE id = $1`)
	return scanChatMessage(row)
}

func scanChatMessage(s scanner) (*ai.ChatMessage, error) {
	var m ai.ChatMessage
	var modelID, provider *string
	if err := s.Scan(
		&m.ID, &m.TenantID, &m.ThreadID, &m.Role, &m.Content,
		&m.TokensIn, &m.TokensOut, &modelID, &provider,
		&m.ContextTradeIDs, &m.RequestedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	m.ModelID = modelID
	m.Provider = provider
	return &m, nil
}

// ── TradeEmbeddingRepo ───────────────────────────────────────────────

// TradeEmbeddingRepo manages trade_embeddings rows (RAG index).
type TradeEmbeddingRepo struct {
	pool DBTX
}

func (r *TradeEmbeddingRepo) WithTx(tx DBTX) *TradeEmbeddingRepo {
	return &TradeEmbeddingRepo{pool: tx}
}

// Upsert writes a new embedding row (replaces if trade already indexed).
func (r *TradeEmbeddingRepo) Upsert(ctx context.Context, e *ai.TradeEmbedding) error {
	id := uuid.Must(uuid.NewV7())
	vecLit := formatVectorLiteral(e.Embedding)
	_, err := r.pool.Exec(ctx, `
		INSERT INTO trade_embeddings
			(id, tenant_id, trade_id, embedding, context_text, model_id, embedded_at)
		VALUES ($1, $2, $3, $4::vector, $5, $6, now())
		ON CONFLICT (tenant_id, trade_id) DO UPDATE
		SET embedding = EXCLUDED.embedding,
		    context_text = EXCLUDED.context_text,
		    model_id = EXCLUDED.model_id,
		    embedded_at = EXCLUDED.embedded_at`,
		id, e.TenantID, e.TradeID, vecLit, e.ContextText, e.ModelID)
	return err
}

// GetByTradeID returns the embedding row for a trade (or ErrNotFound).
func (r *TradeEmbeddingRepo) GetByTradeID(ctx context.Context, tenantID, tradeID uuid.UUID) (*ai.TradeEmbedding, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, trade_id, embedding::text, context_text, model_id, embedded_at
		FROM trade_embeddings
		WHERE tenant_id = $1 AND trade_id = $2`, tenantID, tradeID)
	var e ai.TradeEmbedding
	var vecStr string
	if err := row.Scan(&e.ID, &e.TenantID, &e.TradeID, &vecStr, &e.ContextText, &e.ModelID, &e.EmbeddedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	vec, err := parseVectorLiteral(vecStr)
	if err != nil {
		return nil, fmt.Errorf("parse embedding vector: %w", err)
	}
	e.Embedding = vec
	return &e, nil
}

// SearchSimilar returns up to limit trade IDs most similar to the query vector.
// Uses pgvector cosine distance (`<=>`); 0 = identical, 2 = opposite.
func (r *TradeEmbeddingRepo) SearchSimilar(ctx context.Context, tenantID uuid.UUID, queryVec []float32, accountID *uuid.UUID, limit int) ([]SimilarTradeID, error) {
	if limit <= 0 {
		limit = 10
	}
	vecLit := formatVectorLiteral(queryVec)
	args := []any{tenantID, vecLit}
	extra := ""
	if accountID != nil {
		extra = " AND t.trading_account_id = $3"
		args = append(args, *accountID)
	}
	args = append(args, limit)
	q := fmt.Sprintf(`
		SELECT te.trade_id, te.embedding <=> $2::vector AS distance
		FROM trade_embeddings te
		JOIN trades t ON t.id = te.trade_id
		WHERE te.tenant_id = $1%s
		ORDER BY te.embedding <=> $2::vector
		LIMIT $%d`, extra, len(args))
	rows, err := r.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SimilarTradeID{}
	for rows.Next() {
		var s SimilarTradeID
		if err := rows.Scan(&s.TradeID, &s.Distance); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SimilarTradeID is one row of a similarity search.
type SimilarTradeID struct {
	TradeID  uuid.UUID
	Distance float32
}

// formatVectorLiteral writes a float slice in [0.1,0.2,...] form for pgvector.
func formatVectorLiteral(v []float32) string {
	if len(v) == 0 {
		return "[]"
	}
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = fmt.Sprintf("%g", x)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// parseVectorLiteral parses pgvector's [a,b,c] text form back to []float32.
func parseVectorLiteral(s string) ([]float32, error) {
	s = strings.TrimSpace(s)
	if len(s) < 2 || s[0] != '[' || s[len(s)-1] != ']' {
		return nil, fmt.Errorf("invalid vector literal: %q", s)
	}
	body := s[1 : len(s)-1]
	if body == "" {
		return []float32{}, nil
	}
	parts := strings.Split(body, ",")
	out := make([]float32, len(parts))
	for i, p := range parts {
		var x float32
		if _, err := fmt.Sscanf(strings.TrimSpace(p), "%g", &x); err != nil {
			return nil, fmt.Errorf("parse vec[%d]=%q: %w", i, p, err)
		}
		out[i] = x
	}
	return out, nil
}