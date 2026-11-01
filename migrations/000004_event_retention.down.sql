DROP TABLE IF EXISTS event_retention_checkpoints;

ALTER TABLE projects
DROP COLUMN IF EXISTS retention_days;