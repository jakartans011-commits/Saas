-- ============================================================
-- TRAILSEAL — INITIAL SCHEMA
-- Organization (Tenant) tetap di bbolt, TIDAK dipindah ke sini.
-- Tabel di bawah ini scoped di bawah tenant_id (string, sama
-- dengan Tenant.ID di package tenant/ — bukan foreign key lintas
-- database, hanya referensi logis).
-- ============================================================

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ============================================================
-- PROJECTS
-- ============================================================

CREATE TABLE projects (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   TEXT NOT NULL,
    name        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'active'
                CHECK (status IN ('active', 'suspended', 'deleted')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);

CREATE INDEX idx_projects_tenant ON projects (tenant_id) WHERE deleted_at IS NULL;

-- ============================================================
-- API KEYS
-- key_hash = SHA-256(plaintext key), UNIQUE, dipakai untuk lookup.
-- Plaintext key TIDAK PERNAH disimpan — hanya dikembalikan sekali
-- ke client saat dibuat.
-- ============================================================

CREATE TABLE api_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES projects (id),
    tenant_id    TEXT NOT NULL,
    name         TEXT NOT NULL,
    key_hash     TEXT NOT NULL,
    key_prefix   TEXT NOT NULL,          -- 8 char pertama, untuk ditampilkan di UI
    scope        TEXT NOT NULL
                 CHECK (scope IN ('ingest', 'read', 'admin')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_api_keys_hash ON api_keys (key_hash);
CREATE INDEX idx_api_keys_project ON api_keys (project_id) WHERE revoked_at IS NULL;

-- ============================================================
-- PROJECT CHAIN STATE
-- Satu baris per project, dikunci (SELECT ... FOR UPDATE) tiap
-- kali ada event baru masuk — inilah yang menjamin sequence dan
-- prev_hash tidak pernah corrupt di concurrent write.
-- ============================================================

CREATE TABLE project_chain_state (
    project_id     UUID PRIMARY KEY REFERENCES projects (id),
    last_sequence  BIGINT NOT NULL DEFAULT 0,
    last_hash      TEXT,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================
-- EVENTS
-- sequence monotonic PER PROJECT (bukan global) — dimulai dari 1.
-- hash = SHA256(project_id | sequence | occurred_at | event_type |
--               actor_id | payload | prev_hash)
-- ============================================================

CREATE TABLE events (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES projects (id),
    tenant_id    TEXT NOT NULL,
    sequence     BIGINT NOT NULL,
    event_type   TEXT NOT NULL,
    actor_id     TEXT NOT NULL DEFAULT '',
    payload      JSONB NOT NULL DEFAULT '{}',
    prev_hash    TEXT,
    hash         TEXT NOT NULL,
    occurred_at  TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_events_project_sequence ON events (project_id, sequence);
CREATE INDEX idx_events_project_time ON events (project_id, occurred_at);
CREATE INDEX idx_events_project_type ON events (project_id, event_type);

-- ============================================================
-- IDEMPOTENCY
-- Kunci dedup per project. Kalau klien retry request ingest yang
-- sama (network timeout dsb), request kedua tidak menambah event
-- baru — dikembalikan event yang sudah ada.
-- ============================================================

CREATE TABLE idempotency_keys (
    project_id       UUID NOT NULL REFERENCES projects (id),
    idempotency_key  TEXT NOT NULL,
    event_id         UUID NOT NULL REFERENCES events (id),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, idempotency_key)
);