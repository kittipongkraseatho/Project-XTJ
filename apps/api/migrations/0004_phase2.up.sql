-- Migration 0004 — Phase 2: broker sync (mock-only).
-- See docs/superpowers/specs/2026-10-03-trading-journal-phase2-design.md §2.

-- ── Enums ───────────────────────────────────────────────────────────
CREATE TYPE broker_provider         AS ENUM ('mock','metaapi','ctrader','ib');
CREATE TYPE broker_connection_status AS ENUM ('pending','connecting','connected','disconnected','error');
CREATE TYPE sync_run_status         AS ENUM ('queued','running','succeeded','failed');
CREATE TYPE sync_run_trigger        AS ENUM ('manual','scheduled','reconnect');

-- ── broker_connections ───────────────────────────────────────────────
CREATE TABLE broker_connections (
  id                    UUID PRIMARY KEY,
  tenant_id             UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  trading_account_id    UUID NOT NULL REFERENCES trading_accounts(id) ON DELETE CASCADE,
  provider              broker_provider NOT NULL,
  status                broker_connection_status NOT NULL DEFAULT 'pending',
  external_account_id   TEXT,
  credentials_encrypted BYTEA,
  metadata              JSONB NOT NULL DEFAULT '{}'::jsonb,
  last_sync_at          TIMESTAMPTZ,
  last_error            TEXT,
  sync_interval_seconds INT NOT NULL DEFAULT 60 CHECK (sync_interval_seconds >= 10),
  created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (tenant_id, trading_account_id, provider)
);

CREATE INDEX broker_connections_tenant_status_idx
  ON broker_connections (tenant_id, status);

-- Partial index for SyncWorker poll: due connections
CREATE INDEX broker_connections_due_idx
  ON broker_connections (last_sync_at)
  WHERE status = 'connected';

-- ── sync_runs ────────────────────────────────────────────────────────
CREATE TABLE sync_runs (
  id                  UUID PRIMARY KEY,
  tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  broker_connection_id UUID NOT NULL REFERENCES broker_connections(id) ON DELETE CASCADE,
  status              sync_run_status NOT NULL DEFAULT 'queued',
  trigger             sync_run_trigger NOT NULL,
  started_at          TIMESTAMPTZ,
  finished_at         TIMESTAMPTZ,
  deals_fetched       INT NOT NULL DEFAULT 0 CHECK (deals_fetched >= 0),
  deals_imported      INT NOT NULL DEFAULT 0 CHECK (deals_imported >= 0),
  deals_skipped       INT NOT NULL DEFAULT 0 CHECK (deals_skipped >= 0),
  error               TEXT,
  metadata            JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX sync_runs_connection_recent_idx
  ON sync_runs (broker_connection_id, created_at DESC);

CREATE INDEX sync_runs_tenant_recent_idx
  ON sync_runs (tenant_id, created_at DESC);