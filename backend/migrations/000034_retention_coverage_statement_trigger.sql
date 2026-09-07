-- Mark the retention-coverage projection dirty once per INSERT statement
-- instead of once per inserted row.
--
-- `prism_mark_retention_coverage_dirty` upserts a single row per dataset, so
-- the row-level trigger made every appended retained row update that one
-- tuple. Within a transaction the work is proportional to the batch; across
-- transactions every concurrent writer queues on the same tuple lock, which
-- serializes otherwise independent inserts on the two highest-write tables.
-- A bulk load with four parallel writers spends most of its time with three
-- of them blocked on `transactionid`.
--
-- Only INSERT moves to statement level, and deliberately so. A statement-level
-- trigger with a transition table lives on the partitioned parent alone, and
-- retention deletes boundary rows straight from a child partition
-- (`DELETE FROM request_logs_pYYYYMMDD WHERE created_at < ...`), which such a
-- trigger would never see. UPDATE and DELETE therefore keep the row-level
-- trigger, which PostgreSQL clones onto every partition; both are rare enough
-- that per-row work costs nothing. Every append in this system is written
-- through the parent table, so the INSERT path loses no mutation.
--
-- The published semantics are unchanged:
--
-- * An INSERT against an owner that is not dirty, or that this transaction
--   already wrote, preserves the previous bounds and freshness. That is the
--   same-transaction append handoff `RecordActualCoverageAppend` relies on:
--   the owning writer, not the trigger, extends a complete projection.
-- * An INSERT against an owner another transaction left dirty extends the
--   bounds and forces `stale`, exactly as the row-level form did once its
--   first row had set the dirty bit.
-- * UPDATE and DELETE always extend the bounds and force `stale`.
-- * An INSERT statement that touched no rows is not a mutation and leaves the
--   projection alone, matching a row-level trigger that never fired.
--
-- One difference is deliberate: when no owner row exists yet, an append seeds
-- it with the statement's MIN/MAX rather than the first row's timestamp. The
-- seeded row is `precision='unavailable'`, `complete=false`, `dirty=true`, so
-- its bounds are never read as authoritative; the owner refresh recomputes
-- them either way.

CREATE OR REPLACE FUNCTION public.prism_mark_retention_coverage_dirty() RETURNS trigger
    LANGUAGE plpgsql AS $$
DECLARE
    dataset_name text := TG_ARGV[0];
    earliest_value timestamp with time zone;
    latest_value timestamp with time zone;
    appended_count bigint;
BEGIN
    IF TG_LEVEL = 'STATEMENT' THEN
        SELECT min(created_at), max(created_at), count(*)
          INTO earliest_value, latest_value, appended_count FROM appended_rows;
        IF appended_count = 0 THEN
            RETURN NULL;
        END IF;
    ELSIF TG_OP = 'DELETE' THEN
        earliest_value := OLD.created_at;
        latest_value := OLD.created_at;
    ELSE
        earliest_value := NEW.created_at;
        latest_value := NEW.created_at;
    END IF;

    INSERT INTO public.retention_coverage_read_models (
        dataset, earliest_retained_at, latest_retained_at, precision,
        complete, freshness, dirty, updated_at
    ) VALUES (
        dataset_name, earliest_value, latest_value, 'unavailable',
        false, 'stale', true, now()
    )
    ON CONFLICT (dataset) DO UPDATE SET
        earliest_retained_at = CASE
            WHEN TG_OP = 'INSERT'
              AND (NOT public.retention_coverage_read_models.dirty OR public.retention_coverage_read_models.xmin = pg_current_xact_id()::xid)
                THEN public.retention_coverage_read_models.earliest_retained_at
            WHEN EXCLUDED.earliest_retained_at IS NULL THEN public.retention_coverage_read_models.earliest_retained_at
            WHEN public.retention_coverage_read_models.earliest_retained_at IS NULL THEN EXCLUDED.earliest_retained_at
            ELSE LEAST(public.retention_coverage_read_models.earliest_retained_at, EXCLUDED.earliest_retained_at)
        END,
        latest_retained_at = CASE
            WHEN TG_OP = 'INSERT'
              AND (NOT public.retention_coverage_read_models.dirty OR public.retention_coverage_read_models.xmin = pg_current_xact_id()::xid)
                THEN public.retention_coverage_read_models.latest_retained_at
            WHEN EXCLUDED.latest_retained_at IS NULL THEN public.retention_coverage_read_models.latest_retained_at
            WHEN public.retention_coverage_read_models.latest_retained_at IS NULL THEN EXCLUDED.latest_retained_at
            ELSE GREATEST(public.retention_coverage_read_models.latest_retained_at, EXCLUDED.latest_retained_at)
        END,
        complete = public.retention_coverage_read_models.complete,
        freshness = CASE
            WHEN TG_OP = 'INSERT'
              AND (NOT public.retention_coverage_read_models.dirty OR public.retention_coverage_read_models.xmin = pg_current_xact_id()::xid)
                THEN public.retention_coverage_read_models.freshness
            ELSE 'stale'
        END,
        dirty = true,
        updated_at = now();
    RETURN NULL;
