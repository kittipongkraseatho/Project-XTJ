-- name: CreateOAuthState :exec
INSERT INTO oauth_states (state_token, flow, provider, redirect_uri, code_verifier, ip, user_agent, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: GetOAuthState :one
SELECT state_token, flow, provider, redirect_uri, code_verifier, ip, user_agent, created_at, expires_at, consumed_at
FROM oauth_states
WHERE state_token = $1;

-- name: ConsumeOAuthState :exec
UPDATE oauth_states SET consumed_at = NOW() WHERE state_token = $1;

-- name: DeleteExpiredOAuthStates :exec
DELETE FROM oauth_states WHERE expires_at < NOW() - INTERVAL '1 hour';