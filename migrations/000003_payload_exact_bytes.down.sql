-- migrations/000003_payload_exact_bytes.down.sql
ALTER TABLE events ALTER COLUMN payload TYPE JSONB USING payload::text::jsonb;