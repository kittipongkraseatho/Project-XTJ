-- Migration 0006 — Phase 5: public API + API keys.
-- See docs/superpowers/specs/2026-10-03-trading-journal-phase5-design.md §2.

-- ── api_keys (scoped tokens for the public API) ─────────────────────
CREATE TABLE api_keys (
  id                  UUID PRIMARY KEY,
  tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  name                TEXT NOT NULL,
  prefix              TEXT NOT NULL,
  secret_hash         TEXT NOT NULL,
  scopes              TEXT[] NOT NULL DEFAULT '{}',
  last_used_at        TIMESTAMPTZ,
  expires_at          TIMESTAMPTZ,
  is_active           BOOLEAN NOT NULL DEFAULT TRUE,
  created_by_user_id  UUID NOT NULL REFERENCES users(id),
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
  revoked_at          TIMESTAMPTZ,
  UNIQUE (prefix)
);

CREATE INDEX api_keys_tenant_active_idx ON api_keys (tenant_id, is_active);
CREATE INDEX api_keys_expires_idx ON api_keys (expires_at)
  WHERE is_active = TRUE;