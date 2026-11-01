-- migrations/000011_drop_duplicate_event_index.down.sql
CREATE INDEX IF NOT EXISTS idx_events_retention ON events (project_id, occurred_at);