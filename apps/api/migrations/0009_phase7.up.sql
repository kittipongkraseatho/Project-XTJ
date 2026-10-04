-- 0009_phase7.up.sql
-- Phase 7 — Stripe webhook state machine.
--
-- 1. webhook_events     — idempotency log for Stripe events.
--                         UNIQUE on stripe_event_id gives us at-most-once apply.
-- 2. The `subscriptions` and `payments` tables already carry
--    `external_subscription_id` and `external_invoice_id` UNIQUE columns
--    from Phase 1 + Phase 4, so we don't need to add new columns there.
--    We only need a few new repo methods (added in phase9_repos.go).

BEGIN;

CREATE TABLE IF NOT EXISTS webhook_events (
    id              UUID PRIMARY KEY,
    stripe_event_id TEXT NOT NULL,
    event_type      TEXT NOT NULL,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS webhook_events_stripe_id_idx
    ON webhook_events (stripe_event_id);

CREATE INDEX IF NOT EXISTS webhook_events_received_idx
    ON webhook_events (received_at DESC);

COMMIT;