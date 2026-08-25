BEGIN;

-- Retain the canonical remote library IDs selected for each immutable source
-- configuration audit event. This preserves historical operator intent even
-- after the source's current selected-library projection changes.
CREATE TABLE inventory_source_configuration_audit_selected_libraries (
    audit_id UUID NOT NULL REFERENCES inventory_source_configuration_audit(id) ON DELETE RESTRICT,
    remote_library_id TEXT NOT NULL CHECK (octet_length(remote_library_id) BETWEEN 1 AND 256),
    PRIMARY KEY (audit_id, remote_library_id)
);

-- This evidence follows the same append-only retention rule as its audit
-- parent. It intentionally reuses the 000015 trigger function so direct SQL
-- cannot alter or remove recorded selections.
CREATE TRIGGER inventory_source_configuration_audit_selected_libraries_append_only
    BEFORE UPDATE OR DELETE ON inventory_source_configuration_audit_selected_libraries
    FOR EACH ROW EXECUTE FUNCTION prevent_inventory_source_evidence_delete();

COMMIT;
