-- Tabel audit_events: menggantikan BoltAuditStore (file lokal per-instance)
-- dengan storage terpusat Postgres, supaya jejak audit konsisten walau
-- ada lebih dari satu instance saas-api, dan lebih mudah di-backup/replicate
-- mengikuti strategi Postgres yang sudah ada untuk tenant/user/apikey lain.

CREATE TABLE IF NOT EXISTS audit_events (
    id          UUID PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    user_id     TEXT NOT NULL,
    action      TEXT NOT NULL,
    resource    TEXT NOT NULL,
    outcome     TEXT NOT NULL,
    "timestamp" TIMESTAMPTZ NOT NULL DEFAULT now(),
    metadata    JSONB
    -- Tidak ada kolom "anonymized_at" terpisah — konsisten dengan
    -- BoltAuditStore, status anonymize ditandai lewat metadata->>'anonymized'
    -- = 'true' (lihat anonymizeEvent di audit/store_bbolt.go).
);

-- Index utama: query paling umum adalah ListForTenant (tenant + urut waktu
-- terbaru dulu) dan CleanupForTenant (tenant + filter by timestamp cutoff).
CREATE INDEX IF NOT EXISTS idx_audit_events_tenant_timestamp
    ON audit_events (tenant_id, "timestamp" DESC);

-- ============================================================
-- TAMPER-EVIDENCE (lihat rekomendasi: audit log harus immutable)
-- ============================================================
-- Larang UPDATE apapun KECUALI proses anonymize resmi (lihat
-- anonymizeEvent di audit/store_bbolt.go, yang harus punya padanan
-- persis di audit/store_postgres.go). Anonymize resmi cirinya:
--   - id/tenant_id/action/resource/outcome/timestamp TIDAK berubah
--     (jejak "apa terjadi, kapan, hasilnya apa" tetap ada)
--   - user_id berubah MENJADI PERSIS "[anonymized]"
--   - metadata sebelumnya BELUM ditandai anonymized (mencegah re-anonymize
--     berulang menutupi tampering)
--   - metadata sesudah HARUS ditandai anonymized = 'true'
-- Kombinasi apapun di luar pola ini (termasuk mengubah action/resource/
-- outcome/timestamp, atau mengubah user_id ke nilai lain) ditolak.
CREATE OR REPLACE FUNCTION audit_events_prevent_core_update()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
        OR NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
        OR NEW.action IS DISTINCT FROM OLD.action
        OR NEW.resource IS DISTINCT FROM OLD.resource
        OR NEW.outcome IS DISTINCT FROM OLD.outcome
        OR NEW."timestamp" IS DISTINCT FROM OLD."timestamp"
    THEN
        RAISE EXCEPTION 'audit_events: core fields (id/tenant_id/action/resource/outcome/timestamp) are immutable';
    END IF;

    IF NEW.user_id IS DISTINCT FROM OLD.user_id
        OR NEW.metadata IS DISTINCT FROM OLD.metadata
    THEN
        IF NEW.user_id IS DISTINCT FROM '[anonymized]'
            OR COALESCE(OLD.metadata->>'anonymized', 'false') = 'true'
            OR COALESCE(NEW.metadata->>'anonymized', 'false') != 'true'
        THEN
            RAISE EXCEPTION 'audit_events: only the official anonymize transition is allowed (user_id -> [anonymized], metadata marked anonymized=true, exactly once)';
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_audit_events_prevent_core_update
    BEFORE UPDATE ON audit_events
    FOR EACH ROW
    EXECUTE FUNCTION audit_events_prevent_core_update();
