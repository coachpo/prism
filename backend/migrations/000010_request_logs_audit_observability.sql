-- Requests/Audit/Observability v2 migration (Requests SPEC §4.4/§5.2/§5.6,
-- Observe SPEC §3.5, Pricing SPEC §6.4).
--
-- Runs only after the Pricing 000008 additive -> 000009 finalize
-- sequence is complete and every v1 observability producer is stopped and
-- provably absent (offline writer transition). It creates:
--   * request_logs row-kind / scoped-status / attempt / failure diagnostics;
--   * usage_request_events finalized-ingress fields and attempt_count=0;
--   * audit_logs BYTEA bodies, scrubbed header targets, capture provenance,
--     and protected legacy raw shadows;
--   * runtime_telemetry_outbox v2 (metadata item with core/extension payloads
--     and section lifecycles) plus artifact items, staging, quarantine,
--     upgrade state, and the three-domain backfill state.
--
-- 000011 (a separate later release) validates the NOT VALID CHECKs, drops the
-- legacy raw shadows, and locks the v2 writer generation. This file must not
-- be applied while any v1 producer is live.

-- ---------------------------------------------------------------------------
-- 1. request_logs: row kind, scoped statuses, attempts, failure diagnostics
-- ---------------------------------------------------------------------------

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS row_kind character varying(24);

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS caller_request_id character varying(255);

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS url_scrub_provenance character varying(32);

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS metadata_redacted_fields text[] NOT NULL DEFAULT '{}';

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS metadata_truncated_fields text[] NOT NULL DEFAULT '{}';

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS attempt_trigger character varying(32);

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS attempt_result character varying(32);

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS is_winner boolean;

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS attempt_duration_ms integer;

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS legacy_duration_ms integer;

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS upstream_status_code integer;

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS gateway_status_code integer;

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS legacy_status_code integer;

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS error_source character varying(20);

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS error_code character varying(120);

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS failure_stage character varying(32);

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS error_detail_redacted boolean NOT NULL DEFAULT false;

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS error_detail_truncated boolean NOT NULL DEFAULT false;

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS stream_error_detail_redacted boolean NOT NULL DEFAULT false;

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS stream_error_detail_truncated boolean NOT NULL DEFAULT false;

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS upstream_request_started boolean;

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS response_headers_received boolean;

ALTER TABLE public.request_logs
    ADD COLUMN IF NOT EXISTS first_body_or_stream_event_seen boolean;

-- Legacy scoped-status migration: old NOT NULL status_code/response_time_ms
-- become nullable legacy projections; new writer never writes them.
ALTER TABLE public.request_logs
    ALTER COLUMN status_code DROP NOT NULL;

ALTER TABLE public.request_logs
    ALTER COLUMN response_time_ms DROP NOT NULL;

UPDATE public.request_logs
SET
    row_kind = 'legacy_unknown',
    url_scrub_provenance = 'legacy_unknown',
    legacy_status_code = status_code,
    legacy_duration_ms = response_time_ms,
    status_code = NULL,
    response_time_ms = NULL
WHERE row_kind IS NULL;

ALTER TABLE public.request_logs
    ALTER COLUMN row_kind SET NOT NULL;

ALTER TABLE public.request_logs
    ALTER COLUMN url_scrub_provenance SET NOT NULL;

-- ---------------------------------------------------------------------------
-- 2. request_logs NOT VALID CHECKs (validated by 000011)
-- ---------------------------------------------------------------------------

ALTER TABLE public.request_logs
    ADD CONSTRAINT ck_request_logs_row_kind
    CHECK (row_kind IN ('planning','admission','upstream','legacy_unknown'))
    NOT VALID;

ALTER TABLE public.request_logs
    ADD CONSTRAINT ck_request_logs_attempt_trigger
    CHECK (attempt_trigger IS NULL OR attempt_trigger IN (
        'initial','retry_same_target','hedge','failover'
    ))
    NOT VALID;

ALTER TABLE public.request_logs
    ADD CONSTRAINT ck_request_logs_attempt_result
    CHECK (attempt_result IS NULL OR attempt_result IN (
        'completed','http_error','stream_error','transport_error',
        'cancelled','client_disconnected','unknown'
    ))
    NOT VALID;

ALTER TABLE public.request_logs
    ADD CONSTRAINT ck_request_logs_error_source
    CHECK (error_source IS NULL OR error_source IN (
        'prism','upstream','transport','client','unknown'
    ))
    NOT VALID;

ALTER TABLE public.request_logs
    ADD CONSTRAINT ck_request_logs_failure_stage
    CHECK (failure_stage IS NULL OR failure_stage IN (
        'routing','admission','upstream_connect','upstream_response','stream','unknown'
    ))
    NOT VALID;

ALTER TABLE public.request_logs
    ADD CONSTRAINT ck_request_logs_error_code_grammar
    CHECK (error_code IS NULL OR (
        error_code ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,119}$'
    ))
    NOT VALID;

