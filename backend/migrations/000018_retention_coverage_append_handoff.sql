-- 000018_retention_coverage_append_handoff
-- Runtime telemetry inserts a retained fact and then advances the matching
-- coverage owner in the same transaction. The original dirty trigger marked
-- every mutation stale, so RecordActualCoverageAppend could not distinguish
-- that valid append handoff from an owner that was already stale.
--
-- Preserve prior bounds and freshness only for a clean INSERT owner or
-- subsequent INSERTs from its transaction; a foreign dirty xid stays stale.
-- The same-transaction append handoff applies the retention floor before extending trusted bounds.
-- RecordActualCoverageAppend still requires a complete, fresh, source-matched
-- owner before it clears the trigger's dirty bit. UPDATE and DELETE always
-- force stale, and a prior stale owner stays stale until owner refresh runs.

CREATE OR REPLACE FUNCTION public.prism_mark_retention_coverage_dirty() RETURNS trigger
    LANGUAGE plpgsql AS $$
DECLARE
    dataset_name text := TG_ARGV[0];
    created_at_value timestamp with time zone;
BEGIN
    IF TG_OP = 'DELETE' THEN
        created_at_value := OLD.created_at;
    ELSE
        created_at_value := NEW.created_at;
    END IF;

    INSERT INTO public.retention_coverage_read_models (
        dataset, earliest_retained_at, latest_retained_at, precision,
        complete, freshness, dirty, updated_at
    ) VALUES (
        dataset_name, created_at_value, created_at_value, 'unavailable',
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
