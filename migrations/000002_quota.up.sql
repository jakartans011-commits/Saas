-- ============================================================
-- QUOTA — monthly event limit per project + counter table
-- ============================================================

ALTER TABLE projects
    ADD COLUMN monthly_event_limit BIGINT NOT NULL DEFAULT 10000;
    -- Default sementara untuk Fase 1 (belum ada tier billing).
    -- Fase 3 nanti akan set nilai ini berdasarkan plan customer.

CREATE TABLE project_usage_counters (
    project_id  UUID NOT NULL REFERENCES projects (id),
    period      TEXT NOT NULL,  -- format 'YYYY-MM', mis. '2026-08'
    event_count BIGINT NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, period)
);