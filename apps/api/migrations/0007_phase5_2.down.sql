-- 0007_phase5_2.down.sql
-- Phase 5.2 rollback
BEGIN;
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhook_subscriptions;
COMMIT;