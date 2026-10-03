-- Migration 0002 — OAuth provider identities + flow state.
-- See docs/superpowers/specs/2026-10-03-trading-journal-phase0-design.md §7A.

CREATE TYPE identity_provider AS ENUM ('google','line','github');

-- user_identities: permanent record of an external provider identity linked to our user.
CREATE TABLE user_identities (
  id             UUID              PRIMARY KEY,
  user_id        UUID              NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  tenant_id      UUID              NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  provider       identity_provider NOT NULL,
  provider_sub   TEXT              NOT NULL,
  email          CITEXT,
  email_verified BOOLEAN           NOT NULL DEFAULT FALSE,
  display_name   TEXT,
  avatar_url     TEXT,
  raw_profile    JSONB             NOT NULL DEFAULT '{}'::jsonb,
  created_at     TIMESTAMPTZ       NOT NULL DEFAULT NOW(),
  updated_at     TIMESTAMPTZ       NOT NULL DEFAULT NOW(),
  last_used_at   TIMESTAMPTZ       NOT NULL DEFAULT NOW(),
  UNIQUE (provider, provider_sub)
);
CREATE INDEX idx_user_identities_user     ON user_identities(user_id);
CREATE INDEX idx_user_identities_tenant   ON user_identities(tenant_id);
CREATE INDEX idx_user_identities_provider ON user_identities(provider, provider_sub);

-- oauth_states: transient (10-min TTL) state for the OAuth round-trip.
-- Stores PKCE code_verifier server-side (more secure than FE storage).
CREATE TABLE oauth_states (
  state_token    TEXT         PRIMARY KEY,
  flow           TEXT         NOT NULL,
  provider       identity_provider NOT NULL,
  redirect_uri   TEXT,
  code_verifier  TEXT,
  ip             INET,
  user_agent     TEXT,
  created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
  expires_at     TIMESTAMPTZ  NOT NULL,
  consumed_at    TIMESTAMPTZ
);
CREATE INDEX idx_oauth_states_expires ON oauth_states(expires_at) WHERE consumed_at IS NULL;