ALTER TABLE public.request_logs
    ADD CONSTRAINT ck_request_logs_attempt_fields_scope
    CHECK (
        row_kind = 'upstream'
        OR (attempt_trigger IS NULL AND attempt_result IS NULL AND is_winner IS NULL
            AND attempt_number IS NULL AND attempt_duration_ms IS NULL)
    )
    NOT VALID;

ALTER TABLE public.request_logs
    ADD CONSTRAINT ck_request_logs_metadata_arrays
    CHECK (
        cardinality(metadata_redacted_fields) <= 13
        AND cardinality(metadata_truncated_fields) <= 13
    )
    NOT VALID;

-- New failed rows must carry a stable code and scoped status. Legacy rows are
-- exempt via the legacy_unknown row kind.
ALTER TABLE public.request_logs
    ADD CONSTRAINT ck_request_logs_new_failed_rows
    CHECK (
        row_kind = 'legacy_unknown'
        OR (error_source IS NULL AND error_code IS NULL AND failure_stage IS NULL)
        OR (error_code IS NOT NULL AND error_code <> '')
    )
    NOT VALID;

-- ---------------------------------------------------------------------------
-- 3. usage_request_events: finalized-ingress contract (Observe SPEC §3.5)
-- ---------------------------------------------------------------------------

ALTER TABLE public.usage_request_events
    DROP CONSTRAINT IF EXISTS ck_usage_request_events_attempt_count_positive;

ALTER TABLE public.usage_request_events
    ADD CONSTRAINT ck_usage_request_events_attempt_count_nonneg
    CHECK (attempt_count >= 0)
    NOT VALID;

ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS expected_request_log_row_count integer;

ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS final_attempt_number integer;

ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS final_attempt_trigger character varying(32);

ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS final_target_entry_trigger character varying(32);

ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS same_target_retry_occurred boolean NOT NULL DEFAULT false;

ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS hedge_occurred boolean NOT NULL DEFAULT false;

ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS failover_occurred boolean NOT NULL DEFAULT false;

ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS routing_evidence_complete boolean;

ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS final_error_code character varying(120);

ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS ingress_started_at timestamp with time zone;

ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS ingress_completed_at timestamp with time zone;

ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS proxy_api_key_id_snapshot integer;

ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS proxy_api_key_attribution_state character varying(24) NOT NULL DEFAULT 'unknown';

ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS error_source character varying(20);

ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS error_code character varying(120);

ALTER TABLE public.usage_request_events
    ADD COLUMN IF NOT EXISTS failure_stage character varying(32);

ALTER TABLE public.usage_request_events
    ADD CONSTRAINT ck_usage_request_events_final_attempt_trigger
    CHECK (final_attempt_trigger IS NULL OR final_attempt_trigger IN (
        'initial','retry_same_target','hedge','failover'
    ))
    NOT VALID;

ALTER TABLE public.usage_request_events
    ADD CONSTRAINT ck_usage_request_events_final_target_entry_trigger
    CHECK (final_target_entry_trigger IS NULL OR final_target_entry_trigger IN (
        'initial','failover','hedge','unknown'
    ))
    NOT VALID;

ALTER TABLE public.usage_request_events
    ADD CONSTRAINT ck_usage_request_events_final_error_code_grammar
    CHECK (final_error_code IS NULL OR (
        final_error_code ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,119}$'
    ))
    NOT VALID;

ALTER TABLE public.usage_request_events
    ADD CONSTRAINT ck_usage_request_events_proxy_key_attribution
    CHECK (proxy_api_key_attribution_state IN ('identified','none','unknown'))
    NOT VALID;

-- Ingress wall-clock only from finalized usage evidence.
ALTER TABLE public.usage_request_events
    ADD CONSTRAINT ck_usage_request_events_ingress_time_pair
    CHECK (
        (ingress_started_at IS NULL AND ingress_completed_at IS NULL)
        OR (ingress_started_at IS NOT NULL AND ingress_completed_at IS NOT NULL
            AND ingress_completed_at >= ingress_started_at)
    )
    NOT VALID;

-- ---------------------------------------------------------------------------
-- 4. audit_logs: scoped statuses and legacy duration
-- ---------------------------------------------------------------------------

ALTER TABLE public.audit_logs
    ALTER COLUMN response_status DROP NOT NULL;

ALTER TABLE public.audit_logs
    ALTER COLUMN duration_ms DROP NOT NULL;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS attempt_number integer;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS row_kind character varying(24);

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS attempt_duration_ms integer;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS legacy_duration_ms integer;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS upstream_status_code integer;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS gateway_status_code integer;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS legacy_status_code integer;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS url_scrub_provenance character varying(32);

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_url_truncated boolean NOT NULL DEFAULT false;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS endpoint_base_url_truncated boolean NOT NULL DEFAULT false;

-- ---------------------------------------------------------------------------
-- 5. audit_logs: legacy header shadows + scrubbed JSONB targets
-- ---------------------------------------------------------------------------

