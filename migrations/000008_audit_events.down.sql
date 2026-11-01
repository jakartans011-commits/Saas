DROP TRIGGER IF EXISTS trg_audit_events_prevent_core_update ON audit_events;
DROP FUNCTION IF EXISTS audit_events_prevent_core_update();
DROP TABLE IF EXISTS audit_events;
