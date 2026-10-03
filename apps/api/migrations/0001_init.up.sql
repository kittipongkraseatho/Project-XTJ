-- Migration 0001 — Phase 0 Foundation: 7 base tables + 4 plan seeds.
-- See docs/superpowers/specs/2026-10-03-trading-journal-phase0-design.md §6.

-- ── Extensions ──────────────────────────────────────────────────────
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;

-- ── Enums ───────────────────────────────────────────────────────────
CREATE TYPE user_status         AS ENUM ('active','suspended','pending_verification');
CREATE TYPE tenant_status       AS ENUM ('active','suspended');
CREATE TYPE membership_role     AS ENUM ('owner','admin','member','viewer');
CREATE TYPE membership_status   AS ENUM ('active','invited','suspended');
CREATE TYPE plan_code           AS ENUM ('free','upload_journal','starter','pro');
CREATE TYPE plan_interval       AS ENUM ('monthly','yearly');
CREATE TYPE subscription_status AS ENUM ('trialing','active','past_due','canceled','expired');
CREATE TYPE session_status      AS ENUM ('active','revoked','expired');

-- ── plans (global, no tenant_id) ────────────────────────────────────
CREATE TABLE plans (
  id          UUID         PRIMARY KEY,
  code        plan_code    NOT NULL UNIQUE,
  name        TEXT         NOT NULL,
  description TEXT,
  price_cents BIGINT       NOT NULL DEFAULT 0 CHECK (price_cents >= 0),
  currency    CHAR(3)      NOT NULL DEFAULT 'THB',
  interval    plan_interval NOT NULL DEFAULT 'monthly',
  is_active   BOOLEAN      NOT NULL DEFAULT TRUE,
  metadata    JSONB        NOT NULL DEFAULT '{}'::jsonb,
  created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- ── tenants ─────────────────────────────────────────────────────────
CREATE TABLE tenants (
  id         UUID         PRIMARY KEY,
  name       TEXT         NOT NULL,
  slug       TEXT         NOT NULL UNIQUE,
  status     tenant_status NOT NULL DEFAULT 'active',
  metadata   JSONB        NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  deleted_at TIMESTAMPTZ
);
CREATE INDEX idx_tenants_slug_active ON tenants(slug) WHERE deleted_at IS NULL;

-- ── users ───────────────────────────────────────────────────────────
CREATE TABLE users (
  id                          UUID         PRIMARY KEY,
  email                       CITEXT       NOT NULL UNIQUE,
  password_hash               TEXT         NOT NULL,
  display_name                TEXT         NOT NULL,
  avatar_cloudinary_public_id TEXT,
  status                      user_status  NOT NULL DEFAULT 'active',
  metadata                    JSONB        NOT NULL DEFAULT '{}'::jsonb,
  created_at                  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  updated_at                  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  deleted_at                  TIMESTAMPTZ,
  CONSTRAINT users_email_format CHECK (email ~ '^[^@[:space:]]+@[^@[:space:]]+\.[^@[:space:]]+$')
);
CREATE INDEX idx_users_email_active ON users(email) WHERE deleted_at IS NULL;

-- ── memberships (user ↔ tenant ↔ role) ──────────────────────────────
CREATE TABLE memberships (
  id         UUID              PRIMARY KEY,
  tenant_id  UUID              NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  user_id    UUID              NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role       membership_role   NOT NULL DEFAULT 'owner',
  status     membership_status NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ       NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ       NOT NULL DEFAULT NOW(),
  UNIQUE (tenant_id, user_id)
);
CREATE INDEX idx_memberships_user   ON memberships(user_id);
CREATE INDEX idx_memberships_tenant ON memberships(tenant_id);
CREATE INDEX idx_memberships_role   ON memberships(tenant_id, role);

-- ── subscriptions (tenant → plan) ───────────────────────────────────
CREATE TABLE subscriptions (
  id                       UUID                 PRIMARY KEY,
  tenant_id                UUID                 NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  plan_id                  UUID                 NOT NULL REFERENCES plans(id),
  status                   subscription_status NOT NULL DEFAULT 'active',
  started_at               TIMESTAMPTZ          NOT NULL DEFAULT NOW(),
  current_period_start     TIMESTAMPTZ          NOT NULL,
  current_period_end       TIMESTAMPTZ          NOT NULL,
  cancel_at                TIMESTAMPTZ,
  canceled_at              TIMESTAMPTZ,
  external_provider        TEXT,
  external_subscription_id TEXT,
  metadata                 JSONB                NOT NULL DEFAULT '{}'::jsonb,
  created_at               TIMESTAMPTZ          NOT NULL DEFAULT NOW(),
  updated_at               TIMESTAMPTZ          NOT NULL DEFAULT NOW(),
  CONSTRAINT subscriptions_period_valid CHECK (current_period_end > current_period_start)
);
CREATE INDEX              idx_subscriptions_tenant_status ON subscriptions(tenant_id, status);
CREATE UNIQUE INDEX       uniq_subscriptions_tenant_active
  ON subscriptions(tenant_id) WHERE status IN ('active','trialing');

-- ── refresh_sessions (rotating opaque tokens) ───────────────────────
CREATE TABLE refresh_sessions (
  id                    UUID            PRIMARY KEY,
  user_id               UUID            NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id             UUID            NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  token_hash            TEXT            NOT NULL UNIQUE,
  status                session_status  NOT NULL DEFAULT 'active',
  user_agent            TEXT,
  ip                    INET,
  created_at            TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
  last_used_at          TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
  expires_at            TIMESTAMPTZ     NOT NULL,
  revoked_at            TIMESTAMPTZ,
  replaced_by_session_id UUID           REFERENCES refresh_sessions(id),
  CONSTRAINT refresh_sessions_expiry_after_create CHECK (expires_at > created_at)
);
CREATE INDEX idx_refresh_sessions_user_active ON refresh_sessions(user_id) WHERE status = 'active';
CREATE INDEX idx_refresh_sessions_expires      ON refresh_sessions(expires_at) WHERE status = 'active';

-- ── audit_logs (append-only, immutable) ────────────────────────────
CREATE TABLE audit_logs (
  id              BIGSERIAL    PRIMARY KEY,
  occurred_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  actor_user_id   UUID         REFERENCES users(id),
  actor_tenant_id UUID         REFERENCES tenants(id),
  action          TEXT         NOT NULL,
  resource_type   TEXT,
  resource_id     UUID,
  ip              INET,
  user_agent      TEXT,
  request_id      UUID,
  metadata        JSONB        NOT NULL DEFAULT '{}'::jsonb
);
CREATE INDEX idx_audit_logs_tenant_time ON audit_logs(actor_tenant_id, occurred_at DESC);
CREATE INDEX idx_audit_logs_actor_time  ON audit_logs(actor_user_id,   occurred_at DESC);
CREATE INDEX idx_audit_logs_action_time ON audit_logs(action,           occurred_at DESC);

-- ── Seed: 4 plans ──────────────────────────────────────────────────
INSERT INTO plans (id, code, name, description, price_cents, currency, interval, is_active) VALUES
  (gen_random_uuid(), 'free',           'Free',           'ทดลองใช้ฟรี',                0,      'THB', 'monthly', TRUE),
  (gen_random_uuid(), 'upload_journal', 'Upload Journal', 'อัปโหลด MT5 history',       9900,   'THB', 'monthly', TRUE),
  (gen_random_uuid(), 'starter',        'Starter',        'MT5 Auto Sync',             19900,  'THB', 'monthly', TRUE),
  (gen_random_uuid(), 'pro',            'Pro',            'Multi-account + Advanced',  39900,  'THB', 'monthly', TRUE)
ON CONFLICT (code) DO NOTHING;