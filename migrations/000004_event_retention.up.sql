-- ============================================================
-- EVENT RETENTION
-- Default 90 hari, dapat diubah per project.
-- ============================================================

ALTER TABLE projects
ADD COLUMN IF NOT EXISTS retention_days INT NOT NULL DEFAULT 90;

CREATE TABLE IF NOT EXISTS event_retention_checkpoints (
    project_id              UUID PRIMARY KEY REFERENCES projects(id),
    purged_through_sequence BIGINT NOT NULL,
    purged_through_hash     TEXT NOT NULL,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_events_retention
    ON events (project_id, occurred_at);