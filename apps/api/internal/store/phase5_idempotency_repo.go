package store

import (
	"context"
	"errors"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"
)

// ── IdempotencyRepo (Phase 10) ────────────────────────────────────────

// IdempotencyRepo manages the api_key_idempotency table — the (api_key_id,
// idempotency_key) → (request_hash, response_status, response_body) cache
// for Phase 10's public write endpoints.
type IdempotencyRepo struct{ pool DBTX }

func (r *IdempotencyRepo) WithTx(tx DBTX) *IdempotencyRepo {
	return &IdempotencyRepo{pool: tx}
}

// CachedResponse is the replay payload for a previously-seen request.
type CachedResponse struct {
	RequestHash    string
	ResponseStatus int
	ResponseBody   []byte
	ExpiresAt      time.Time
}

// Lookup returns the cached response for (apiKeyID, key) or (nil, nil) on miss.
func (r *IdempotencyRepo) Lookup(ctx context.Context, apiKeyID uuid.UUID, key string) (*CachedResponse, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT request_hash, response_status, response_body, expires_at
		FROM api_key_idempotency
		WHERE api_key_id = $1 AND idempotency_key = $2 AND expires_at > now()`,
		apiKeyID, key)
	var c CachedResponse
	if err := row.Scan(&c.RequestHash, &c.ResponseStatus, &c.ResponseBody, &c.ExpiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// Store persists the cache row. Idempotent: ON CONFLICT update the existing
// row's response (used when a handler retries a previously-failed request).
func (r *IdempotencyRepo) Store(ctx context.Context, apiKeyID uuid.UUID, key, requestHash string, status int, body []byte, ttl time.Duration) error {
	id := uuid.Must(uuid.NewV7())
	_, err := r.pool.Exec(ctx, `
		INSERT INTO api_key_idempotency
			(id, api_key_id, idempotency_key, request_hash, response_status, response_body, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, now() + $7::interval)
		ON CONFLICT (api_key_id, idempotency_key) DO UPDATE
		SET request_hash    = EXCLUDED.request_hash,
		    response_status = EXCLUDED.response_status,
		    response_body   = EXCLUDED.response_body,
		    expires_at      = EXCLUDED.expires_at`,
		id, apiKeyID, key, requestHash, status, body, ttl.String())
	return err
}

// SweepExpired deletes rows past their expires_at. Called hourly by
// IdempotencySweepJob (Phase 10 §5).
func (r *IdempotencyRepo) SweepExpired(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM api_key_idempotency WHERE expires_at < now()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}