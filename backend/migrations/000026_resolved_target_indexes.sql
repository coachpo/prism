-- Resolved-target partition indexes for three-scope observability (GOAL C6).
--
-- The three scopes need efficient final-execution and route-attempt lookups:
--   * request_logs.resolved_target_model_id for attempt grain and final latency
--   * usage_request_events.resolved_target_model_id for final_execution cost/model grouping
--   * request_logs.connection_id + resolved_target for terminal-target actual identity
--
-- Existing indexes cover profile_id+created_at and ingress chains, but not
-- resolved-target predicates. Add parent partitioned indexes so future
-- partitions inherit them automatically; existing children inherit in this
-- transaction. Additive only; no history rewrite.

CREATE INDEX IF NOT EXISTS idx_request_logs_resolved_target_created
    ON public.request_logs USING btree (profile_id, resolved_target_model_id, created_at, id)
    WHERE row_kind = 'upstream' AND resolved_target_model_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_request_logs_terminal_target_actual
    ON public.request_logs USING btree (profile_id, connection_id, resolved_target_model_id, created_at)
    WHERE row_kind = 'upstream' AND connection_id > 0;

CREATE INDEX IF NOT EXISTS idx_usage_request_events_resolved_target_created
    ON public.usage_request_events USING btree (profile_id, resolved_target_model_id, created_at, id)
    WHERE resolved_target_model_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_usage_request_events_terminal_target_final
    ON public.usage_request_events USING btree (profile_id, connection_id, resolved_target_model_id, created_at)
    WHERE connection_id > 0 AND final_attempt_number IS NOT NULL;
