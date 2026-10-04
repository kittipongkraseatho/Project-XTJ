-- 0010_phase9.down.sql
-- Phase 9 — drop in reverse order. pgvector extension left in place (other
-- tables may rely on it in future phases).
BEGIN;

ALTER TABLE ai_usage_daily
    DROP COLUMN IF EXISTS embedding_tokens,
    DROP COLUMN IF EXISTS chat_requests,
    DROP COLUMN IF EXISTS recap_requests;

DROP TABLE IF EXISTS ai_chat_messages;
DROP TABLE IF EXISTS ai_chat_threads;
DROP TABLE IF EXISTS ai_recaps;
DROP TABLE IF EXISTS trade_embeddings;

COMMIT;