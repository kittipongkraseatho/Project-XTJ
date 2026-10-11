BEGIN;

ALTER TABLE trades DROP COLUMN IF EXISTS strategy_version_id;
DROP TABLE IF EXISTS trade_behavior_findings;
DROP TABLE IF EXISTS trade_behavior_profiles;
ALTER TABLE strategies DROP CONSTRAINT IF EXISTS strategies_current_version_fk;
DROP TABLE IF EXISTS strategy_versions;
DROP TABLE IF EXISTS strategies;

COMMIT;
