-- Tabel checkpoint verifikasi inkremental per project (Phase 2, ROADMAP §2D).
-- Terpisah dari tenant_audit_verify_cache (000009, per-TENANT, hasil
-- terakhir untuk disajikan ke user) — tabel ini per-PROJECT, murni
-- internal, cuma menandai sampai sequence mana chain sudah pernah
-- diverifikasi valid, supaya VerifyRangeIncremental tidak perlu
-- memeriksa ulang event yang sudah pernah dicek.
--
-- TIDAK menulis ke tabel events. TIDAK dibaca/ditulis oleh VerifyRange,
-- VerifyChain, ComputeHash, atau VerifyTenant (full) yang sudah ada —
-- murni dipakai jalur baru VerifyRangeIncremental/VerifyTenantIncremental
-- (events/verify_incremental.go).
--
-- Checkpoint HANYA maju kalau verifikasi bagian baru itu valid. Kalau
-- broken, baris ini TIDAK diupdate, supaya bagian yang broken tetap
-- diperiksa ulang di panggilan berikutnya sampai diperbaiki.

CREATE TABLE IF NOT EXISTS audit_verify_checkpoints (
    project_id              TEXT PRIMARY KEY,
    last_verified_sequence  BIGINT NOT NULL,
    last_verified_hash      TEXT NOT NULL,
    updated_at              TIMESTAMPTZ NOT NULL
);