-- The legacy TEXT header columns become protected raw shadows readable only
-- by the backfill owner. The migration never casts or parses raw TEXT into
-- the target columns; the Go scrub owner rewrites them.
ALTER TABLE public.audit_logs
    RENAME COLUMN request_headers TO request_headers_legacy_raw_text;

ALTER TABLE public.audit_logs
    RENAME COLUMN response_headers TO response_headers_legacy_raw_text;

ALTER TABLE public.audit_logs
    ALTER COLUMN request_headers_legacy_raw_text DROP NOT NULL;

ALTER TABLE public.audit_logs
    ALTER COLUMN response_headers_legacy_raw_text DROP NOT NULL;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_headers jsonb;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS response_headers jsonb;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_headers_scrub_provenance character varying(32);

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS response_headers_scrub_provenance character varying(32);

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_headers_capture_status character varying(32);

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS response_headers_capture_status character varying(32);

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_headers_capture_limit_reason character varying(24);

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS response_headers_capture_limit_reason character varying(24);

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_headers_truncated boolean NOT NULL DEFAULT false;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS response_headers_truncated boolean NOT NULL DEFAULT false;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_headers_entries_observed integer;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_headers_entries_stored integer;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS response_headers_entries_observed integer;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS response_headers_entries_stored integer;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_headers_bytes_observed bigint;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_headers_bytes_stored bigint;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS response_headers_bytes_observed bigint;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS response_headers_bytes_stored bigint;

-- Legacy audit rows: row-kind scope and URL scrub provenance are unknown;
-- header capture status reflects the protected raw shadow awaiting backfill.
UPDATE public.audit_logs
SET
    row_kind = 'legacy_unknown',
    url_scrub_provenance = 'legacy_unknown',
    request_headers_scrub_provenance = CASE
        WHEN request_headers_legacy_raw_text IS NOT NULL THEN 'legacy_unknown'
        ELSE 'not_applicable'
    END,
    response_headers_scrub_provenance = CASE
        WHEN response_headers_legacy_raw_text IS NOT NULL THEN 'legacy_unknown'
        ELSE 'not_applicable'
    END,
    request_headers_capture_status = CASE
        WHEN request_headers_legacy_raw_text IS NOT NULL THEN 'pending_headers'
        ELSE 'not_requested'
    END,
    response_headers_capture_status = CASE
        WHEN response_headers_legacy_raw_text IS NOT NULL THEN 'pending_headers'
        ELSE 'not_requested'
    END,
    request_headers_capture_limit_reason = 'none',
    response_headers_capture_limit_reason = 'none'
WHERE row_kind IS NULL;

ALTER TABLE public.audit_logs
    ALTER COLUMN row_kind SET NOT NULL;

ALTER TABLE public.audit_logs
    ALTER COLUMN url_scrub_provenance SET NOT NULL;

-- ---------------------------------------------------------------------------
-- 6. audit_logs: BYTEA body conversion with deterministic lossy provenance
-- ---------------------------------------------------------------------------

-- Preserve the legacy TEXT bodies under protected names during conversion.
ALTER TABLE public.audit_logs
    RENAME COLUMN request_body TO request_body_legacy_text;

ALTER TABLE public.audit_logs
    RENAME COLUMN response_body TO response_body_legacy_text;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_body bytea;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS response_body bytea;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_body_encoding character varying(16);

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS response_body_encoding character varying(16);

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_body_capture_provenance character varying(32);

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS response_body_capture_provenance character varying(32);

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_body_capture_end_state character varying(32);

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS response_body_capture_end_state character varying(32);

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_body_capture_status character varying(32);

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS response_body_capture_status character varying(32);

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_body_capture_limit_reason character varying(24);

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS response_body_capture_limit_reason character varying(24);

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_body_truncated boolean NOT NULL DEFAULT false;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS response_body_truncated boolean NOT NULL DEFAULT false;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_body_bytes_observed bigint;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS request_body_bytes_stored bigint;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS response_body_bytes_observed bigint;

ALTER TABLE public.audit_logs
    ADD COLUMN IF NOT EXISTS response_body_bytes_stored bigint;

