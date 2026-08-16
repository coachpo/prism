-- Requests/Audit/Observability v2 finalize migration (Requests SPEC §5.6).
--
-- This migration ships in a LATER release than 000010. It MUST fail closed
-- when any precondition is unmet and MUST NOT be recorded as applied:
--   * v1 outbox drain is not complete (v1 rows/leases/writer generation);
--   * any backfill domain is not `ready`;
--   * any raw legacy shadow still contains bytes;
--   * the v2 writer generation is not locked.
--
-- When the runner would otherwise auto-apply pending migrations back to back,
-- this file must be withheld from the 000010 release package; upgraded
-- instances run it only after the offline drain/backfill owners finish.

DO $$
DECLARE
    v1_rows bigint;
    v1_accepted bigint;
    backfill_unready bigint;
    raw_shadow_rows bigint;
    upgrade_state_value text;
    fence_active boolean;
    writer_generation_value bigint;
BEGIN
    SELECT COALESCE(SUM(
        CASE WHEN schema_version = 1 THEN 1 ELSE 0 END
    ), 0)
    INTO v1_rows
    FROM public.runtime_telemetry_outbox;

    SELECT COALESCE(SUM(
        CASE
            WHEN schema_version = 1
             AND payload->>'handoff_phase' = 'stream_accepted' THEN 1
            ELSE 0
        END
    ), 0)
    INTO v1_accepted
    FROM public.runtime_telemetry_outbox;

    SELECT COUNT(*)
    INTO backfill_unready
    FROM public.observability_v2_backfill_state
    WHERE status <> 'ready';

    SELECT COUNT(*)
    INTO raw_shadow_rows
    FROM public.audit_logs
    WHERE request_headers_legacy_raw_text IS NOT NULL
       OR response_headers_legacy_raw_text IS NOT NULL;

    SELECT state, writer_fence_active, writer_generation
    INTO upgrade_state_value, fence_active, writer_generation_value
    FROM public.observability_v2_upgrade_state
    WHERE id = 1;

    IF v1_rows > 0 OR v1_accepted > 0 THEN
        RAISE EXCEPTION 'observability v2 finalize blocked: v1 telemetry outbox not drained (rows=%, accepted=%)',
            v1_rows, v1_accepted;
    END IF;
    IF backfill_unready > 0 THEN
        RAISE EXCEPTION 'observability v2 finalize blocked: % backfill domain(s) not ready', backfill_unready;
    END IF;
    IF raw_shadow_rows > 0 THEN
        RAISE EXCEPTION 'observability v2 finalize blocked: % audit row(s) still carry raw header shadows', raw_shadow_rows;
    END IF;
    IF upgrade_state_value IS NULL OR upgrade_state_value NOT IN ('v1_drained','backfill_ready','final') THEN
        RAISE EXCEPTION 'observability v2 finalize blocked: upgrade state is %', COALESCE(upgrade_state_value, 'unset');
    END IF;
    IF NOT fence_active THEN
        RAISE EXCEPTION 'observability v2 finalize blocked: v2 writer fence is not active';
    END IF;
END $$;

-- ---------------------------------------------------------------------------
-- Validate the NOT VALID CHECKs installed by 000010 (legacy rows are now
-- either converted or exempted, so validation must succeed).
-- ---------------------------------------------------------------------------

ALTER TABLE public.request_logs
    VALIDATE CONSTRAINT ck_request_logs_row_kind;

ALTER TABLE public.request_logs
    VALIDATE CONSTRAINT ck_request_logs_attempt_trigger;

ALTER TABLE public.request_logs
    VALIDATE CONSTRAINT ck_request_logs_attempt_result;

ALTER TABLE public.request_logs
    VALIDATE CONSTRAINT ck_request_logs_error_source;

ALTER TABLE public.request_logs
    VALIDATE CONSTRAINT ck_request_logs_failure_stage;

ALTER TABLE public.request_logs
    VALIDATE CONSTRAINT ck_request_logs_error_code_grammar;

ALTER TABLE public.request_logs
    VALIDATE CONSTRAINT ck_request_logs_attempt_fields_scope;

ALTER TABLE public.request_logs
    VALIDATE CONSTRAINT ck_request_logs_metadata_arrays;

ALTER TABLE public.request_logs
    VALIDATE CONSTRAINT ck_request_logs_new_failed_rows;

ALTER TABLE public.usage_request_events
    VALIDATE CONSTRAINT ck_usage_request_events_attempt_count_nonneg;

ALTER TABLE public.usage_request_events
    VALIDATE CONSTRAINT ck_usage_request_events_final_attempt_trigger;

