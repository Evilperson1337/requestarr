BEGIN;

-- Durable audit and queue history must be explicitly cleared before a rollback.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM inventory_authoritative_cycles)
       OR EXISTS (SELECT 1 FROM inventory_source_status)
       OR EXISTS (SELECT 1 FROM inventory_source_status_transitions)
       OR EXISTS (SELECT 1 FROM durable_jobs)
       OR EXISTS (SELECT 1 FROM durable_job_events) THEN
        RAISE EXCEPTION 'cannot roll back inventory cycles and durable jobs while history exists';
    END IF;
END
$$;

DROP TABLE durable_job_events;
DROP TABLE durable_jobs;
DROP TABLE inventory_source_status_transitions;
DROP TABLE inventory_source_status;
DROP TABLE inventory_authoritative_cycle_events;
DROP TABLE inventory_authoritative_cycles;

COMMIT;
