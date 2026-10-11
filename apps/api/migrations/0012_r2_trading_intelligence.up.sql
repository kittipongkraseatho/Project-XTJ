-- 0012_r2_trading_intelligence.up.sql
-- R2 behavior profile and immutable strategy versioning.

BEGIN;

CREATE TABLE IF NOT EXISTS strategies (
    id                 UUID PRIMARY KEY,
    tenant_id          UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    owner_user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name               TEXT NOT NULL,
    description        TEXT NOT NULL DEFAULT '',
    status             TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','active','archived')),
    current_version_id UUID,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_strategies_tenant_status
    ON strategies (tenant_id, status, created_at DESC);

CREATE TABLE IF NOT EXISTS strategy_versions (
    id             UUID PRIMARY KEY,
    tenant_id      UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    strategy_id    UUID NOT NULL REFERENCES strategies(id) ON DELETE CASCADE,
    version_no     INTEGER NOT NULL CHECK (version_no > 0),
    schema_version INTEGER NOT NULL CHECK (schema_version = 1),
    rules          JSONB NOT NULL,
    created_by     UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (strategy_id, version_no),
    UNIQUE (tenant_id, id)
);

ALTER TABLE strategies
    ADD CONSTRAINT strategies_current_version_fk
    FOREIGN KEY (current_version_id) REFERENCES strategy_versions(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_strategy_versions_tenant_strategy
    ON strategy_versions (tenant_id, strategy_id, version_no DESC);

CREATE TABLE IF NOT EXISTS trade_behavior_profiles (
    id                   UUID PRIMARY KEY,
    tenant_id            UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    trading_account_id   UUID NOT NULL REFERENCES trading_accounts(id) ON DELETE CASCADE,
    window_start         TIMESTAMPTZ NOT NULL,
    window_end           TIMESTAMPTZ NOT NULL,
    algorithm_version    TEXT NOT NULL,
    status               TEXT NOT NULL CHECK (status IN ('insufficient_data','ready')),
    sample_size          INTEGER NOT NULL CHECK (sample_size >= 0),
    metrics              JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, trading_account_id, window_start, window_end, algorithm_version),
    CHECK (window_start < window_end)
);

CREATE INDEX IF NOT EXISTS idx_behavior_profiles_account_window
    ON trade_behavior_profiles (tenant_id, trading_account_id, window_end DESC);

CREATE TABLE IF NOT EXISTS trade_behavior_findings (
    id          UUID PRIMARY KEY,
    tenant_id   UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    profile_id  UUID NOT NULL REFERENCES trade_behavior_profiles(id) ON DELETE CASCADE,
    code        TEXT NOT NULL,
    severity    TEXT NOT NULL CHECK (severity IN ('info','warning','critical')),
    finding_count INTEGER NOT NULL CHECK (finding_count >= 0),
    evidence    JSONB NOT NULL DEFAULT '{}'::jsonb,
    UNIQUE (profile_id, code)
);

CREATE INDEX IF NOT EXISTS idx_behavior_findings_tenant_profile
    ON trade_behavior_findings (tenant_id, profile_id);

ALTER TABLE trades
    ADD COLUMN IF NOT EXISTS strategy_version_id UUID REFERENCES strategy_versions(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_trades_tenant_strategy_version
    ON trades (tenant_id, strategy_version_id)
    WHERE strategy_version_id IS NOT NULL;

COMMIT;
