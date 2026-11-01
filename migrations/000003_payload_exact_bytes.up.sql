-- migrations/000003_payload_exact_bytes.up.sql
ALTER TABLE events ALTER COLUMN payload TYPE JSON USING payload::text::json;