-- Legacy TEXT -> BYTEA conversion (Requests SPEC §5.2): the stored bytes are
-- the exact UTF-8 representation of the old TEXT (convert_to with the
-- database encoding is byte-identical), capped at 4 MiB per body, with the
-- 12 MiB per-ingress request budget applied in immutable launch order for
-- rows that carry an internal ingress link. Rows without an ingress link use
-- a singleton scope (per-body 4 MiB cap only). Observed bytes are the full
-- pre-conversion octet length; end state is unknown for legacy evidence.
WITH budgeted AS (
    SELECT
        id,
        attempt_number,
        created_at,
        COALESCE(octet_length(request_body_legacy_text), 0) AS observed_request,
        LEAST(COALESCE(octet_length(request_body_legacy_text), 0), 4194304) AS capped_request,
        COALESCE(octet_length(response_body_legacy_text), 0) AS observed_response,
        LEAST(COALESCE(octet_length(response_body_legacy_text), 0), 4194304) AS capped_response,
        ingress_request_id
    FROM public.audit_logs
), cumulative_request AS (
    SELECT
        id,
        observed_request,
        capped_request,
        observed_response,
        capped_response,
        ingress_request_id,
        SUM(capped_request) OVER (
            PARTITION BY ingress_request_id
            ORDER BY attempt_number ASC NULLS LAST, created_at ASC, id ASC
            ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW
        ) AS cumulative_capped_request
    FROM budgeted
), allocated AS (
    SELECT
        id,
        observed_request,
        capped_request,
        observed_response,
        capped_response,
        ingress_request_id,
        GREATEST(0, LEAST(capped_request, 12582912 - (cumulative_capped_request - capped_request))) AS allocated_request
    FROM cumulative_request
)
UPDATE public.audit_logs a
SET
    request_body = CASE
        WHEN allocated.allocated_request > 0
            THEN SUBSTRING(convert_to(a.request_body_legacy_text, 'UTF8') FROM 1 FOR allocated.allocated_request::int)
        ELSE NULL
    END,
    request_body_bytes_observed = allocated.observed_request,
    request_body_bytes_stored = allocated.allocated_request,
    request_body_capture_provenance = CASE
        WHEN allocated.observed_request > 0 THEN 'legacy_text_transcoded'
        WHEN a.audit_enabled_at_request THEN 'legacy_unknown'
        ELSE 'not_applicable'
    END,
    request_body_capture_end_state = CASE
        WHEN allocated.observed_request > 0 THEN 'unknown'
        ELSE NULL
    END,
    request_body_capture_status = CASE
        WHEN allocated.observed_request = 0 THEN
            CASE WHEN a.audit_enabled_at_request THEN 'legacy_unknown' ELSE 'not_requested' END
        WHEN allocated.allocated_request = 0 THEN 'omitted_ingress_budget'
        WHEN allocated.allocated_request < allocated.observed_request THEN 'truncated'
        ELSE 'captured'
    END,
    request_body_capture_limit_reason = CASE
        WHEN allocated.observed_request = 0 THEN 'none'
        WHEN allocated.allocated_request = 0 THEN 'ingress_budget'
        WHEN allocated.allocated_request < allocated.observed_request
            AND allocated.observed_request > 4194304
            AND allocated.allocated_request >= 4194304 THEN 'body_cap'
        WHEN allocated.allocated_request < allocated.observed_request
            AND allocated.observed_request > 4194304 THEN 'both'
        WHEN allocated.allocated_request < allocated.observed_request THEN 'ingress_budget'
        ELSE 'none'
    END,
    request_body_truncated = (allocated.allocated_request > 0 AND allocated.allocated_request < allocated.observed_request),
    request_body_encoding = CASE
        WHEN allocated.observed_request > 0 THEN 'utf8'
        ELSE NULL
    END,
    response_body = CASE
        WHEN allocated.capped_response > 0
            THEN SUBSTRING(convert_to(a.response_body_legacy_text, 'UTF8') FROM 1 FOR allocated.capped_response::int)
        ELSE NULL
    END,
    response_body_bytes_observed = allocated.observed_response,
    response_body_bytes_stored = allocated.capped_response,
    response_body_capture_provenance = CASE
        WHEN allocated.observed_response > 0 THEN 'legacy_text_transcoded'
        WHEN a.audit_enabled_at_request THEN 'legacy_unknown'
        ELSE 'not_applicable'
    END,
    response_body_capture_end_state = CASE
        WHEN allocated.observed_response > 0 THEN 'unknown'
        ELSE NULL
    END,
    response_body_capture_status = CASE
        WHEN allocated.observed_response = 0 THEN
            CASE WHEN a.audit_enabled_at_request THEN 'legacy_unknown' ELSE 'not_requested' END
        WHEN allocated.capped_response < allocated.observed_response THEN 'truncated'
        ELSE 'captured'
    END,
    response_body_capture_limit_reason = CASE
        WHEN allocated.observed_response = 0 THEN 'none'
        WHEN allocated.capped_response < allocated.observed_response THEN 'body_cap'
        ELSE 'none'
    END,
    response_body_truncated = (allocated.capped_response > 0 AND allocated.capped_response < allocated.observed_response),
    response_body_encoding = CASE
        WHEN allocated.observed_response > 0 THEN 'utf8'
        ELSE NULL
    END,
    upstream_status_code = CASE
        WHEN a.response_status IS NOT NULL THEN a.response_status
        ELSE NULL
    END,
    legacy_status_code = a.response_status,
    attempt_duration_ms = CASE WHEN a.row_kind = 'upstream' THEN a.duration_ms ELSE NULL END,
    legacy_duration_ms = a.duration_ms,
    response_status = NULL,
    duration_ms = NULL
FROM allocated
WHERE allocated.id = a.id;

-- Drop the legacy TEXT body columns after their bytes were moved into BYTEA.
ALTER TABLE public.audit_logs
    DROP COLUMN IF EXISTS request_body_legacy_text;

ALTER TABLE public.audit_logs
    DROP COLUMN IF EXISTS response_body_legacy_text;

