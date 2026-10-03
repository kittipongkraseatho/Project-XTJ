-- Migration 0001 DOWN — drop 7 base tables + extensions.
-- Order matters: dependents first.

DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS refresh_sessions;
DROP TABLE IF EXISTS subscriptions;
DROP TABLE IF EXISTS memberships;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS tenants;
DROP TABLE IF EXISTS plans;

DROP TYPE IF EXISTS session_status;
DROP TYPE IF EXISTS subscription_status;
DROP TYPE IF EXISTS plan_interval;
DROP TYPE IF EXISTS plan_code;
DROP TYPE IF EXISTS membership_status;
DROP TYPE IF EXISTS membership_role;
DROP TYPE IF EXISTS tenant_status;
DROP TYPE IF EXISTS user_status;

-- Keep extensions installed (other DBs may use them). If you want a clean slate:
-- DROP EXTENSION IF EXISTS citext;
-- DROP EXTENSION IF EXISTS pgcrypto;