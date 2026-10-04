-- 0008_phase6.up.sql
-- Phase 6 — AI Analysis (per-trade commentary)
--
-- 1. trade_analyses    — one row per analysis request; idempotent on
--                         (trade_id, prompt_hash) so re-clicking "Analyze"
--                         returns the same row instead of calling the LLM twice.
-- 2. ai_usage_daily    — per-tenant per-day counter for budget enforcement.

BEGIN;

CREATE TABLE IF NOT EXISTS trade_analyses (
    id                      UUID PRIMARY KEY,
    tenant_id               UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    trade_id                UUID NOT NULL REFERENCES trades(id) ON DELETE CASCADE,
    prompt_hash             TEXT NOT NULL,
    content                 TEXT,
    tokens_in               INT NOT NULL DEFAULT 0,
    tokens_out              INT NOT NULL DEFAULT 0,
    model_id                TEXT NOT NULL,
    provider                TEXT NOT NULL,
    status                  TEXT NOT NULL DEFAULT 'pending',
    error                   TEXT,
    requested_by_user_id    UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    requested_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at            TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS trade_analyses_tenant_trade_idx
    ON trade_analyses (tenant_id, trade_id, requested_at DESC);

CREATE INDEX IF NOT EXISTS trade_analyses_pending_idx
    ON trade_analyses (status, requested_at)
    WHERE status IN ('pending', 'generating');

CREATE TABLE IF NOT EXISTS ai_usage_daily (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    day             DATE NOT NULL,
    requests        INT NOT NULL DEFAULT 0,
    tokens_in       BIGINT NOT NULL DEFAULT 0,
    tokens_out      BIGINT NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX IF NOT EXISTS ai_usage_daily_tenant_day_idx
    ON ai_usage_daily (tenant_id, day);

COMMIT;