-- ---------------------------------------------------------------------------
-- 7. audit_logs NOT VALID CHECKs (validated by 000011)
-- ---------------------------------------------------------------------------

ALTER TABLE public.audit_logs
    ADD CONSTRAINT ck_audit_logs_row_kind
    CHECK (row_kind IN ('planning','admission','upstream','legacy_unknown'))
    NOT VALID;

ALTER TABLE public.audit_logs
    ADD CONSTRAINT ck_audit_logs_url_scrub_provenance
    CHECK (url_scrub_provenance IN (
        'runtime_scrubbed','legacy_rescrubbed','legacy_unknown','not_applicable'
    ))
    NOT VALID;

ALTER TABLE public.audit_logs
    ADD CONSTRAINT ck_audit_logs_headers_scrub_provenance
    CHECK (
        (request_headers_scrub_provenance IS NULL OR request_headers_scrub_provenance IN (
            'runtime_scrubbed','legacy_rescrubbed','legacy_all_values_redacted',
            'legacy_unknown','not_applicable'
        ))
        AND (response_headers_scrub_provenance IS NULL OR response_headers_scrub_provenance IN (
            'runtime_scrubbed','legacy_rescrubbed','legacy_all_values_redacted',
            'legacy_unknown','not_applicable'
        ))
    )
    NOT VALID;

ALTER TABLE public.audit_logs
    ADD CONSTRAINT ck_audit_logs_body_capture_provenance
    CHECK (
        (request_body_capture_provenance IS NULL OR request_body_capture_provenance IN (
            'runtime_bytes','legacy_text_transcoded','legacy_unknown','not_applicable'
        ))
        AND (response_body_capture_provenance IS NULL OR response_body_capture_provenance IN (
            'runtime_bytes','legacy_text_transcoded','legacy_unknown','not_applicable'
        ))
    )
    NOT VALID;

ALTER TABLE public.audit_logs
    ADD CONSTRAINT ck_audit_logs_request_body_capture_status
    CHECK (request_body_capture_status IS NULL OR request_body_capture_status IN (
        'not_requested','metadata_only','pending_body','captured','truncated',
        'omitted_ingress_budget','omitted_handoff_budget','materialization_failed',
        'legacy_unknown'
    ))
    NOT VALID;

ALTER TABLE public.audit_logs
    ADD CONSTRAINT ck_audit_logs_response_body_capture_status
    CHECK (response_body_capture_status IS NULL OR response_body_capture_status IN (
        'not_requested','metadata_only','pending_body','captured','truncated',
        'omitted_handoff_budget','materialization_failed','legacy_unknown'
    ))
    NOT VALID;

ALTER TABLE public.audit_logs
    ADD CONSTRAINT ck_audit_logs_headers_capture_status
    CHECK (
        (request_headers_capture_status IS NULL OR request_headers_capture_status IN (
            'not_requested','pending_headers','captured','truncated',
            'omitted_ingress_budget','omitted_handoff_budget','materialization_failed',
            'legacy_unknown'
        ))
        AND (response_headers_capture_status IS NULL OR response_headers_capture_status IN (
            'not_requested','pending_headers','captured','truncated',
            'omitted_ingress_budget','omitted_handoff_budget','materialization_failed',
            'legacy_unknown'
        ))
    )
    NOT VALID;

ALTER TABLE public.audit_logs
    ADD CONSTRAINT ck_audit_logs_body_capture_limit_reason
    CHECK (
        (request_body_capture_limit_reason IS NULL OR request_body_capture_limit_reason IN (
            'none','body_cap','ingress_budget','both','handoff_budget'
        ))
        AND (response_body_capture_limit_reason IS NULL OR response_body_capture_limit_reason IN (
            'none','body_cap','handoff_budget'
        ))
    )
    NOT VALID;

ALTER TABLE public.audit_logs
    ADD CONSTRAINT ck_audit_logs_headers_capture_limit_reason
    CHECK (
        (request_headers_capture_limit_reason IS NULL OR request_headers_capture_limit_reason IN (
            'none','block_cap','ingress_budget','both','handoff_budget'
        ))
        AND (response_headers_capture_limit_reason IS NULL OR response_headers_capture_limit_reason IN (
            'none','block_cap','ingress_budget','both','handoff_budget'
        ))
    )
    NOT VALID;

-- Header content/status closure: captured|truncated may carry a canonical
-- JSON array (an empty real block is `[]`); every other status requires the
-- JSONB target to be SQL null. Pre-scrub values never enter the target.
ALTER TABLE public.audit_logs
    ADD CONSTRAINT ck_audit_logs_request_headers_closure
    CHECK (
        (request_headers_capture_status IN ('captured','truncated')
         AND request_headers IS NOT NULL)
        OR (request_headers_capture_status NOT IN ('captured','truncated')
            AND request_headers IS NULL)
    )
    NOT VALID;

