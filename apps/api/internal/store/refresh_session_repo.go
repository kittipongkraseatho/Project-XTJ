package store

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"github.com/gofrs/uuid"
	"github.com/jackc/pgx/v5"
)

// RefreshSession is the DB shape. We don't have a domain package for it
// since refresh sessions are an auth-internal concern; the service layer
// hides the type from handlers.
type RefreshSession struct {
	ID                  uuid.UUID
	UserID              uuid.UUID
	TenantID            uuid.UUID
	TokenHash           string
	Status              string
	UserAgent           *string
	IP                  *netip.Addr
	CreatedAt           time.Time
	LastUsedAt          time.Time
	ExpiresAt           time.Time
	RevokedAt           *time.Time
	ReplacedBySessionID *uuid.UUID
}

type RefreshSessionRepo struct{ pool DBTX }

func (r *RefreshSessionRepo) WithTx(tx DBTX) *RefreshSessionRepo { return &RefreshSessionRepo{pool: tx} }

func (r *RefreshSessionRepo) Create(ctx context.Context, s *RefreshSession) error {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO refresh_sessions (id, user_id, tenant_id, token_hash, status, user_agent, ip, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING created_at, last_used_at`,
		s.ID, s.UserID, s.TenantID, s.TokenHash, s.Status, s.UserAgent, s.IP, s.ExpiresAt)
	return row.Scan(&s.CreatedAt, &s.LastUsedAt)
}

func (r *RefreshSessionRepo) GetByHash(ctx context.Context, hash string) (*RefreshSession, error) {
	row := r.pool.QueryRow(ctx, refreshSessionSelect+` WHERE token_hash = $1`, hash)
	return scanRefreshSession(row)
}

// ListActiveForUser returns all non-expired active sessions for a user.
// Used by reuse-detection to revoke ALL active sessions.
func (r *RefreshSessionRepo) ListActiveForUser(ctx context.Context, userID uuid.UUID) ([]*RefreshSession, error) {
	rows, err := r.pool.Query(ctx, refreshSessionSelect+`
		WHERE user_id = $1 AND status = 'active' AND expires_at > NOW()`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*RefreshSession
	for rows.Next() {
		s, err := scanRefreshSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (r *RefreshSessionRepo) Revoke(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE refresh_sessions SET status = 'revoked', revoked_at = NOW() WHERE id = $1`, id)
	return err
}

// RevokeAllForUser revokes every active session for a user.
// Returns the number of rows affected.
func (r *RefreshSessionRepo) RevokeAllForUser(ctx context.Context, userID uuid.UUID) (int64, error) {
	tag, err := r.pool.Exec(ctx, `
		UPDATE refresh_sessions SET status = 'revoked', revoked_at = NOW()
		WHERE user_id = $1 AND status = 'active'`, userID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// Rotate marks `oldID` as revoked and links it to the new session id.
func (r *RefreshSessionRepo) Rotate(ctx context.Context, oldID, newID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE refresh_sessions
		SET status = 'revoked', revoked_at = NOW(), replaced_by_session_id = $2
		WHERE id = $1`, oldID, newID)
	return err
}

func (r *RefreshSessionRepo) Touch(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `UPDATE refresh_sessions SET last_used_at = NOW() WHERE id = $1`, id)
	return err
}

const refreshSessionSelect = `
SELECT id, user_id, tenant_id, token_hash, status, user_agent, ip,
       created_at, last_used_at, expires_at, revoked_at, replaced_by_session_id
FROM refresh_sessions`

func scanRefreshSession(row pgx.Row) (*RefreshSession, error) {
	var s RefreshSession
	if err := row.Scan(
		&s.ID, &s.UserID, &s.TenantID, &s.TokenHash, &s.Status, &s.UserAgent, &s.IP,
		&s.CreatedAt, &s.LastUsedAt, &s.ExpiresAt, &s.RevokedAt, &s.ReplacedBySessionID,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}