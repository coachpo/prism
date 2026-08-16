-- Stats read-path index fixes.
--
-- 1) checkpointPage's real access shape is (job_id equality, id ordered);
--    (job_id, created_at, id) cannot feed ORDER BY id directly, so the
--    planner falls back to a primary-key scan on large jobs.
CREATE INDEX IF NOT EXISTS idx_management_job_events_job_id_seq
    ON public.management_job_events USING btree (job_id, id);

-- 2) Drop the two single-column indexes fully covered by the composite
--    (profile_id, ...) index prefixes, reducing request_logs write
--    amplification. ix_request_logs_id must stay: the primary key is
--    (created_at, id), while request detail reads go
--    `WHERE profile_id = $1 AND id = $2`, which would degrade to a full
--    partition scan without an id-first index.
DROP INDEX IF EXISTS public.ix_request_logs_created_at;
DROP INDEX IF EXISTS public.ix_request_logs_profile_id;
