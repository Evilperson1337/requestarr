BEGIN;

-- v23 restores forward constraint-trigger checks after the dirty-v20 replay,
-- but existing orphan audit rows do not fire a new trigger. Refuse this
-- reachable correction rather than certifying historical evidence that lacks
-- its immutable revision or audit-selection counterpart.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
          FROM inventory_source_configuration_audit AS audit
          LEFT JOIN inventory_source_configuration_revisions AS revision
            ON revision.audit_id = audit.id
           AND revision.source_id = audit.source_id
           AND revision.revision = audit.configuration_revision
         WHERE revision.audit_id IS NULL
    ) THEN
        RAISE EXCEPTION 'inventory source audit evidence must have matching revision evidence'
            USING ERRCODE = '23514';
    END IF;

    IF EXISTS (
        SELECT 1
          FROM inventory_source_configuration_audit_selected_libraries AS selection
          LEFT JOIN inventory_source_configuration_revisions AS revision
            ON revision.audit_id = selection.audit_id
         WHERE revision.audit_id IS NULL
    ) THEN
        RAISE EXCEPTION 'inventory source audit selection evidence must have matching revision evidence'
            USING ERRCODE = '23514';
    END IF;
END $$;

COMMIT;
