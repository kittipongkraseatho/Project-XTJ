-- 0010_phase9.up.sql
-- Phase 9 — AI Expansion (weekly recap + Q&A chat with RAG).
--
-- 1. pgvector extension       — needed for `vector(1536)` column + cosine RAG.
-- 2. trade_embeddings         — one row per trade; embedding(text) for RAG
--                                retrieval. IVFFlat index built separately
--                                (see 0011_pgvector_index.up.sql, run with
--                                CONCURRENTLY outside this migration tx).
-- 3. ai_recaps                — one row per recap kick-off (single-account,
--                                weekly). Mirrors trade_analyses shape.
-- 4. ai_chat_threads          — multi-turn Q&A thread head row.
-- 5. ai_chat_messages         — per-message log; trimmed to last 50 in app.
-- 6. ai_usage_daily           — 3 new columns: recap_requests, chat_requests,
--                                embedding_tokens. Shared budget with Phase 6's
--                                per-trade analyses.

BEGIN;

-- 1. pgvector extension (idempotent — DB superuser may not have CREATE EXTENSION
-- on managed Postgres; CI testcontainers uses pgvector/pgvector image so this works.
-- In production, ops should pre-create: CREATE EXTENSION IF NOT EXISTS vector;).
CREATE EXTENSION IF NOT EXISTS vector;

-- 2. trade_embeddings — RAG index (one embedding per trade).
CREATE TABLE IF NOT EXISTS trade_embeddings (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    trade_id        UUID NOT NULL REFERENCES trades(id) ON DELETE CASCADE,
    embedding       vector(1536) NOT NULL,
    context_text    TEXT NOT NULL,
    model_id        TEXT NOT NULL,
    embedded_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS trade_embeddings_tenant_trade_idx
    ON trade_embeddings (tenant_id, trade_id);

CREATE INDEX IF NOT EXISTS trade_embeddings_embedded_at_idx
    ON trade_embeddings (tenant_id, embedded_at DESC);

-- 3. ai_recaps — single-account weekly recap requests.
CREATE TABLE IF NOT EXISTS ai_recaps (
    id                      UUID PRIMARY KEY,
    tenant_id               UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    account_id              UUID NOT NULL REFERENCES trading_accounts(id) ON DELETE CASCADE,
    week_start              DATE NOT NULL,
    week_end                DATE NOT NULL,
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

CREATE INDEX IF NOT EXISTS ai_recaps_tenant_account_week_idx
    ON ai_recaps (tenant_id, account_id, week_start DESC);

CREATE INDEX IF NOT EXISTS ai_recaps_pending_idx
    ON ai_recaps (status, requested_at)
    WHERE status IN ('pending', 'generating');

-- 4. ai_chat_threads — Q&A thread header.
CREATE TABLE IF NOT EXISTS ai_chat_threads (
    id              UUID PRIMARY KEY,
    tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    account_id      UUID REFERENCES trading_accounts(id) ON DELETE CASCADE,
    title           TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_message_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ai_chat_threads_tenant_user_recent_idx
    ON ai_chat_threads (tenant_id, user_id, last_message_at DESC);

-- One default thread per (user, account) when account_id is set; NULL account_id
-- allows multi-thread across all accounts.
CREATE UNIQUE INDEX IF NOT EXISTS ai_chat_threads_default_per_account_idx
    ON ai_chat_threads (tenant_id, user_id, account_id)
    WHERE account_id IS NOT NULL;

-- 5. ai_chat_messages — per-message log; trimmed to last 50 by service.
CREATE TABLE IF NOT EXISTS ai_chat_messages (
    id                  UUID PRIMARY KEY,
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    thread_id           UUID NOT NULL REFERENCES ai_chat_threads(id) ON DELETE CASCADE,
    role                TEXT NOT NULL CHECK (role IN ('user', 'assistant')),
    content             TEXT NOT NULL,
    tokens_in           INT NOT NULL DEFAULT 0,
    tokens_out          INT NOT NULL DEFAULT 0,
    model_id            TEXT,
    provider            TEXT,
    context_trade_ids   UUID[],
    requested_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS ai_chat_messages_thread_time_idx
    ON ai_chat_messages (tenant_id, thread_id, requested_at ASC);

CREATE INDEX IF NOT EXISTS ai_chat_messages_tenant_time_idx
    ON ai_chat_messages (tenant_id, requested_at DESC);

-- 6. ai_usage_daily — extend Phase 6 table with recap + chat + embedding counters.
ALTER TABLE ai_usage_daily
    ADD COLUMN IF NOT EXISTS recap_requests     INT     NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS chat_requests      INT     NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS embedding_tokens   BIGINT  NOT NULL DEFAULT 0;

COMMIT;