ALTER TABLE public.audit_logs
    ADD CONSTRAINT ck_audit_logs_response_headers_closure
    CHECK (
        (response_headers_capture_status IN ('captured','truncated')
         AND response_headers IS NOT NULL)
        OR (response_headers_capture_status NOT IN ('captured','truncated')
            AND response_headers IS NULL)
    )
    NOT VALID;

-- Body content/status closure: only captured|truncated with stored > 0 may
-- carry BYTEA; pending/omitted/failed/not-requested/metadata-only must be NULL.
ALTER TABLE public.audit_logs
    ADD CONSTRAINT ck_audit_logs_request_body_closure
    CHECK (
        (request_body_capture_status IN ('captured','truncated')
         AND request_body_bytes_stored > 0
         AND request_body IS NOT NULL)
        OR (request_body_capture_status NOT IN ('captured','truncated')
            AND request_body IS NULL)
    )
    NOT VALID;

ALTER TABLE public.audit_logs
    ADD CONSTRAINT ck_audit_logs_response_body_closure
    CHECK (
        (response_body_capture_status IN ('captured','truncated')
         AND response_body_bytes_stored > 0
         AND response_body IS NOT NULL)
        OR (response_body_capture_status NOT IN ('captured','truncated')
            AND response_body IS NULL)
    )
    NOT VALID;

-- stored <= observed; truncated iff 0 < stored < observed because of a cap.
ALTER TABLE public.audit_logs
    ADD CONSTRAINT ck_audit_logs_request_body_counts
    CHECK (
        (request_body_bytes_observed IS NULL AND request_body_bytes_stored IS NULL)
        OR (request_body_bytes_observed >= 0 AND request_body_bytes_stored >= 0
            AND request_body_bytes_stored <= request_body_bytes_observed)
    )
    NOT VALID;

ALTER TABLE public.audit_logs
    ADD CONSTRAINT ck_audit_logs_response_body_counts
    CHECK (
        (response_body_bytes_observed IS NULL AND response_body_bytes_stored IS NULL)
        OR (response_body_bytes_observed >= 0 AND response_body_bytes_stored >= 0
            AND response_body_bytes_stored <= response_body_bytes_observed)
    )
    NOT VALID;

ALTER TABLE public.audit_logs
    ADD CONSTRAINT ck_audit_logs_body_encoding
    CHECK (
        (request_body_encoding IS NULL OR request_body_encoding IN ('utf8','binary','unknown'))
        AND (response_body_encoding IS NULL OR response_body_encoding IN ('utf8','binary','unknown'))
    )
    NOT VALID;

ALTER TABLE public.audit_logs
    ADD CONSTRAINT ck_audit_logs_request_body_capture_end_state
    CHECK (request_body_capture_end_state IS NULL OR request_body_capture_end_state IN (
        'complete','client_disconnected','read_error','unknown'
    ))
    NOT VALID;

ALTER TABLE public.audit_logs
    ADD CONSTRAINT ck_audit_logs_response_body_capture_end_state
    CHECK (response_body_capture_end_state IS NULL OR response_body_capture_end_state IN (
        'complete','provider_incomplete','client_disconnected','read_error','unknown'
    ))
    NOT VALID;

-- ---------------------------------------------------------------------------
-- 8. runtime_telemetry_outbox v2: metadata item surface
-- ---------------------------------------------------------------------------

-- The single metadata item per ingress carries versioned, independently
-- validated core_payload and audit_extension_payload, section lifecycles,
-- a two-phase lifecycle for streaming ingresses, and per-item durable retry
-- fields. Existing v1 rows stay isolated under schema_version=1 until the
-- exclusive v1 drain owner transforms them.
ALTER TABLE public.runtime_telemetry_outbox
    ADD COLUMN IF NOT EXISTS schema_version integer NOT NULL DEFAULT 1;

ALTER TABLE public.runtime_telemetry_outbox
    ADD COLUMN IF NOT EXISTS lifecycle_state character varying(32) NOT NULL DEFAULT 'finalized';

ALTER TABLE public.runtime_telemetry_outbox
    ADD COLUMN IF NOT EXISTS owner_instance character varying(64);

ALTER TABLE public.runtime_telemetry_outbox
    ADD COLUMN IF NOT EXISTS owner_lease_expires_at timestamp with time zone;

ALTER TABLE public.runtime_telemetry_outbox
    ADD COLUMN IF NOT EXISTS item_version bigint NOT NULL DEFAULT 1;

ALTER TABLE public.runtime_telemetry_outbox
    ADD COLUMN IF NOT EXISTS core_payload jsonb;

ALTER TABLE public.runtime_telemetry_outbox
    ADD COLUMN IF NOT EXISTS audit_extension_payload jsonb;

ALTER TABLE public.runtime_telemetry_outbox
    ADD COLUMN IF NOT EXISTS core_state character varying(32) NOT NULL DEFAULT 'pending';

ALTER TABLE public.runtime_telemetry_outbox
    ADD COLUMN IF NOT EXISTS extension_state character varying(32) NOT NULL DEFAULT 'pending';

