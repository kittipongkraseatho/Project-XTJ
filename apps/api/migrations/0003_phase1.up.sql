-- Migration 0003 — Phase 1: trading accounts, MT5 parser, journal, tags, analytics.
-- See docs/superpowers/specs/2026-10-03-trading-journal-phase1-design.md §2.

-- ── Enums ───────────────────────────────────────────────────────────
CREATE TYPE account_broker        AS ENUM ('mt5','mt4','ctrader','custom');
CREATE TYPE account_kind          AS ENUM ('demo','real');
CREATE TYPE import_format         AS ENUM ('mt5_html','mt5_csv','generic_csv');
CREATE TYPE import_status         AS ENUM ('queued','running','completed','failed','cancelled');
CREATE TYPE deal_type             AS ENUM ('buy','sell','balance','credit','commission','correction');
CREATE TYPE deal_entry_exit        AS ENUM ('entry','exit','entry_inout','exit_inout','inout');
CREATE TYPE position_state        AS ENUM ('open','closed');
CREATE TYPE trade_direction       AS ENUM ('long','short');
CREATE TYPE trade_outcome         AS ENUM ('win','loss','breakeven');
CREATE TYPE tag_owner             AS ENUM ('system','user');
CREATE TYPE emotion               AS ENUM ('calm','neutral','fomo','anxious','confident','fearful','greedy');
CREATE TYPE image_status          AS ENUM ('uploading','ready','failed','deleted');

-- ── trading_accounts ─────────────────────────────────────────────────
CREATE TABLE trading_accounts (
  id              UUID PRIMARY KEY,
  user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  broker          account_broker NOT NULL DEFAULT 'mt5',
  broker_account_id TEXT NOT NULL,        -- MT5 login / account id from broker
  display_name    TEXT NOT NULL,
  currency        CHAR(3) NOT NULL DEFAULT 'USD',
  leverage        NUMERIC(10,2),
  kind            account_kind NOT NULL DEFAULT 'real',
  metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
  is_active       BOOLEAN NOT NULL DEFAULT TRUE,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (tenant_id, broker, broker_account_id)
);
CREATE INDEX idx_trading_accounts_user ON trading_accounts(user_id);

-- ── imported_files ───────────────────────────────────────────────────
CREATE TABLE imported_files (
  id              UUID PRIMARY KEY,
  tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  trading_account_id UUID NOT NULL REFERENCES trading_accounts(id) ON DELETE CASCADE,
  user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  format          import_format NOT NULL,
  original_filename TEXT NOT NULL,
  byte_size       BIGINT NOT NULL CHECK (byte_size > 0),
  content_hash    TEXT NOT NULL,        -- sha256 hex of file
  storage_path    TEXT,                -- S3/disk path; Phase 1 stores the parsed text in import_jobs.metadata
  metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (tenant_id, content_hash)
);
CREATE INDEX idx_imported_files_account_time ON imported_files(trading_account_id, created_at DESC);

