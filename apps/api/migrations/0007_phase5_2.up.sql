-- 0007_phase5_2.up.sql
-- Phase 5.2 — Webhook delivery (outbound)
--
-- 1. webhook_subscriptions — tenant-owned receiver config
--    Plaintext secret at rest (DB at-rest encryption). Per-subscription.
-- 2. webhook_deliveries    — per-attempt log; one row per (subscription, event)

BEGIN;

CREATE TABLE IF NOT EXISTS webhook_subscriptions (
    id                  UUID PRIMARY KEY,
    url                 TEXT NOT NULL,
    event_types         TEXT[] NOT NULL DEFAULT '{}',
    secret              TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    is_active           BOOLEAN NOT NULL DEFAULT TRUE,
    tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    created_by_user_id  UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    revoked_at          TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS webhook_subscriptions_tenant_active_idx
    ON webhook_subscriptions (tenant_id, is_active);

CREATE INDEX IF NOT EXISTS webhook_subscriptions_tenant_idx
    ON webhook_subscriptions (tenant_id);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id                  UUID PRIMARY KEY,
    tenant_id           UUID NOT NULL,
    subscription_id     UUID NOT NULL REFERENCES webhook_subscriptions(id) ON DELETE CASCADE,
    event_type          TEXT NOT NULL,
    event_id            TEXT NOT NULL,
    payload             JSONB NOT NULL,
    attempt             INT NOT NULL DEFAULT 0,
    max_attempts        INT NOT NULL DEFAULT 5,
    status              TEXT NOT NULL DEFAULT 'pending',
    response_code       INT,
    response_body       TEXT,
    error               TEXT,
    next_attempt_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_attempted_at   TIMESTAMPTZ,
    completed_at        TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS webhook_deliveries_event_id_idx
    ON webhook_deliveries (event_id);

CREATE INDEX IF NOT EXISTS webhook_deliveries_pending_idx
    ON webhook_deliveries (next_attempt_at)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS webhook_deliveries_subscription_idx
    ON webhook_deliveries (subscription_id, created_at DESC);

COMMIT;