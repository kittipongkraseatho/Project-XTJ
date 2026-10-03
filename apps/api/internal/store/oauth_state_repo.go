package store

import (
	"context"
	"time"
)

// OAuthState is the transient state record for an OAuth round-trip.
type OAuthState struct {
	StateToken   string
	Flow         string
	Provider     IdentityProvider
	RedirectURI  *string
	CodeVerifier *string
	IP           *string
	UserAgent    *string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	ConsumedAt   *time.Time
}

type OAuthStateRepo struct{ pool DBTX }

func (r *OAuthStateRepo) WithTx(tx DBTX) *OAuthStateRepo { return &OAuthStateRepo{pool: tx} }

func (r *OAuthStateRepo) Create(ctx context.Context, s *OAuthState) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO oauth_states (state_token, flow, provider, redirect_uri, code_verifier, ip, user_agent, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		s.StateToken, s.Flow, s.Provider, s.RedirectURI, s.CodeVerifier, s.IP, s.UserAgent, s.ExpiresAt)
	return err
}

func (r *OAuthStateRepo) GetByToken(ctx context.Context, token string) (*OAuthState, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT state_token, flow, provider, redirect_uri, code_verifier, ip, user_agent, created_at, expires_at, consumed_at
		FROM oauth_states WHERE state_token = $1`, token)
	var s OAuthState
	if err := row.Scan(&s.StateToken, &s.Flow, &s.Provider, &s.RedirectURI, &s.CodeVerifier, &s.IP, &s.UserAgent, &s.CreatedAt, &s.ExpiresAt, &s.ConsumedAt); err != nil {
		return nil, mapErr(err)
	}
	return &s, nil
}

func (r *OAuthStateRepo) Consume(ctx context.Context, token string) error {
	_, err := r.pool.Exec(ctx, `UPDATE oauth_states SET consumed_at = NOW() WHERE state_token = $1`, token)
	return err
}

// mapErr reuses store.ErrNotFound for pgx.ErrNoRows.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	return err
}