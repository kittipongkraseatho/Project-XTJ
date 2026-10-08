-- 0011_phase10.up.sql
-- Phase 10 — Public API Write Scopes (idempotency cache).
--
-- New table api_key_idempotency stores (api_key_id, idempotency_key) →
-- (request_hash, response_status, response_body, expires_at). The same UUID
-- replayed with the same body returns the cached response within 24h; replayed
-- with a different body returns 409 IDEMPOTENCY_KEY_CONFLICT.
--
-- A background sweep job (IdempotencySweepJob, registered in §5) deletes
-- rows past expires_at every hour. No application code path depends on
-- the sweep — the middleware's expires_at filter is the correctness boundary.

BEGIN;

CREATE TABLE IF NOT EXISTS api_key_idempotency (
    id                UUID PRIMARY KEY,
    api_key_id        UUID NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    idempotency_key   TEXT NOT NULL,
    request_hash      TEXT NOT NULL,        -- SHA256 of normalized request body
    response_status   INT  NOT NULL,
    response_body     JSONB NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at        TIMESTAMPTZ NOT NULL  -- now() + 24h
);

CREATE UNIQUE INDEX IF NOT EXISTS api_key_idempotency_key_idx
    ON api_key_idempotency (api_key_id, idempotency_key);

CREATE INDEX IF NOT EXISTS api_key_idempotency_expires_idx
    ON api_key_idempotency (expires_at);

COMMIT;