ALTER TABLE public.runtime_telemetry_outbox
    ADD COLUMN IF NOT EXISTS core_attempt_count integer NOT NULL DEFAULT 0;

ALTER TABLE public.runtime_telemetry_outbox
    ADD COLUMN IF NOT EXISTS core_next_attempt_at timestamp with time zone NOT NULL DEFAULT now();

ALTER TABLE public.runtime_telemetry_outbox
    ADD COLUMN IF NOT EXISTS core_last_safe_error_code character varying(120);

ALTER TABLE public.runtime_telemetry_outbox
    ADD COLUMN IF NOT EXISTS extension_attempt_count integer NOT NULL DEFAULT 0;

ALTER TABLE public.runtime_telemetry_outbox
    ADD COLUMN IF NOT EXISTS extension_next_attempt_at timestamp with time zone NOT NULL DEFAULT now();

ALTER TABLE public.runtime_telemetry_outbox
    ADD COLUMN IF NOT EXISTS extension_last_safe_error_code character varying(120);

ALTER TABLE public.runtime_telemetry_outbox
    ADD COLUMN IF NOT EXISTS core_materialized_at timestamp with time zone;

ALTER TABLE public.runtime_telemetry_outbox
    ADD CONSTRAINT ck_runtime_telemetry_outbox_schema_version
    CHECK (schema_version IN (1, 2))
    NOT VALID;

ALTER TABLE public.runtime_telemetry_outbox
    ADD CONSTRAINT ck_runtime_telemetry_outbox_lifecycle
    CHECK (lifecycle_state IN (
        'provisional_stream','finalized','telemetry_orphaned','core_materialization_failed'
    ))
    NOT VALID;

ALTER TABLE public.runtime_telemetry_outbox
    ADD CONSTRAINT ck_runtime_telemetry_outbox_core_state
    CHECK (core_state IN ('pending','materialized','failed'))
    NOT VALID;

ALTER TABLE public.runtime_telemetry_outbox
    ADD CONSTRAINT ck_runtime_telemetry_outbox_extension_state
    CHECK (extension_state IN ('pending','materialized','quarantined','omitted'))
    NOT VALID;

ALTER TABLE public.runtime_telemetry_outbox
    ADD CONSTRAINT ck_runtime_telemetry_outbox_attempt_counts
    CHECK (core_attempt_count >= 0 AND extension_attempt_count >= 0)
    NOT VALID;

-- The unique metadata identity: exactly one metadata item per ingress per
-- profile. No bare INSERT may create a second metadata row.
CREATE UNIQUE INDEX IF NOT EXISTS uq_runtime_telemetry_outbox_metadata_identity
    ON public.runtime_telemetry_outbox (profile_id, ingress_request_id)
    WHERE schema_version = 2;

-- ---------------------------------------------------------------------------
-- 9. runtime_telemetry_artifacts: bounded body/header items
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.runtime_telemetry_artifacts (
    id bigint NOT NULL GENERATED ALWAYS AS IDENTITY,
    profile_id integer NOT NULL,
    ingress_request_id character varying(36) NOT NULL,
    component_key character varying(64) NOT NULL,
    artifact_kind character varying(32) NOT NULL,
    opaque_item_id character varying(64) NOT NULL,
    schema_version integer NOT NULL DEFAULT 2,
    lifecycle_state character varying(32) NOT NULL DEFAULT 'finalized',
    owner_instance character varying(64),
    owner_lease_expires_at timestamp with time zone,
    item_version bigint NOT NULL DEFAULT 1,
    payload jsonb NOT NULL,
    capture_status character varying(32),
    capture_limit_reason character varying(24),
    observed_bytes bigint,
    stored_bytes bigint,
    encoding character varying(16),
    capture_end_state character varying(32),
    truncated boolean NOT NULL DEFAULT false,
    audit_component_created_at timestamp with time zone,
    audit_retention_generation bigint,
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamp with time zone NOT NULL DEFAULT now(),
    last_safe_error_code character varying(120),
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT uq_runtime_telemetry_artifacts_identity
        UNIQUE (profile_id, ingress_request_id, component_key, artifact_kind),
    CONSTRAINT uq_runtime_telemetry_artifacts_opaque
        UNIQUE (opaque_item_id),
    CONSTRAINT ck_runtime_telemetry_artifacts_kind
        CHECK (artifact_kind IN ('request_body','response_body','headers')),
    CONSTRAINT ck_runtime_telemetry_artifacts_lifecycle
        CHECK (lifecycle_state IN (
            'provisional_stream','finalized','telemetry_orphaned','materialization_failed'
        )),
    CONSTRAINT ck_runtime_telemetry_artifacts_attempt_count
        CHECK (attempt_count >= 0)
);

