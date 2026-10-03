-- Migration 0005 — Phase 4: billing (payments) + scheduled email reports.
-- See docs/superpowers/specs/2026-10-03-trading-journal-phase4-design.md §2.

-- ── Enums ───────────────────────────────────────────────────────────
CREATE TYPE payment_status AS ENUM ('open', 'paid', 'failed', 'refunded');
CREATE TYPE schedule_frequency AS ENUM ('daily', 'weekly', 'monthly');

-- ── payments (invoice / charge events) ─────────────────────────────────────
CREATE TABLE payments (
  id                  UUID PRIMARY KEY,
  tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  subscription_id     UUID NOT NULL REFERENCES subscriptions(id) ON DELETE CASCADE,
  provider            TEXT NOT NULL DEFAULT 'stripe',
  external_invoice_id TEXT NOT NULL,
  amount_cents        BIGINT NOT NULL CHECK (amount_cents >= 0),
  currency            CHAR(3) NOT NULL,
  status              payment_status NOT NULL,
  paid_at             TIMESTAMPTZ,
  metadata            JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (provider, external_invoice_id)
);

CREATE INDEX payments_tenant_recent_idx ON payments (tenant_id, created_at DESC);
CREATE INDEX payments_subscription_idx ON payments (subscription_id, created_at DESC);

-- ── report_schedules (cron-like PDF email delivery) ──────────────────────
CREATE TABLE report_schedules (
  id                 UUID PRIMARY KEY,
  tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  trading_account_id  UUID NOT NULL REFERENCES trading_accounts(id) ON DELETE CASCADE,
  frequency           schedule_frequency NOT NULL,
  hour_utc           INT NOT NULL CHECK (hour_utc BETWEEN 0 AND 23),
  weekday            INT CHECK (weekday BETWEEN 0 AND 6),
  day_of_month        INT CHECK (day_of_month BETWEEN 1 AND 28),
  last_sent_at        TIMESTAMPTZ,
  next_send_at        TIMESTAMPTZ NOT NULL,
  recipients         TEXT[] NOT NULL DEFAULT '{}',
  is_active          BOOLEAN NOT NULL DEFAULT TRUE,
  metadata            JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, trading_account_id, frequency)
);

-- Partial index for the ScheduleWorker poll: active schedules due now.
CREATE INDEX report_schedules_due_idx
  ON report_schedules (next_send_at)
  WHERE is_active = TRUE;