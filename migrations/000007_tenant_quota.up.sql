-- ============================================================
-- TENANT-LEVEL QUOTA — memperbaiki bug: quota sebelumnya di-enforce
-- per-project (project_usage_counters + projects.monthly_event_limit),
-- padahal Locked Pricing & BUSINESS_MODEL.md menjanjikan kuota
-- POOLED per tenant lintas semua project.
--
-- Kolom projects.monthly_event_limit dan tabel project_usage_counters
-- TIDAK dihapus (menghindari perubahan destruktif ke skema Locked) —
-- cukup berhenti dipakai untuk enforcement. Boleh dibersihkan di
-- migration terpisah nanti setelah dipastikan tidak ada pemanggil lain.
-- ============================================================

CREATE TABLE tenant_quotas (
    tenant_id           TEXT PRIMARY KEY,
    monthly_event_limit BIGINT NOT NULL DEFAULT 5000,
    -- Default 5000 = Free plan (Locked Pricing). ApplyPlanToTenant
    -- akan meng-upsert nilai ini sesuai plan aktif tenant.
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE tenant_usage_counters (
    tenant_id   TEXT NOT NULL REFERENCES tenant_quotas (tenant_id),
    period      TEXT NOT NULL,  -- format 'YYYY-MM', sama seperti project_usage_counters
    event_count BIGINT NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, period)
);

-- Backfill: buat baris tenant_quotas untuk semua tenant_id yang
-- sudah pernah muncul di events, supaya Append() tidak gagal
-- "failed to load tenant quota" untuk tenant existing pasca-migrasi.
-- Limit di-set ke MAX monthly_event_limit dari project mereka saat
-- ini (asumsi aman: tenant Pro/Business proyeknya sudah di-set ke
-- limit plan tsb oleh ApplyPlanToTenant versi lama) — bukan tebakan
-- acak, tapi TETAP perlu direkonsiliasi manual untuk tenant yang
-- datanya sempat drift (lihat catatan verifikasi di bawah).
INSERT INTO tenant_quotas (tenant_id, monthly_event_limit)
SELECT DISTINCT p.tenant_id, COALESCE(MAX(p.monthly_event_limit), 5000)
FROM projects p
GROUP BY p.tenant_id
ON CONFLICT (tenant_id) DO NOTHING;
