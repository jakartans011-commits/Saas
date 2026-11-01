-- migrations/000002_quota.down.sql
DROP TABLE IF EXISTS project_usage_counters;
ALTER TABLE projects DROP COLUMN IF EXISTS monthly_event_limit;