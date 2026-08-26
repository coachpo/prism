-- Ingress-chain read-path covering indexes (Requests SPEC §6.5/§10.2).
--
-- The ingress-chain view reads three shapes that the previous index set did
-- not cover end to end:
--
-- 1) The ordinary ingress-set groups request_logs per ingress and orders the
--    outer page by MIN(created_at); the finalized-summary join picks rows
--    per (profile_id, ingress_request_id). idx_request_logs_ingress_created_id
--    gives those lookups a single ordered range scan per ingress.
-- 2) The full-cohort retained totals aggregate over a profile window with
--    only row_kind / ingress_request_id consumers.
--    idx_request_logs_profile_created_totals turns that into an index-only
--    range scan and replaces idx_request_logs_profile_created_at (its key
--    prefix is identical, plus the id key keeps created_at DESC, id DESC page
--    ordering direct).
-- 3) Finalized summaries pick the newest usage event per ingress by id;
--    cohort EXISTS clauses probe (profile_id, ingress_request_id).
--    idx_usage_request_events_profile_ingress_id serves both and replaces
--    idx_usage_request_events_profile_ingress_request (same prefix without
--    the tiebreaker id).
--
-- ix_request_logs_status_code,
-- idx_usage_request_events_ingress_request_id, and
-- ix_usage_request_events_profile_id have no remaining consumer their
-- composite replacements do not cover; dropping them removes redundant write
-- amplification on the highest-write tables.
--
-- The migration runner owns a single transaction, so CONCURRENTLY is not
-- available here; parent indexes propagate to existing child partitions in
-- this transaction and to future partitions at creation time.

CREATE INDEX IF NOT EXISTS idx_request_logs_ingress_created_id
    ON public.request_logs USING btree (profile_id, ingress_request_id, created_at, id);

CREATE INDEX IF NOT EXISTS idx_request_logs_profile_created_totals
    ON public.request_logs USING btree (profile_id, created_at, id) INCLUDE (ingress_request_id, row_kind);

CREATE INDEX IF NOT EXISTS idx_usage_request_events_profile_ingress_id
    ON public.usage_request_events USING btree (profile_id, ingress_request_id, id);

DROP INDEX IF EXISTS public.idx_request_logs_profile_created_at;
DROP INDEX IF EXISTS public.ix_request_logs_status_code;
DROP INDEX IF EXISTS public.idx_usage_request_events_profile_ingress_request;
DROP INDEX IF EXISTS public.idx_usage_request_events_ingress_request_id;
DROP INDEX IF EXISTS public.ix_usage_request_events_profile_id;