-- ── import_jobs ──────────────────────────────────────────────────────
CREATE TABLE import_jobs (
  id              UUID PRIMARY KEY,
  tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  imported_file_id UUID NOT NULL REFERENCES imported_files(id) ON DELETE CASCADE,
  status          import_status NOT NULL DEFAULT 'queued',
  started_at      TIMESTAMPTZ,
  finished_at     TIMESTAMPTZ,
  parsed_rows     INTEGER NOT NULL DEFAULT 0,
  inserted_rows   INTEGER NOT NULL DEFAULT 0,
  skipped_rows    INTEGER NOT NULL DEFAULT 0,
  errors          JSONB NOT NULL DEFAULT '[]'::jsonb,
  worker_id       TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_import_jobs_tenant_status ON import_jobs(tenant_id, status, created_at DESC);

-- ── orders (raw order events, optional for analytics) ────────────────
CREATE TABLE orders (
  id              UUID PRIMARY KEY,
  tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  trading_account_id UUID NOT NULL REFERENCES trading_accounts(id) ON DELETE CASCADE,
  provider_order_id TEXT NOT NULL,
  symbol          TEXT,
  order_type      TEXT,                -- 'market','stop','limit',...
  side            deal_type,
  volume          NUMERIC(18,8),
  price           NUMERIC(18,8),
  placed_at       TIMESTAMPTZ,
  filled_at       TIMESTAMPTZ,
  status          TEXT,
  metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (tenant_id, trading_account_id, provider_order_id)
);

-- ── deals (raw fills — primary dedup target) ─────────────────────────
CREATE TABLE deals (
  id              UUID PRIMARY KEY,
  tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  trading_account_id UUID NOT NULL REFERENCES trading_accounts(id) ON DELETE CASCADE,
  import_job_id   UUID REFERENCES import_jobs(id) ON DELETE SET NULL,
  provider_deal_id TEXT NOT NULL,        -- MT5 deal ticket
  symbol          TEXT NOT NULL,
  deal_type       deal_type NOT NULL,
  entry_type      deal_entry_exit NOT NULL,
  volume          NUMERIC(18,8) NOT NULL CHECK (volume >= 0),
  price           NUMERIC(18,8) NOT NULL,
  commission      NUMERIC(18,8) NOT NULL DEFAULT 0,
  swap            NUMERIC(18,8) NOT NULL DEFAULT 0,
  profit          NUMERIC(18,8) NOT NULL DEFAULT 0,
  magic           BIGINT,
  comment         TEXT,
  executed_at     TIMESTAMPTZ NOT NULL,
  metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (tenant_id, trading_account_id, provider_deal_id)
);
CREATE INDEX idx_deals_account_executed ON deals(trading_account_id, executed_at);
CREATE INDEX idx_deals_symbol_executed  ON deals(tenant_id, symbol, executed_at);

-- ── positions (broker-level position concept) ───────────────────────
CREATE TABLE positions (
  id              UUID PRIMARY KEY,
  tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  trading_account_id UUID NOT NULL REFERENCES trading_accounts(id) ON DELETE CASCADE,
  provider_position_id TEXT NOT NULL,    -- MT5 position ticket
  direction       trade_direction NOT NULL,
  symbol          TEXT NOT NULL,
  volume          NUMERIC(18,8) NOT NULL,
  open_price      NUMERIC(18,8) NOT NULL,
  close_price     NUMERIC(18,8),
  open_time       TIMESTAMPTZ NOT NULL,
  close_time      TIMESTAMPTZ,
  state           position_state NOT NULL DEFAULT 'open',
  realized_p_l    NUMERIC(18,8) NOT NULL DEFAULT 0,
  commission      NUMERIC(18,8) NOT NULL DEFAULT 0,
  swap            NUMERIC(18,8) NOT NULL DEFAULT 0,
  magic           BIGINT,
  metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (tenant_id, trading_account_id, provider_position_id)
);
CREATE INDEX idx_positions_account_open_time ON positions(trading_account_id, open_time);
CREATE INDEX idx_positions_tenant_symbol ON positions(tenant_id, symbol);

-- ── trades (derived — what the user sees) ────────────────────────────
CREATE TABLE trades (
  id              UUID PRIMARY KEY,
  tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  trading_account_id UUID NOT NULL REFERENCES trading_accounts(id) ON DELETE CASCADE,
  position_id     UUID REFERENCES positions(id) ON DELETE SET NULL,
  symbol          TEXT NOT NULL,
  direction       trade_direction NOT NULL,
  volume          NUMERIC(18,8) NOT NULL,
  open_price      NUMERIC(18,8) NOT NULL,
  close_price     NUMERIC(18,8),
  open_time       TIMESTAMPTZ NOT NULL,
  close_time      TIMESTAMPTZ,
  holding_seconds BIGINT,
  gross_profit    NUMERIC(18,8) NOT NULL DEFAULT 0,
  gross_loss      NUMERIC(18,8) NOT NULL DEFAULT 0,
  net_p_l         NUMERIC(18,8) NOT NULL DEFAULT 0,
  commission      NUMERIC(18,8) NOT NULL DEFAULT 0,
  swap            NUMERIC(18,8) NOT NULL DEFAULT 0,
  magic           BIGINT,
  outcome         trade_outcome NOT NULL DEFAULT 'breakeven',
  is_partial      BOOLEAN NOT NULL DEFAULT FALSE,
  metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_trades_tenant_close_time ON trades(tenant_id, close_time DESC NULLS LAST);
CREATE INDEX idx_trades_account_close_time ON trades(trading_account_id, close_time DESC NULLS LAST);
CREATE INDEX idx_trades_symbol ON trades(tenant_id, symbol);

-- ── trade_tags + tags ────────────────────────────────────────────────
CREATE TABLE tags (
  id          UUID PRIMARY KEY,
  tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  color       TEXT,
  owner       tag_owner NOT NULL DEFAULT 'user',
  metadata    JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (tenant_id, name)
);
CREATE INDEX idx_tags_tenant ON tags(tenant_id);

CREATE TABLE trade_tags (
  trade_id    UUID NOT NULL REFERENCES trades(id) ON DELETE CASCADE,
  tag_id      UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (trade_id, tag_id)
);
CREATE INDEX idx_trade_tags_tag ON trade_tags(tag_id);

-- ── journal_entries (1:1 with trade) ────────────────────────────────
CREATE TABLE journal_entries (
  id              UUID PRIMARY KEY,
  trade_id        UUID NOT NULL UNIQUE REFERENCES trades(id) ON DELETE CASCADE,
  tenant_id       UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  strategy        TEXT,
  entry_reason    TEXT,
  exit_reason     TEXT,
  emotion         emotion,
  mistake         TEXT,
  notes           TEXT,
  metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_journal_entries_tenant ON journal_entries(tenant_id);

-- ── trade_images (Cloudinary metadata only) ─────────────────────────
CREATE TABLE trade_images (
  id                          UUID PRIMARY KEY,
  tenant_id                   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  trade_id                    UUID NOT NULL REFERENCES trades(id) ON DELETE CASCADE,
  user_id                     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  cloudinary_public_id        TEXT NOT NULL,
  cloudinary_asset_id         TEXT,
  resource_type               TEXT NOT NULL DEFAULT 'image',
  format                      TEXT NOT NULL,
  width                       INTEGER,
  height                      INTEGER,
  bytes                       BIGINT NOT NULL CHECK (bytes >= 0),
  secure_url                  TEXT NOT NULL,
  folder                      TEXT,
  version                     BIGINT,
  status                      image_status NOT NULL DEFAULT 'ready',
  deleted_at                  TIMESTAMPTZ,
  created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_trade_images_trade ON trade_images(trade_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_trade_images_tenant ON trade_images(tenant_id);

-- ── daily_metrics (precomputed analytics aggregates) ────────────────
CREATE TABLE daily_metrics (
  id                  UUID PRIMARY KEY,
  tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  trading_account_id  UUID REFERENCES trading_accounts(id) ON DELETE CASCADE,
  trade_date          DATE NOT NULL,
  trade_count         INTEGER NOT NULL DEFAULT 0,
  win_count           INTEGER NOT NULL DEFAULT 0,
  loss_count          INTEGER NOT NULL DEFAULT 0,
  gross_profit        NUMERIC(18,8) NOT NULL DEFAULT 0,
  gross_loss          NUMERIC(18,8) NOT NULL DEFAULT 0,
  net_p_l             NUMERIC(18,8) NOT NULL DEFAULT 0,
  commission          NUMERIC(18,8) NOT NULL DEFAULT 0,
  swap                NUMERIC(18,8) NOT NULL DEFAULT 0,
  ending_equity       NUMERIC(18,8),
  metadata            JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (tenant_id, trading_account_id, trade_date)
);
CREATE INDEX idx_daily_metrics_tenant_date ON daily_metrics(tenant_id, trade_date DESC);