-- ---------------------------------------------------------------------------
-- 10. audit_artifact_staging: materializer merge staging
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.audit_artifact_staging (
    id bigint NOT NULL GENERATED ALWAYS AS IDENTITY,
    profile_id integer NOT NULL,
    ingress_request_id character varying(36) NOT NULL,
    request_log_id bigint,
    component_key character varying(64) NOT NULL,
    artifact_kind character varying(32) NOT NULL,
    state character varying(32) NOT NULL DEFAULT 'pending_metadata',
    payload jsonb NOT NULL,
    attempt_count integer NOT NULL DEFAULT 0,
    next_attempt_at timestamp with time zone NOT NULL DEFAULT now(),
    last_safe_error_code character varying(120),
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT uq_audit_artifact_staging_identity
        UNIQUE (profile_id, ingress_request_id, component_key, artifact_kind),
    CONSTRAINT ck_audit_artifact_staging_state
        CHECK (state IN ('pending_metadata','ready_to_merge','materialization_failed','tombstoned')),
    CONSTRAINT ck_audit_artifact_staging_kind
        CHECK (artifact_kind IN ('request_body','response_body','headers')),
    CONSTRAINT ck_audit_artifact_staging_attempt_count
        CHECK (attempt_count >= 0)
);

-- ---------------------------------------------------------------------------
-- 11. runtime_telemetry_quarantine: bounded extension quarantine
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.runtime_telemetry_quarantine (
    id bigint NOT NULL GENERATED ALWAYS AS IDENTITY,
    profile_id integer NOT NULL,
    ingress_request_id character varying(36) NOT NULL,
    schema_version integer NOT NULL DEFAULT 2,
    extension_payload jsonb NOT NULL,
    schema_error_code character varying(120),
    retention_generation bigint,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT ck_runtime_telemetry_quarantine_version
        CHECK (schema_version = 2)
);

-- ---------------------------------------------------------------------------
-- 12. observability_v2_upgrade_state
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.observability_v2_upgrade_state (
    id integer NOT NULL DEFAULT 1,
    state character varying(32) NOT NULL,
    v1_finalized_outbox_count bigint NOT NULL DEFAULT 0,
    v1_accepted_outbox_count bigint NOT NULL DEFAULT 0,
    old_owner_lease_count bigint NOT NULL DEFAULT 0,
    old_writer_generation_count bigint NOT NULL DEFAULT 0,
    writer_generation bigint NOT NULL DEFAULT 0,
    writer_fence_active boolean NOT NULL DEFAULT false,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_observability_v2_upgrade_state PRIMARY KEY (id),
    CONSTRAINT observability_v2_upgrade_state_singleton
        CHECK (id = 1),
    CONSTRAINT ck_observability_v2_upgrade_state
        CHECK (state IN (
            'collecting_v1_inventory','draining_v1','v1_drained',
            'backfill_in_progress','backfill_ready','final'
        ))
);

-- Fresh installs have zero legacy rows, so 000010 synchronously proves the
-- 000011 readiness preconditions and may continue in the same batch. Upgrades
-- with any retained observability rows enter draining_v1 with the writer
-- fence inactive; 000011 must then ship in a later release after the offline
-- v1 drain and three-domain backfill owners finish.
DO $$
DECLARE
    legacy_rows bigint;
BEGIN
    INSERT INTO public.observability_v2_upgrade_state (id, state, updated_at)
    VALUES (1, 'draining_v1', now())
    ON CONFLICT (id) DO NOTHING;

    SELECT (
        (SELECT COUNT(*) FROM public.request_logs)
        + (SELECT COUNT(*) FROM public.usage_request_events)
        + (SELECT COUNT(*) FROM public.audit_logs)
        + (SELECT COUNT(*) FROM public.runtime_telemetry_outbox)
    ) INTO legacy_rows;

    IF legacy_rows = 0 THEN
        UPDATE public.observability_v2_upgrade_state
        SET
            state = 'backfill_ready',
            writer_generation = 1,
            writer_fence_active = true,
            updated_at = now()
        WHERE id = 1;
    END IF;
END $$;

-- ---------------------------------------------------------------------------
-- 13. observability_v2_backfill_state: three-domain readiness gate
-- ---------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.observability_v2_backfill_state (
    profile_id integer NOT NULL,
    domain character varying(32) NOT NULL,
    status character varying(32) NOT NULL,
    last_created_at timestamp with time zone,
    last_id bigint,
    updated_at timestamp with time zone NOT NULL,
    last_safe_error_code character varying(120),
    PRIMARY KEY (profile_id, domain),
    CONSTRAINT ck_observability_v2_backfill_state_domain
        CHECK (domain IN ('request_urls','request_metadata','audit_headers_urls')),
    CONSTRAINT ck_observability_v2_backfill_state_status
        CHECK (status IN ('pending','running','ready','failed','unavailable'))
);

-- ---------------------------------------------------------------------------
-- 14. Chain lookup index (Requests SPEC §10.2)
-- ---------------------------------------------------------------------------

-- The parent declaration alone is insufficient; existing children must
-- inherit the index. The runner's single transaction creates the parent
-- index; future children inherit from the partitioned parent template.
CREATE INDEX IF NOT EXISTS idx_request_logs_ingress_chain
    ON public.request_logs (profile_id, ingress_request_id, attempt_number, created_at, id);
