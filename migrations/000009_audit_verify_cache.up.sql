-- Cache hasil verifikasi audit-chain per tenant (Phase 2, ROADMAP §2A).
-- Satu baris per tenant. TIDAK punya hubungan dengan tabel events dan
-- TIDAK menulis ke chain — murni cache hasil VerifyRange/VerifyChain
-- yang sudah ada (dipakai apa adanya, prinsip #2 roadmap).
--
-- status:
--   "valid"  — seluruh chain yang diperiksa konsisten
--   "broken" — ada mata rantai yang gagal (first_broken_* terisi)
--   "empty"  — tenant belum punya event apapun

CREATE TABLE IF NOT EXISTS tenant_audit_verify_cache (
    tenant_id               TEXT PRIMARY KEY,
    status                  TEXT NOT NULL,
    verified_at             TIMESTAMPTZ NOT NULL,
    events_checked          INTEGER NOT NULL DEFAULT 0,
    oldest_event_at         TIMESTAMPTZ,
    first_broken_event_id   TEXT,
    first_broken_sequence   BIGINT,
    first_broken_timestamp  TIMESTAMPTZ,
    -- Throttle POST (D9): 1 request / 5 detik per tenant, lewat update
    -- kondisional atomik (WHERE last_forced_at < now() - interval '5 seconds'),
    -- aman untuk multi-instance tanpa limiter terpisah (lihat §7 D9).
    last_forced_at          TIMESTAMPTZ
);