END;
$$;

DROP TRIGGER IF EXISTS trg_request_logs_retention_coverage_dirty ON public.request_logs;
DROP TRIGGER IF EXISTS trg_audit_logs_retention_coverage_dirty ON public.audit_logs;
DROP TRIGGER IF EXISTS trg_usage_request_events_retention_coverage_dirty ON public.usage_request_events;
DROP TRIGGER IF EXISTS trg_loadbalance_events_retention_coverage_dirty ON public.loadbalance_events;

CREATE TRIGGER trg_request_logs_retention_coverage_dirty_append
    AFTER INSERT ON public.request_logs
    REFERENCING NEW TABLE AS appended_rows
    FOR EACH STATEMENT EXECUTE FUNCTION public.prism_mark_retention_coverage_dirty('request_logs');
CREATE TRIGGER trg_request_logs_retention_coverage_dirty
    AFTER UPDATE OR DELETE ON public.request_logs
    FOR EACH ROW EXECUTE FUNCTION public.prism_mark_retention_coverage_dirty('request_logs');

CREATE TRIGGER trg_audit_logs_retention_coverage_dirty_append
    AFTER INSERT ON public.audit_logs
    REFERENCING NEW TABLE AS appended_rows
    FOR EACH STATEMENT EXECUTE FUNCTION public.prism_mark_retention_coverage_dirty('audit_logs');
CREATE TRIGGER trg_audit_logs_retention_coverage_dirty
    AFTER UPDATE OR DELETE ON public.audit_logs
    FOR EACH ROW EXECUTE FUNCTION public.prism_mark_retention_coverage_dirty('audit_logs');

CREATE TRIGGER trg_usage_request_events_retention_coverage_dirty_append
    AFTER INSERT ON public.usage_request_events
    REFERENCING NEW TABLE AS appended_rows
    FOR EACH STATEMENT EXECUTE FUNCTION public.prism_mark_retention_coverage_dirty('usage_request_events');
CREATE TRIGGER trg_usage_request_events_retention_coverage_dirty
    AFTER UPDATE OR DELETE ON public.usage_request_events
    FOR EACH ROW EXECUTE FUNCTION public.prism_mark_retention_coverage_dirty('usage_request_events');

CREATE TRIGGER trg_loadbalance_events_retention_coverage_dirty_append
    AFTER INSERT ON public.loadbalance_events
    REFERENCING NEW TABLE AS appended_rows
    FOR EACH STATEMENT EXECUTE FUNCTION public.prism_mark_retention_coverage_dirty('loadbalance_events');
CREATE TRIGGER trg_loadbalance_events_retention_coverage_dirty
    AFTER UPDATE OR DELETE ON public.loadbalance_events
    FOR EACH ROW EXECUTE FUNCTION public.prism_mark_retention_coverage_dirty('loadbalance_events');