ALTER TABLE public.usage_request_events
    VALIDATE CONSTRAINT ck_usage_request_events_final_target_entry_trigger;

ALTER TABLE public.usage_request_events
    VALIDATE CONSTRAINT ck_usage_request_events_final_error_code_grammar;

ALTER TABLE public.usage_request_events
    VALIDATE CONSTRAINT ck_usage_request_events_proxy_key_attribution;

ALTER TABLE public.usage_request_events
    VALIDATE CONSTRAINT ck_usage_request_events_ingress_time_pair;

ALTER TABLE public.audit_logs
    VALIDATE CONSTRAINT ck_audit_logs_row_kind;

ALTER TABLE public.audit_logs
    VALIDATE CONSTRAINT ck_audit_logs_url_scrub_provenance;

ALTER TABLE public.audit_logs
    VALIDATE CONSTRAINT ck_audit_logs_headers_scrub_provenance;

ALTER TABLE public.audit_logs
    VALIDATE CONSTRAINT ck_audit_logs_body_capture_provenance;

ALTER TABLE public.audit_logs
    VALIDATE CONSTRAINT ck_audit_logs_request_body_capture_status;

ALTER TABLE public.audit_logs
    VALIDATE CONSTRAINT ck_audit_logs_response_body_capture_status;

ALTER TABLE public.audit_logs
    VALIDATE CONSTRAINT ck_audit_logs_headers_capture_status;

ALTER TABLE public.audit_logs
    VALIDATE CONSTRAINT ck_audit_logs_body_capture_limit_reason;

ALTER TABLE public.audit_logs
    VALIDATE CONSTRAINT ck_audit_logs_headers_capture_limit_reason;

ALTER TABLE public.audit_logs
    VALIDATE CONSTRAINT ck_audit_logs_request_headers_closure;

ALTER TABLE public.audit_logs
    VALIDATE CONSTRAINT ck_audit_logs_response_headers_closure;

ALTER TABLE public.audit_logs
    VALIDATE CONSTRAINT ck_audit_logs_request_body_closure;

ALTER TABLE public.audit_logs
    VALIDATE CONSTRAINT ck_audit_logs_response_body_closure;

ALTER TABLE public.audit_logs
    VALIDATE CONSTRAINT ck_audit_logs_request_body_counts;

ALTER TABLE public.audit_logs
    VALIDATE CONSTRAINT ck_audit_logs_response_body_counts;

ALTER TABLE public.audit_logs
    VALIDATE CONSTRAINT ck_audit_logs_body_encoding;

ALTER TABLE public.audit_logs
    VALIDATE CONSTRAINT ck_audit_logs_request_body_capture_end_state;

ALTER TABLE public.audit_logs
    VALIDATE CONSTRAINT ck_audit_logs_response_body_capture_end_state;

ALTER TABLE public.runtime_telemetry_outbox
    VALIDATE CONSTRAINT ck_runtime_telemetry_outbox_schema_version;

ALTER TABLE public.runtime_telemetry_outbox
    VALIDATE CONSTRAINT ck_runtime_telemetry_outbox_lifecycle;

ALTER TABLE public.runtime_telemetry_outbox
    VALIDATE CONSTRAINT ck_runtime_telemetry_outbox_core_state;

ALTER TABLE public.runtime_telemetry_outbox
    VALIDATE CONSTRAINT ck_runtime_telemetry_outbox_extension_state;

ALTER TABLE public.runtime_telemetry_outbox
    VALIDATE CONSTRAINT ck_runtime_telemetry_outbox_attempt_counts;

-- ---------------------------------------------------------------------------
-- Drop the protected legacy raw shadows (backfill has nulled them).
-- ---------------------------------------------------------------------------

ALTER TABLE public.audit_logs
    DROP COLUMN IF EXISTS request_headers_legacy_raw_text;

ALTER TABLE public.audit_logs
    DROP COLUMN IF EXISTS response_headers_legacy_raw_text;

-- ---------------------------------------------------------------------------
-- Lock the v2 writer generation and mark the upgrade final.
-- ---------------------------------------------------------------------------

UPDATE public.observability_v2_upgrade_state
SET
    state = 'final',
    v1_finalized_outbox_count = 0,
    v1_accepted_outbox_count = 0,
    old_owner_lease_count = 0,
    old_writer_generation_count = 0,
    writer_fence_active = true,
    updated_at = now()
WHERE id = 1;

-- Future v2 writers must carry writer_generation = 1; the fence stays active.
