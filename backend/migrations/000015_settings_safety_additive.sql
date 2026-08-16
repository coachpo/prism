-- 000015 settings safety/coherence: additive schema (Settings SPEC §14.1/§14.2).
--
-- Logical slot UXM-007 (physical version frozen by the repo migration manifest;
-- the next free slot after 000014). UXM-008 is an explicit-finalizer history
-- marker and NEVER ships as a pending SQL file.
--
-- This migration is strictly additive:
--   * the four log_retention_settings policy values are preserved byte-for-byte
--     (NULL stays NULL, explicit values stay explicit; the research instance's
--     four `1` values are never rewritten);
--   * no cleanup job is created, nothing is deleted, no effective auth
--     credential/session changes and no audit evidence is touched;
--   * legacy >36500 values survive: the new 1..36500 CHECKs are added NOT VALID
--     after dropping the weaker `>= 1` checks and are only validated by the
--     explicit finalizer after every repair issue is resolved.
--
-- Contract highlights installed here:
--   1. log_retention_settings.revision (monotonic CAS) + 1..36500 NOT VALID
--      CHECKs (finalization validates them).
--   2. log_retention_policy_resources: one row per fixed dataset carrying
--      policy_generation, a separate semantic fence_generation, settings
--      revision, configured UTC day-aligned logical cutoff, published
--      retention floor projection and reclaim state.
--   3. log_retention_preflights: single-use destructive preflight records
--      (token hash at rest, exact affected-domain owner snapshots).
--   4. management_jobs v2 extension for log_retention: contract_version,
--      operation_id/request_hash (mandatory for v2), origin, purge_to_time,
--      purge_state, stage, terminal_disposition, legacy provenance fields,
--      boundary_rows_deleted (legacy rows_deleted backfill, never a total),
--      dropped-partition accounting, plus the v2 state machine CHECKs and a
--      partial unique index for one manual reservation per dataset.
--   5. legacy_retention_job_classification_evidence: append-only conservative
--      classification of every existing log-retention job; management_jobs
--      origin/provenance backfilled from the same immutable evidence. Only a
--      proven scheduler sentinel + proven log-retention shape + proven
--      scheduler creation may classify automatic; anything else drains as
--      manual or blocks as repair_required.
--   6. retention_worker_transition_state + worker_generation + DB triggers:
--      the legacy 5-second scheduler create/claim/delete path is fenced for
--      any worker below the minimum generation. Only the frozen generation-
--      tagged v1 drain executor (explicitly authorized) may finish previously
--      accepted legacy rows; old binaries without worker evidence fail.
--   7. settings_mutation_operations: durable operation/request identity for
--      response-loss recovery of retention/audit/auth/cancel intents.
--   8. settings_migration_evidence + settings_owner_drift_inventory:
--      generation-1 current heads for the three duplicated legacy
--      user_settings retention columns vs the instance-owned
--      log_retention_settings values (equal => converged, different => drift).
--   9. settings_schema_transition: singleton phase/finalizer-lease state for
--      the explicit finalizer (no process-liveness, no second admission
--      generation, no backend_generation_liveness table).
--  10. profile_audit_settings_state (group revision/CAS) +
--      profile_api_family_audit_settings.migration_provenance NOT NULL with
--      three-value immutable backfill (explicit / legacy_existing /
--      legacy_missing_projected_disabled).
--  11. auth_config_versions + app_auth_settings desired/effective pointers:
--      one immutable generation copied from the current mode/credential/
--      session version; every current session keeps working; no in-place
--      credential mutation authority remains for the target writers.
--  12. audit_storage_daily_facts + audit_storage_fact_state: bounded daily
--      logical-storage facts with one current-generation pointer.
--  13. retention_coverage_read_models: owner-maintained actual-coverage
--      bounds; Settings never derives coverage from policy days or a live
--      aggregate query.
--
-- Routing (is_default / partial unique / conflict inventory) is already final
-- from 000007_routing_policy_strategy_defaults_and_event_identity and is not
-- repeated here. Audit bytea budgets (4 MiB / 12+4 MiB) are already installed
-- by 000007_audit_bytea_budgets and are not repeated.

-- ============================================================================
-- Part 1: log_retention_settings revision + 1..36500 NOT VALID CHECKs
-- ============================================================================

ALTER TABLE public.log_retention_settings
    ADD COLUMN revision bigint NOT NULL DEFAULT 1;

-- Replace the weaker legacy CHECKs (`>= 1`) with the target 1..36500 range.
-- NOT VALID keeps any legacy value above 36500 readable and repairable; the
-- explicit finalizer validates these constraints only after every repair
-- issue is resolved (Settings SPEC §5.1/§14.2).
ALTER TABLE public.log_retention_settings
    DROP CONSTRAINT log_retention_settings_audit_logs_retention_days_check,
    DROP CONSTRAINT log_retention_settings_loadbalance_events_retention_days_check,
    DROP CONSTRAINT log_retention_settings_request_logs_retention_days_check,
    DROP CONSTRAINT log_retention_settings_statistics_retention_days_check;

ALTER TABLE public.log_retention_settings
    ADD CONSTRAINT log_retention_settings_audit_logs_retention_days_check
        CHECK (audit_logs_retention_days IS NULL OR (audit_logs_retention_days >= 1 AND audit_logs_retention_days <= 36500)) NOT VALID,
    ADD CONSTRAINT log_retention_settings_loadbalance_events_retention_days_check
        CHECK (loadbalance_events_retention_days IS NULL OR (loadbalance_events_retention_days >= 1 AND loadbalance_events_retention_days <= 36500)) NOT VALID,
    ADD CONSTRAINT log_retention_settings_request_logs_retention_days_check
        CHECK (request_logs_retention_days IS NULL OR (request_logs_retention_days >= 1 AND request_logs_retention_days <= 36500)) NOT VALID,
    ADD CONSTRAINT log_retention_settings_statistics_retention_days_check
        CHECK (statistics_retention_days IS NULL OR (statistics_retention_days >= 1 AND statistics_retention_days <= 36500)) NOT VALID;

-- ============================================================================
-- Part 2: log_retention_policy_resources (per-dataset policy/generation state)
-- ============================================================================

CREATE TABLE public.log_retention_policy_resources (
    dataset text NOT NULL,
    policy_generation bigint NOT NULL,
    fence_generation bigint NOT NULL DEFAULT 1,
    settings_revision bigint NOT NULL,
    configured_logical_cutoff timestamp with time zone,
    published_retention_floor timestamp with time zone,
    retention_revocation_epoch bigint NOT NULL DEFAULT 0,
    purge_state text NOT NULL DEFAULT 'idle',
    physical_reclaim_state text NOT NULL DEFAULT 'idle',
    desired_work_identity text,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_log_retention_policy_resources PRIMARY KEY (dataset),
    CONSTRAINT ck_log_retention_policy_resources_dataset
        CHECK (dataset IN ('request_logs','audit_logs','usage_request_events','loadbalance_events')),
    CONSTRAINT ck_log_retention_policy_resources_generation CHECK (policy_generation >= 1),
    CONSTRAINT ck_log_retention_policy_resources_fence_generation CHECK (fence_generation >= 1),
    CONSTRAINT ck_log_retention_policy_resources_revocation_epoch CHECK (retention_revocation_epoch >= 0),
    CONSTRAINT ck_log_retention_policy_resources_purge_state
        CHECK (purge_state IN ('idle','running','recovery_required','published','rolled_back')),
    CONSTRAINT ck_log_retention_policy_resources_reclaim_state
        CHECK (physical_reclaim_state IN ('idle','waiting','running','done'))
);

-- Deterministic generation-1 baseline; no jobs are created by this migration.
INSERT INTO public.log_retention_policy_resources (
    dataset, policy_generation, fence_generation, settings_revision, configured_logical_cutoff,
    published_retention_floor, retention_revocation_epoch, purge_state,
    physical_reclaim_state, desired_work_identity, updated_at
)
SELECT
    candidate.dataset,
    1,
    1,
    1,
    CASE
        WHEN policy_value.days IS NULL THEN NULL
        ELSE (
            date_trunc('day', now() AT TIME ZONE 'UTC')
            - (policy_value.days * interval '1 day')
        ) AT TIME ZONE 'UTC'
    END,
    NULL,
    0,
    'idle',
    'idle',
    NULL,
    now()
FROM (VALUES
    ('request_logs'),
    ('audit_logs'),
    ('usage_request_events'),
    ('loadbalance_events')
) AS candidate(dataset)
LEFT JOIN LATERAL (
    SELECT CASE
        WHEN candidate.dataset = 'request_logs' THEN lrs.request_logs_retention_days
        WHEN candidate.dataset = 'audit_logs' THEN lrs.audit_logs_retention_days
        WHEN candidate.dataset = 'usage_request_events' THEN lrs.statistics_retention_days
        ELSE lrs.loadbalance_events_retention_days
    END AS days
    FROM public.log_retention_settings AS lrs
    WHERE lrs.singleton_key = 'global'
) AS policy_value ON true;

-- Requests/Audit owns this protection projection. Settings embeds it without
-- deriving a second cursor, floor, epoch or materializer state from policy
-- resources. Reader transactions take FOR SHARE; the purge owner advances the
-- generations while acquiring its exclusive fence.
CREATE TABLE public.audit_retention_fence_projections (
    id integer NOT NULL DEFAULT 1,
    contract_version integer NOT NULL DEFAULT 1,
    fence_generation bigint NOT NULL DEFAULT 1,
    reader_fence_state text NOT NULL DEFAULT 'clear',
    materializer_generation bigint NOT NULL DEFAULT 1,
    materializer_state text NOT NULL DEFAULT 'ready',
    generated_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_audit_retention_fence_projections PRIMARY KEY (id),
    CONSTRAINT ck_audit_retention_fence_projections_singleton CHECK (id = 1),
    CONSTRAINT ck_audit_retention_fence_projections_contract CHECK (contract_version = 1),
    CONSTRAINT ck_audit_retention_fence_projections_fence_generation CHECK (fence_generation >= 1),
    CONSTRAINT ck_audit_retention_fence_projections_materializer_generation CHECK (materializer_generation >= 1),
    CONSTRAINT ck_audit_retention_fence_projections_reader_state
        CHECK (reader_fence_state IN ('clear','waiting_for_readers')),
    CONSTRAINT ck_audit_retention_fence_projections_materializer_state
        CHECK (materializer_state IN ('ready','draining','blocked'))
);

INSERT INTO public.audit_retention_fence_projections (
    id, contract_version, fence_generation, reader_fence_state,
    materializer_generation, materializer_state, generated_at, updated_at
) VALUES (1, 1, 1, 'clear', 1, 'ready', now(), now())
ON CONFLICT (id) DO NOTHING;

-- Actual coverage is an owner read model, not a synonym for the configured
-- policy. A fresh install starts with an unavailable/dirty projection. The
-- owning writers mark it dirty on mutation and the owning retention worker
-- refreshes it under the same transaction as a floor/epoch publication. A
-- dirty projection is deliberately not presented as complete coverage.
CREATE TABLE public.retention_coverage_read_models (
    dataset text NOT NULL,
    earliest_retained_at timestamp with time zone,
    latest_retained_at timestamp with time zone,
    gaps jsonb NOT NULL DEFAULT '[]'::jsonb,
    materialization_cut jsonb NOT NULL DEFAULT '{}'::jsonb,
    coverage_revision text NOT NULL DEFAULT '',
    coverage_hash text NOT NULL DEFAULT '',
    materialized_at timestamp with time zone,
    precision text NOT NULL DEFAULT 'unavailable',
    complete boolean NOT NULL DEFAULT false,
    freshness text NOT NULL DEFAULT 'unavailable',
    source_revision text NOT NULL DEFAULT '',
    retention_generation bigint NOT NULL DEFAULT 1,
    dirty boolean NOT NULL DEFAULT true,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_retention_coverage_read_models PRIMARY KEY (dataset),
    CONSTRAINT ck_retention_coverage_read_models_dataset
        CHECK (dataset IN ('request_logs','audit_logs','usage_request_events','loadbalance_events')),
    CONSTRAINT ck_retention_coverage_read_models_precision
        CHECK (precision IN ('owner_bounds','estimated','unavailable')),
    CONSTRAINT ck_retention_coverage_read_models_freshness
        CHECK (freshness IN ('fresh','stale','unavailable')),
    CONSTRAINT ck_retention_coverage_read_models_time_order
        CHECK (earliest_retained_at IS NULL OR latest_retained_at IS NULL OR earliest_retained_at <= latest_retained_at)
);

INSERT INTO public.retention_coverage_read_models (
    dataset, source_revision, retention_generation, updated_at
)
SELECT dataset, '', policy_generation, now()
FROM public.log_retention_policy_resources
ON CONFLICT (dataset) DO NOTHING;

CREATE FUNCTION public.prism_mark_retention_coverage_dirty() RETURNS trigger
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
            WHEN EXCLUDED.earliest_retained_at IS NULL THEN public.retention_coverage_read_models.earliest_retained_at
            WHEN public.retention_coverage_read_models.earliest_retained_at IS NULL THEN EXCLUDED.earliest_retained_at
            ELSE LEAST(public.retention_coverage_read_models.earliest_retained_at, EXCLUDED.earliest_retained_at)
        END,
        latest_retained_at = CASE
            WHEN EXCLUDED.latest_retained_at IS NULL THEN public.retention_coverage_read_models.latest_retained_at
            WHEN public.retention_coverage_read_models.latest_retained_at IS NULL THEN EXCLUDED.latest_retained_at
            ELSE GREATEST(public.retention_coverage_read_models.latest_retained_at, EXCLUDED.latest_retained_at)
        END,
        -- Preserve a previously complete projection for the same-transaction
        -- append handoff.  The owner writer can then extend the bounded
        -- projection without a full rescan; an already incomplete projection
        -- remains incomplete because this expression keeps its prior value.
        complete = public.retention_coverage_read_models.complete,
        freshness = 'stale',
        dirty = true,
        updated_at = now();
    RETURN NULL;
END;
$$;

CREATE TRIGGER trg_request_logs_retention_coverage_dirty
    AFTER INSERT OR UPDATE OR DELETE ON public.request_logs
    FOR EACH ROW EXECUTE FUNCTION public.prism_mark_retention_coverage_dirty('request_logs');
CREATE TRIGGER trg_audit_logs_retention_coverage_dirty
    AFTER INSERT OR UPDATE OR DELETE ON public.audit_logs
    FOR EACH ROW EXECUTE FUNCTION public.prism_mark_retention_coverage_dirty('audit_logs');
CREATE TRIGGER trg_usage_request_events_retention_coverage_dirty
    AFTER INSERT OR UPDATE OR DELETE ON public.usage_request_events
    FOR EACH ROW EXECUTE FUNCTION public.prism_mark_retention_coverage_dirty('usage_request_events');
CREATE TRIGGER trg_loadbalance_events_retention_coverage_dirty
    AFTER INSERT OR UPDATE OR DELETE ON public.loadbalance_events
    FOR EACH ROW EXECUTE FUNCTION public.prism_mark_retention_coverage_dirty('loadbalance_events');

-- Proxy Keys owns the readiness fence used by Auth staging/activation.  The
-- row is deliberately separate from auth_config_versions: readiness is a
-- Proxy-owned ledger fact and must advance even when no auth mutation is in
-- flight.  The owner refreshes it under FOR UPDATE whenever a key mutation,
-- readiness read, or auth activation recheck enters the Proxy fence.  The
-- fingerprint includes the disabled/expired/active classification and the
-- 30-second safety frontier, so wall-clock expiry changes cannot be hidden
-- behind a stale generation.
CREATE TABLE public.proxy_key_readiness_state (
    id integer NOT NULL DEFAULT 1,
    generation bigint NOT NULL DEFAULT 1,
    classification_fingerprint text NOT NULL DEFAULT '',
    counted_at timestamp with time zone NOT NULL,
    active_count bigint NOT NULL DEFAULT 0,
    expired_count bigint NOT NULL DEFAULT 0,
    disabled_count bigint NOT NULL DEFAULT 0,
    safe_active_count bigint NOT NULL DEFAULT 0,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_proxy_key_readiness_state PRIMARY KEY (id),
    CONSTRAINT ck_proxy_key_readiness_state_singleton CHECK (id = 1),
    CONSTRAINT ck_proxy_key_readiness_state_generation CHECK (generation >= 1),
    CONSTRAINT ck_proxy_key_readiness_state_counts CHECK (
        active_count >= 0 AND expired_count >= 0 AND disabled_count >= 0
        AND safe_active_count >= 0 AND safe_active_count <= active_count
    )
);

INSERT INTO public.proxy_key_readiness_state (
    id, generation, classification_fingerprint, counted_at, updated_at
) VALUES (1, 1, '', now(), now())
ON CONFLICT (id) DO NOTHING;

-- ============================================================================
-- Part 3: log_retention_preflights (single-use destructive preflight records)
-- ============================================================================

CREATE TABLE public.log_retention_preflights (
    id text NOT NULL,
    kind text NOT NULL,
    operation_id text NOT NULL,
    preflight_attempt_id text NOT NULL,
    token_hash text NOT NULL,
    scope text NOT NULL DEFAULT 'instance',
    request_hash text NOT NULL,
    settings_revision bigint,
    principal_generation text,
    affected_domains jsonb NOT NULL,
    confirmation_keyword text NOT NULL,
    previewed_at timestamp with time zone NOT NULL,
    generated_at timestamp with time zone NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    consumed_operation_id text,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_log_retention_preflights PRIMARY KEY (id),
    CONSTRAINT ck_log_retention_preflights_kind
        CHECK (kind IN ('policy_change','manual_cleanup')),
    CONSTRAINT ck_log_retention_preflights_scope CHECK (scope = 'instance'),
    CONSTRAINT ck_log_retention_preflights_time_order
        CHECK (generated_at >= previewed_at AND expires_at > generated_at),
    CONSTRAINT uq_log_retention_preflights_attempt UNIQUE (operation_id, preflight_attempt_id)
);

CREATE INDEX idx_log_retention_preflights_expiry
    ON public.log_retention_preflights (expires_at);

-- ============================================================================
-- Part 4: management_jobs v2 extension for log_retention
-- ============================================================================

ALTER TABLE public.management_jobs
    ADD COLUMN contract_version integer NOT NULL DEFAULT 1,
    ADD COLUMN operation_id text,
    ADD COLUMN request_hash text,
    ADD COLUMN resource_key text,
    ADD COLUMN origin text,
    ADD COLUMN legacy_origin_provenance text,
    ADD COLUMN legacy_execution_provenance text,
    ADD COLUMN preflight_id text,
    ADD COLUMN policy_revision bigint,
    ADD COLUMN policy_generation bigint,
    ADD COLUMN fence_generation bigint,
    ADD COLUMN purge_to_time timestamp with time zone,
    ADD COLUMN purge_state text,
    ADD COLUMN stage text,
    ADD COLUMN terminal_disposition text,
    ADD COLUMN legacy_original_state text,
    ADD COLUMN boundary_rows_deleted bigint,
    ADD COLUMN dropped_partition_count bigint,
    ADD COLUMN dropped_partition_count_accuracy text,
    ADD COLUMN dropped_rows_estimate bigint,
    ADD COLUMN dropped_rows_accuracy text,
    ADD COLUMN staged_items_tombstoned bigint,
    ADD COLUMN sensitive_artifact_bytes_deleted bigint,
    ADD COLUMN classification_evidence_generation text,
    ADD COLUMN classification_evidence_hash text,
    ADD COLUMN visibility_state text,
    ADD COLUMN worker_generation bigint;

-- Existing log-retention rows are legacy v1 history. Their rows_deleted is
-- boundary-only evidence and is copied into boundary_rows_deleted so no
-- consumer can mistake it for a whole-scope total.
UPDATE public.management_jobs
SET contract_version = 1,
    resource_key = scope_json->>'table',
    boundary_rows_deleted = rows_deleted,
    visibility_state = 'legacy_unknown',
    worker_generation = 0
WHERE type = 'log_retention';

-- v2 state machine checks (extend the legacy state check with superseded).
ALTER TABLE public.management_jobs
    DROP CONSTRAINT management_jobs_state_check;

ALTER TABLE public.management_jobs
    ADD CONSTRAINT management_jobs_state_check
        CHECK (state = ANY (ARRAY[
            'queued','running','cancel_requested','cancelled','succeeded','failed','superseded'
        ]));

ALTER TABLE public.management_jobs
    ADD CONSTRAINT ck_management_jobs_log_retention_v2
        CHECK (
            type <> 'log_retention'
            OR contract_version = 1
            OR (
                contract_version = 2
                AND operation_id IS NOT NULL
                AND request_hash IS NOT NULL
                AND origin IS NOT NULL
            )
        ),
    ADD CONSTRAINT ck_management_jobs_log_retention_origin
        CHECK (
            type <> 'log_retention'
            OR origin IS NULL
            OR origin IN ('automatic','manual')
        ),
    ADD CONSTRAINT ck_management_jobs_log_retention_purge_state
        CHECK (
            type <> 'log_retention'
            OR purge_state IS NULL
            OR purge_state IN ('not_started','running','recovery_required','published','rolled_back','legacy_unknown')
        ),
    ADD CONSTRAINT ck_management_jobs_log_retention_stage
        CHECK (
            type <> 'log_retention'
            OR stage IS NULL
            OR stage IN (
                'waiting_for_resource','queued','waiting_for_protection','acquiring_purge_fence',
                'purge_running','planning_physical_reclaim','dropping_partitions',
                'deleting_boundary_rows','cleaning_rollup_and_staging','vacuuming_boundary',
                'publishing_epoch_coverage','finished'
            )
        ),
    ADD CONSTRAINT ck_management_jobs_log_retention_terminal_disposition
        CHECK (
            type <> 'log_retention'
            OR terminal_disposition IS NULL
            OR terminal_disposition IN ('completed','cancelled','failed','superseded_by_v2_planning')
        ),
    ADD CONSTRAINT ck_management_jobs_log_retention_legacy_original_state
        CHECK (
            type <> 'log_retention'
            OR legacy_original_state IS NULL
            OR legacy_original_state IN ('queued','cancel_requested')
        ),
    ADD CONSTRAINT ck_management_jobs_log_retention_legacy_origin
        CHECK (
            type <> 'log_retention'
            OR legacy_origin_provenance IS NULL
            OR legacy_origin_provenance IN (
                'proven_automatic_scheduler','proven_manual','conservative_manual_drain',
                'repair_required','repaired_manual_drain'
            )
        ),
    ADD CONSTRAINT ck_management_jobs_log_retention_legacy_execution
        CHECK (
            type <> 'log_retention'
            OR legacy_execution_provenance IS NULL
            OR legacy_execution_provenance IN (
                'proven_never_executed','claimed_or_running','partial_irreversible_effects',
                'terminal','unknown'
            )
        ),
    ADD CONSTRAINT ck_management_jobs_log_retention_legacy_contract
        CHECK (
            type <> 'log_retention'
            OR (contract_version = 2 AND legacy_original_state IS NULL AND legacy_origin_provenance IS NULL AND legacy_execution_provenance IS NULL)
            OR contract_version = 1
        ),
    ADD CONSTRAINT ck_management_jobs_log_retention_superseded_scope
        CHECK (
            type <> 'log_retention'
            OR state <> 'superseded'
            OR (
                contract_version = 1
                AND origin = 'automatic'
                AND legacy_origin_provenance = 'proven_automatic_scheduler'
                AND legacy_execution_provenance = 'proven_never_executed'
                AND terminal_disposition = 'superseded_by_v2_planning'
                AND legacy_original_state IS NOT NULL
                AND finished_at IS NOT NULL
                AND classification_evidence_hash IS NOT NULL
            )
        ),
    ADD CONSTRAINT ck_management_jobs_log_retention_legacy_provenance_pair
        CHECK (
            type <> 'log_retention'
            OR (legacy_origin_provenance IS NULL AND legacy_execution_provenance IS NULL)
            OR (legacy_origin_provenance IS NOT NULL AND legacy_execution_provenance IS NOT NULL)
        );

ALTER TABLE public.management_jobs
    ADD CONSTRAINT ck_management_jobs_log_retention_fence_generation
        CHECK (
            type <> 'log_retention'
            OR contract_version = 1
            OR (origin = 'manual' AND fence_generation IS NULL)
            OR (fence_generation IS NOT NULL AND fence_generation >= 1)
        );

-- At most one v2 manual reservation or execution-owning manual job per dataset.
CREATE UNIQUE INDEX uq_management_jobs_log_retention_manual_active
    ON public.management_jobs (resource_key)
    WHERE type = 'log_retention' AND contract_version = 2 AND origin = 'manual'
      AND state IN ('queued','running');

-- ============================================================================
-- Part 5: legacy retention scheduler fence + worker-generation evidence
-- ============================================================================

CREATE TABLE public.retention_worker_transition_state (
    id integer NOT NULL DEFAULT 1,
    minimum_worker_generation bigint NOT NULL,
    legacy_create_authorized boolean NOT NULL DEFAULT false,
    legacy_claim_authorized boolean NOT NULL DEFAULT false,
    legacy_delete_authorized boolean NOT NULL DEFAULT false,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_retention_worker_transition_state PRIMARY KEY (id),
    CONSTRAINT ck_retention_worker_transition_state_singleton CHECK (id = 1)
);

-- The legacy 5-second scheduler is fenced from this migration onward: its
-- creates fail, its claims/deletes are refused unless the frozen
-- generation-tagged v1 drain executor is explicitly authorized below.
INSERT INTO public.retention_worker_transition_state (
    id, minimum_worker_generation, legacy_create_authorized,
    legacy_claim_authorized, legacy_delete_authorized, updated_at
) VALUES (1, 1, false, false, false, now())
ON CONFLICT (id) DO NOTHING;

CREATE FUNCTION public.prism_guard_log_retention_job() RETURNS trigger
    LANGUAGE plpgsql AS $$
DECLARE
    min_generation bigint;
    drain_claim_allowed boolean;
BEGIN
    SELECT minimum_worker_generation, legacy_claim_authorized AND legacy_delete_authorized
        INTO min_generation, drain_claim_allowed
        FROM public.retention_worker_transition_state WHERE id = 1;

    IF TG_OP = 'INSERT' THEN
        -- Creating a log-retention job requires an authorized v2 worker
        -- generation. The legacy scheduler (no worker evidence) can never
        -- create again.
        IF NEW.worker_generation IS NULL OR NEW.worker_generation < min_generation THEN
            RAISE EXCEPTION 'prism_retention_worker_fenced: create requires worker_generation >= %', min_generation
                USING ERRCODE = '42501';
        END IF;
        RETURN NEW;
    END IF;

    IF TG_OP = 'DELETE' THEN
        IF OLD.worker_generation IS NULL OR (OLD.worker_generation < min_generation AND NOT drain_claim_allowed) THEN
            RAISE EXCEPTION 'prism_retention_worker_fenced: delete requires worker_generation >= %', min_generation
                USING ERRCODE = '42501';
        END IF;
        RETURN OLD;
    END IF;

    -- UPDATE (claim / checkpoint / terminal): v2 workers pass by generation;
    -- the frozen v1 drain executor may only touch legacy contract_version=1
    -- rows it already owns and never create new work.
    IF NEW.worker_generation IS NULL OR NEW.worker_generation < min_generation THEN
        IF NOT (drain_claim_allowed AND OLD.contract_version = 1 AND NEW.contract_version = 1) THEN
            RAISE EXCEPTION 'prism_retention_worker_fenced: update requires worker_generation >= %', min_generation
                USING ERRCODE = '42501';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

-- The trigger is installed at the very end of this migration (after every
-- backfill that touches management_jobs) so the migration's own legacy
-- inventory writes are never mistaken for unauthorized worker traffic.

-- ============================================================================
-- Part 6: legacy_retention_job_classification_evidence (append-only)
-- ============================================================================

CREATE TABLE public.legacy_retention_job_classification_evidence (
    id bigint GENERATED ALWAYS AS IDENTITY,
    job_id text NOT NULL,
    evidence_generation text NOT NULL,
    evidence_hash text NOT NULL,
    predecessor_evidence_hash text,
    classification text NOT NULL,
    legacy_origin_provenance text NOT NULL,
    execution_provenance text NOT NULL,
    scheduler_sentinel_exact_match boolean NOT NULL,
    log_retention_type_and_shape_proven boolean NOT NULL,
    scheduler_creation_provenance text NOT NULL,
    safe_v1_predicate_and_checkpoint text NOT NULL,
    current_head boolean NOT NULL DEFAULT true,
    classified_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_legacy_retention_job_classification_evidence PRIMARY KEY (id),
    CONSTRAINT ck_legacy_retention_job_classification_evidence_classification
        CHECK (classification IN ('automatic','manual_drain','repair_required')),
    CONSTRAINT ck_legacy_retention_job_classification_evidence_origin
        CHECK (legacy_origin_provenance IN (
            'proven_automatic_scheduler','proven_manual','conservative_manual_drain',
            'repair_required','repaired_manual_drain'
        )),
    CONSTRAINT ck_legacy_retention_job_classification_evidence_execution
        CHECK (execution_provenance IN (
            'proven_never_executed','claimed_or_running','partial_irreversible_effects',
            'terminal','unknown'
        )),
    CONSTRAINT ck_legacy_retention_job_classification_evidence_creation
        CHECK (scheduler_creation_provenance IN ('proven','absent','conflicting','unknown')),
    CONSTRAINT ck_legacy_retention_job_classification_evidence_safe
        CHECK (safe_v1_predicate_and_checkpoint IN ('proven','repair_required'))
);

-- One current evidence head per legacy job.
CREATE UNIQUE INDEX uq_legacy_retention_job_classification_evidence_current
    ON public.legacy_retention_job_classification_evidence (job_id)
    WHERE current_head;

-- Conservative generation-1 classification of every existing log-retention
-- job from immutable evidence only (requested_by sentinel + shape + creation
-- provenance). Caller-controlled reason/idempotency text never participates
-- in the automatic predicate beyond the exact scheduler constants.
DO $$
DECLARE
    job_record record;
    sentinel boolean;
    shape_proven boolean;
    creation_provenance text;
    safe_predicate boolean;
    classification text;
    origin_provenance text;
    execution_provenance text;
    event_count bigint;
    evidence_hash text;
    generation_value text;
BEGIN
    generation_value := '1';

    FOR job_record IN
        SELECT j.id, j.type, j.state, j.requested_by, j.reason, j.idempotency_key,
               j.scope_json, j.started_at, j.rows_deleted, j.batches_completed,
               j.finished_at, j.cancel_requested
        FROM public.management_jobs AS j
        WHERE j.type = 'log_retention'
        ORDER BY j.requested_at ASC, j.id ASC
    LOOP
        -- Exact scheduler sentinel: only the frozen scheduler constant counts.
        sentinel := (job_record.requested_by = 'scheduled-log-retention');

        -- Shape proof: log-retention fixed dataset, bounded cutoff (not
        -- delete_all) and the exact scheduler reason.
        shape_proven := (
            job_record.scope_json->>'table' IN ('request_logs','audit_logs','usage_request_events','loadbalance_events')
            AND COALESCE((job_record.scope_json->>'delete_all')::boolean, false) = false
            AND job_record.scope_json->>'cutoff' IS NOT NULL
            AND job_record.reason = 'scheduled global log retention cleanup'
        );

        -- Scheduler creation provenance: the exact sentinel + reason +
        -- deterministic idempotency shape (table:YYYY-MM-DD).
        IF NOT sentinel THEN
            creation_provenance := 'absent';
        ELSIF job_record.reason = 'scheduled global log retention cleanup'
              AND job_record.idempotency_key IS NOT NULL
              AND job_record.idempotency_key ~ '^(request_logs|audit_logs|usage_request_events|loadbalance_events):[0-9]{4}-[0-9]{2}-[0-9]{2}$' THEN
            creation_provenance := 'proven';
        ELSE
            creation_provenance := 'conflicting';
        END IF;

        -- Safe v1 predicate/checkpoint: scope shape is safe to drain.
        safe_predicate := shape_proven;

        -- Execution provenance from durable state/effects.
        SELECT COUNT(*) INTO event_count
        FROM public.management_job_events AS e
        WHERE e.job_id = job_record.id;

        IF job_record.state IN ('succeeded','failed','cancelled') THEN
            execution_provenance := 'terminal';
        ELSIF job_record.started_at IS NOT NULL
              OR job_record.rows_deleted > 0
              OR job_record.batches_completed > 0
              OR event_count > 0 THEN
            IF job_record.rows_deleted > 0 OR job_record.batches_completed > 0 THEN
                execution_provenance := 'partial_irreversible_effects';
            ELSE
                execution_provenance := 'claimed_or_running';
            END IF;
        ELSE
            execution_provenance := 'proven_never_executed';
        END IF;

        -- Conservative origin: automatic requires all three proofs together;
        -- a sentinel collision or missing/conflicting creation proof is never
        -- automatic. Safe unknown rows drain as manual; unsafe rows block.
        IF sentinel AND shape_proven AND creation_provenance = 'proven' THEN
            classification := 'automatic';
            origin_provenance := 'proven_automatic_scheduler';
        ELSIF safe_predicate THEN
            classification := 'manual_drain';
            origin_provenance := 'conservative_manual_drain';
        ELSE
            classification := 'repair_required';
            origin_provenance := 'repair_required';
        END IF;

        IF NOT safe_predicate THEN
            safe_predicate := false;
        END IF;

        evidence_hash := encode(
            sha256(
                convert_to(
                    generation_value || '|' || job_record.id || '|' ||
                    classification || '|' || origin_provenance || '|' ||
                    execution_provenance || '|' ||
                    CASE WHEN sentinel THEN 'sentinel' ELSE 'no-sentinel' END || '|' ||
                    CASE WHEN shape_proven THEN 'shape' ELSE 'no-shape' END || '|' ||
                    creation_provenance || '|' ||
                    CASE WHEN safe_predicate THEN 'safe' ELSE 'repair' END,
                    'UTF8'
                )
            ),
            'hex'
        );

        INSERT INTO public.legacy_retention_job_classification_evidence (
            job_id, evidence_generation, evidence_hash, predecessor_evidence_hash,
            classification, legacy_origin_provenance, execution_provenance,
            scheduler_sentinel_exact_match, log_retention_type_and_shape_proven,
            scheduler_creation_provenance, safe_v1_predicate_and_checkpoint,
            current_head, classified_at
        ) VALUES (
            job_record.id, generation_value, evidence_hash, NULL,
            classification, origin_provenance, execution_provenance,
            sentinel, shape_proven, creation_provenance,
            CASE WHEN safe_predicate THEN 'proven' ELSE 'repair_required' END,
            true, now()
        );

        UPDATE public.management_jobs
        SET origin = CASE WHEN classification = 'automatic' THEN 'automatic' ELSE 'manual' END,
            legacy_origin_provenance = origin_provenance,
            legacy_execution_provenance = execution_provenance,
            classification_evidence_generation = generation_value,
            classification_evidence_hash = evidence_hash
        WHERE id = job_record.id;
    END LOOP;
END $$;

-- ============================================================================
-- Part 7: settings_mutation_operations (durable operation outcome store)
-- ============================================================================

CREATE TABLE public.settings_mutation_operations (
    id bigint GENERATED ALWAYS AS IDENTITY,
    resource_kind text NOT NULL,
    operation_id text NOT NULL,
    request_hash text NOT NULL,
    expected_revision text,
    state text NOT NULL,
    result_json jsonb,
    safe_error_code text,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_settings_mutation_operations PRIMARY KEY (id),
    CONSTRAINT uq_settings_mutation_operations_identity UNIQUE (resource_kind, operation_id),
    CONSTRAINT ck_settings_mutation_operations_kind
        CHECK (resource_kind IN (
            'log_retention','manual_retention_job','retention_job_cancel',
            'owner_drift_archive','audit_settings','auth_settings'
        )),
    CONSTRAINT ck_settings_mutation_operations_state
        CHECK (state IN (
            'accepted','completed','failed','conflict','rolled_back','rollback_required'
        ))
);

-- ============================================================================
-- Part 8: settings_migration_evidence + owner-drift inventory (generation 1)
-- ============================================================================

CREATE TABLE public.settings_migration_evidence (
    id bigint GENERATED ALWAYS AS IDENTITY,
    head_id text NOT NULL,
    lineage_generation text NOT NULL,
    predecessor_head_id text,
    field text NOT NULL,
    evidence_hash text NOT NULL,
    instance_value jsonb NOT NULL,
    legacy_copy_value jsonb NOT NULL,
    resolution_state text NOT NULL,
    terminal_disposition text,
    is_current boolean NOT NULL DEFAULT true,
    generated_at timestamp with time zone NOT NULL,
    resolved_at timestamp with time zone,
    CONSTRAINT pk_settings_migration_evidence PRIMARY KEY (id),
    CONSTRAINT ck_settings_migration_evidence_field
        CHECK (field IN ('request_logs_retention_days','statistics_retention_days','audit_logs_retention_days')),
    CONSTRAINT ck_settings_migration_evidence_resolution
        CHECK (resolution_state IN ('drift','converged','archived')),
    CONSTRAINT ck_settings_migration_evidence_disposition
        CHECK (terminal_disposition IS NULL OR terminal_disposition = 'superseded_by_policy_change')
);

-- One current head per duplicated field.
CREATE UNIQUE INDEX uq_settings_migration_evidence_current
    ON public.settings_migration_evidence (field)
    WHERE is_current;

CREATE TABLE public.settings_owner_drift_inventory (
    id integer NOT NULL DEFAULT 1,
    inventory_generation text NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_settings_owner_drift_inventory PRIMARY KEY (id),
    CONSTRAINT ck_settings_owner_drift_inventory_singleton CHECK (id = 1)
);

-- Generation-1 current heads: compare the instance-owned
-- log_retention_settings values against the legacy profile-scoped
-- user_settings copies. Equal (including both NULL) => converged; any
-- difference (including null-vs-non-null) => drift. The instance value is
-- authoritative; the additive migration never picks a winner or rewrites
-- either side.
DO $$
DECLARE
    inventory_generation_value text := '1';
    field_value record;
    instance_raw integer;
    legacy_raw integer;
    instance_json jsonb;
    legacy_json jsonb;
    head_id_value text;
    evidence_hash_value text;
BEGIN
    INSERT INTO public.settings_owner_drift_inventory (id, inventory_generation, updated_at)
    VALUES (1, inventory_generation_value, now())
    ON CONFLICT (id) DO NOTHING;

    FOR field_value IN
        SELECT column_name, instance_value, legacy_value
        FROM (VALUES
            ('request_logs_retention_days'::text),
            ('statistics_retention_days'::text),
            ('audit_logs_retention_days'::text)
        ) AS f(column_name)
        CROSS JOIN LATERAL (
            SELECT
                CASE f.column_name
                    WHEN 'request_logs_retention_days' THEN lrs.request_logs_retention_days
                    WHEN 'statistics_retention_days' THEN lrs.statistics_retention_days
                    ELSE lrs.audit_logs_retention_days
                END,
                CASE f.column_name
                    WHEN 'request_logs_retention_days' THEN us.request_logs_retention_days
                    WHEN 'statistics_retention_days' THEN us.statistics_retention_days
                    ELSE us.audit_logs_retention_days
                END
            FROM public.log_retention_settings AS lrs
            LEFT JOIN public.user_settings AS us
                ON us.profile_id = 1
            WHERE lrs.singleton_key = 'global'
        ) AS pair(instance_value, legacy_value)
    LOOP
        instance_raw := field_value.instance_value;
        legacy_raw := field_value.legacy_value;

        instance_json := CASE
            WHEN instance_raw IS NULL THEN '{"state":"valid","value":null}'::jsonb
            WHEN instance_raw >= 1 AND instance_raw <= 36500 THEN jsonb_build_object('state','valid','value',instance_raw)
            ELSE jsonb_build_object('state','repair_required','raw_integer', instance_raw::text)
        END;
        legacy_json := CASE
            WHEN legacy_raw IS NULL THEN '{"state":"valid","value":null}'::jsonb
            WHEN legacy_raw >= 1 AND legacy_raw <= 36500 THEN jsonb_build_object('state','valid','value',legacy_raw)
            ELSE jsonb_build_object('state','repair_required','raw_integer', legacy_raw::text)
        END;

        head_id_value := encode(
            sha256(convert_to('drift-head|' || field_value.column_name || '|' || inventory_generation_value, 'UTF8')),
            'hex'
        );
        evidence_hash_value := encode(
            sha256(convert_to(
                'gen|' || inventory_generation_value || '|field|' || field_value.column_name ||
                '|instance|' || instance_json::text || '|legacy|' || legacy_json::text,
                'UTF8'
            )),
            'hex'
        );

        INSERT INTO public.settings_migration_evidence (
            head_id, lineage_generation, predecessor_head_id, field, evidence_hash,
            instance_value, legacy_copy_value, resolution_state, terminal_disposition,
            is_current, generated_at, resolved_at
        ) VALUES (
            head_id_value, inventory_generation_value, NULL, field_value.column_name,
            evidence_hash_value, instance_json, legacy_json,
            CASE WHEN instance_json = legacy_json THEN 'converged' ELSE 'drift' END,
            NULL, true, now(), NULL
        );
    END LOOP;
END $$;

-- ============================================================================
-- Part 9: settings_schema_transition (explicit finalizer state)
-- ============================================================================

CREATE TABLE public.settings_schema_transition (
    id integer NOT NULL DEFAULT 1,
    domain_phase text NOT NULL,
    additive_committed_at timestamp with time zone NOT NULL,
    finalizer_lease_owner text,
    finalizer_lease_token text,
    finalizer_lease_expires_at timestamp with time zone,
    readiness_manifest_hash text,
    last_safe_error text,
    finalized_at timestamp with time zone,
    updated_at timestamp with time zone NOT NULL DEFAULT now(),
    CONSTRAINT pk_settings_schema_transition PRIMARY KEY (id),
    CONSTRAINT ck_settings_schema_transition_singleton CHECK (id = 1),
    CONSTRAINT ck_settings_schema_transition_phase
        CHECK (domain_phase IN ('additive','repair_ready','quiescing','finalizing','final'))
);

INSERT INTO public.settings_schema_transition (id, domain_phase, additive_committed_at)
VALUES (1, 'additive', now())
ON CONFLICT (id) DO NOTHING;

-- ============================================================================
-- Part 10: audit group revision + immutable migration provenance
-- ============================================================================

CREATE TABLE public.profile_audit_settings_state (
    profile_id integer NOT NULL,
    revision bigint NOT NULL DEFAULT 1,
    writer_generation bigint NOT NULL DEFAULT 1,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_profile_audit_settings_state PRIMARY KEY (profile_id)
);

ALTER TABLE public.profile_api_family_audit_settings
    ADD COLUMN migration_provenance text;

-- Label every pre-existing family row and materialize each missing family as
-- disabled with legacy_missing_projected_disabled, preserving the effective
-- request-time behavior the legacy reader projected.
DO $$
DECLARE
    profile_record record;
    family_value text;
BEGIN
    FOR profile_record IN SELECT id FROM public.profiles WHERE deleted_at IS NULL ORDER BY id ASC
    LOOP
        UPDATE public.profile_api_family_audit_settings
        SET migration_provenance = 'legacy_existing'
        WHERE profile_id = profile_record.id;

        FOREACH family_value IN ARRAY ARRAY['openai','anthropic','gemini']
        LOOP
            IF NOT EXISTS (
                SELECT 1 FROM public.profile_api_family_audit_settings
                WHERE profile_id = profile_record.id AND api_family = family_value
            ) THEN
                INSERT INTO public.profile_api_family_audit_settings (
                    profile_id, api_family, audit_enabled, audit_capture_bodies,
                    migration_provenance, created_at, updated_at
                ) VALUES (
                    profile_record.id, family_value, false, false,
                    'legacy_missing_projected_disabled', now(), now()
                );
            END IF;
        END LOOP;

        INSERT INTO public.profile_audit_settings_state (profile_id, revision, writer_generation, updated_at)
        VALUES (profile_record.id, 1, 1, now())
        ON CONFLICT (profile_id) DO NOTHING;
    END LOOP;
END $$;

ALTER TABLE public.profile_api_family_audit_settings
    ALTER COLUMN migration_provenance SET NOT NULL;

ALTER TABLE public.profile_api_family_audit_settings
    ADD CONSTRAINT ck_profile_api_family_audit_settings_provenance
        CHECK (migration_provenance IN ('explicit','legacy_existing','legacy_missing_projected_disabled'));

-- ============================================================================
-- Part 11: auth_config_versions + app_auth_settings desired/effective pointers
-- ============================================================================

CREATE TABLE public.auth_config_versions (
    id bigint GENERATED ALWAYS AS IDENTITY,
    subject_key text NOT NULL,
    generation text NOT NULL,
    desired_mode text NOT NULL,
    username text,
    password_hash text,
    session_version bigint NOT NULL,
    state text NOT NULL,
    created_operation_id text,
    readiness_generation text,
    counted_at timestamp with time zone,
    active_count text,
    expired_count text,
    disabled_count text,
    safe_active_count text,
    zero_key_acknowledged boolean NOT NULL DEFAULT false,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_auth_config_versions PRIMARY KEY (id),
    CONSTRAINT uq_auth_config_versions_generation UNIQUE (subject_key, generation),
    CONSTRAINT ck_auth_config_versions_mode
        CHECK (desired_mode IN ('enabled','disabled')),
    CONSTRAINT ck_auth_config_versions_state
        CHECK (state IN ('staged','effective','unusable','superseded'))
);

ALTER TABLE public.app_auth_settings
    ADD COLUMN auth_revision bigint NOT NULL DEFAULT 1,
    ADD COLUMN desired_config_version_id bigint,
    ADD COLUMN effective_config_version_id bigint,
    ADD COLUMN desired_generation text,
    ADD COLUMN effective_generation text,
    ADD COLUMN transition_operation_id text,
    ADD COLUMN transition_kind text,
    ADD COLUMN transition_state text,
    ADD COLUMN fenced_at timestamp with time zone;

ALTER TABLE public.app_auth_settings
    ADD CONSTRAINT ck_app_auth_settings_transition_kind
        CHECK (transition_kind IS NULL OR transition_kind IN (
            'enable','disable','account_update','account_and_mode_update'
        )),
    ADD CONSTRAINT ck_app_auth_settings_v2_transition_state
        CHECK (transition_state IS NULL OR transition_state IN (
            'staged','publishing','retrying','rollback_required'
        ));

-- Copy the currently effective mode/credential/session version into immutable
-- generation 1 and point both desired/effective pointers at it. Every current
-- session keeps working; nothing is toggled and no session is invalidated.
-- Fresh databases have no app_auth_settings row at migration time: the
-- startup seed creates the row AND the generation-1 config version with the
-- startup clock (deterministic seeds golden). This block only backfills
-- EXISTING rows (upgrade path).
DO $$
DECLARE
    auth_row record;
    config_id bigint;
    auth_exists boolean;
BEGIN
    SELECT EXISTS (
        SELECT 1 FROM public.app_auth_settings WHERE singleton_key = 'app'
    ) INTO auth_exists;

    IF NOT auth_exists THEN
        RETURN;
    END IF;

    SELECT * INTO auth_row
    FROM public.app_auth_settings WHERE singleton_key = 'app' LIMIT 1;

    INSERT INTO public.auth_config_versions (
        subject_key, generation, desired_mode, username, password_hash,
        session_version, state, created_operation_id, created_at, updated_at
    ) VALUES (
        'app', '1', CASE WHEN auth_row.auth_enabled THEN 'enabled' ELSE 'disabled' END,
        auth_row.username, auth_row.password_hash, auth_row.token_version,
        'effective', NULL, now(), now()
    )
    RETURNING id INTO config_id;

    UPDATE public.app_auth_settings
    SET auth_revision = 1,
        desired_config_version_id = config_id,
        effective_config_version_id = config_id,
        desired_generation = '1',
        effective_generation = '1'
    WHERE singleton_key = 'app';
END $$;

-- ============================================================================
-- Part 12: audit_storage_daily_facts + current-generation pointer
-- ============================================================================

CREATE TABLE public.audit_storage_daily_facts (
    id bigint GENERATED ALWAYS AS IDENTITY,
    storage_fact_generation text NOT NULL,
    utc_day date NOT NULL,
    observe_source_revision text NOT NULL,
    retention_generation text,
    logical_rows bigint NOT NULL,
    logical_header_bytes bigint NOT NULL,
    logical_body_bytes bigint NOT NULL,
    delta_complete boolean NOT NULL,
    checksum text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_audit_storage_daily_facts PRIMARY KEY (id),
    CONSTRAINT uq_audit_storage_daily_facts_day UNIQUE (storage_fact_generation, utc_day),
    CONSTRAINT ck_audit_storage_daily_facts_counts
        CHECK (logical_rows >= 0 AND logical_header_bytes >= 0 AND logical_body_bytes >= 0)
);

CREATE TABLE public.audit_storage_fact_state (
    id integer NOT NULL DEFAULT 1,
    current_generation text,
    facts_complete boolean NOT NULL DEFAULT false,
    last_fact_day date,
    generated_at timestamp with time zone,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_audit_storage_fact_state PRIMARY KEY (id),
    CONSTRAINT ck_audit_storage_fact_state_singleton CHECK (id = 1)
);

INSERT INTO public.audit_storage_fact_state (id, current_generation, facts_complete, generated_at, updated_at)
VALUES (1, NULL, false, NULL, now())
ON CONFLICT (id) DO NOTHING;

-- ============================================================================
-- Part 13: audit purge tombstones and late-materialization guards
-- ============================================================================

-- Audit bytes can exist in the materializer's artifact table, staging table, or
-- a not-yet-materialized v2 outbox item even after the corresponding audit row
-- has been selected for purge.  Tombstones are append-only evidence keyed by
-- ingress.  They let the owner remove sensitive bytes now and make a later
-- retry of an old materialization harmless instead of resurrecting the audit
-- fact.
CREATE TABLE public.audit_retention_tombstones (
    id bigint GENERATED ALWAYS AS IDENTITY,
    profile_id integer NOT NULL,
    ingress_request_id character varying(36) NOT NULL,
    cutoff timestamp with time zone NOT NULL,
    retention_generation bigint NOT NULL,
    reason text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_audit_retention_tombstones PRIMARY KEY (id),
    CONSTRAINT uq_audit_retention_tombstones_identity
        UNIQUE (profile_id, ingress_request_id, cutoff, retention_generation),
    CONSTRAINT ck_audit_retention_tombstones_generation CHECK (retention_generation >= 1)
);

CREATE INDEX idx_audit_retention_tombstones_ingress
    ON public.audit_retention_tombstones (profile_id, ingress_request_id, cutoff DESC);

CREATE FUNCTION public.prism_guard_audit_tombstone_mutation() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'audit_retention_tombstones are append-only'
            USING ERRCODE = '42501';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_audit_retention_tombstones_append_only
    BEFORE UPDATE OR DELETE ON public.audit_retention_tombstones
    FOR EACH ROW EXECUTE FUNCTION public.prism_guard_audit_tombstone_mutation();

-- A late audit materialization is deliberately dropped at the row boundary.
-- The request/usage materialization transaction can still commit its other
-- domains; no stale audit row or sensitive body is reintroduced.
CREATE FUNCTION public.prism_guard_late_audit_log() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.ingress_request_id IS NOT NULL AND EXISTS (
        SELECT 1
        FROM public.audit_retention_tombstones AS t
        WHERE t.profile_id = NEW.profile_id
          AND t.ingress_request_id = NEW.ingress_request_id
		  AND NEW.created_at < t.cutoff
    ) THEN
        RETURN NULL;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_audit_logs_late_retention_guard
    BEFORE INSERT OR UPDATE ON public.audit_logs
    FOR EACH ROW EXECUTE FUNCTION public.prism_guard_late_audit_log();

-- Artifact/staging payloads are scrubbed in place before a late retry can
-- expose them.  The purge worker deletes finalized artifact rows separately;
-- this trigger covers a concurrent retry that races with that deletion.
CREATE FUNCTION public.prism_guard_late_audit_artifact() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.ingress_request_id IS NOT NULL AND EXISTS (
        SELECT 1
        FROM public.audit_retention_tombstones AS t
        WHERE t.profile_id = NEW.profile_id
          AND t.ingress_request_id = NEW.ingress_request_id
		  AND COALESCE(NEW.audit_component_created_at, NEW.created_at) < t.cutoff
    ) THEN
        NEW.payload := '{}'::jsonb;
        NEW.lifecycle_state := 'telemetry_orphaned';
        NEW.capture_status := 'tombstoned';
        NEW.observed_bytes := 0;
        NEW.stored_bytes := 0;
        NEW.truncated := false;
        NEW.last_safe_error_code := 'audit_retention_tombstoned';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_runtime_telemetry_artifacts_late_retention_guard
    BEFORE INSERT OR UPDATE ON public.runtime_telemetry_artifacts
    FOR EACH ROW EXECUTE FUNCTION public.prism_guard_late_audit_artifact();

CREATE FUNCTION public.prism_guard_late_audit_staging() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.ingress_request_id IS NOT NULL AND EXISTS (
        SELECT 1
        FROM public.audit_retention_tombstones AS t
        WHERE t.profile_id = NEW.profile_id
          AND t.ingress_request_id = NEW.ingress_request_id
		  AND NEW.created_at < t.cutoff
    ) THEN
        NEW.payload := '{}'::jsonb;
        NEW.state := 'tombstoned';
        NEW.last_safe_error_code := 'audit_retention_tombstoned';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_audit_artifact_staging_late_retention_guard
    BEFORE INSERT OR UPDATE ON public.audit_artifact_staging
    FOR EACH ROW EXECUTE FUNCTION public.prism_guard_late_audit_staging();

-- v2 metadata carries the request/usage facts separately from the audit
-- extension.  Omitting only the audit extension preserves non-audit telemetry
-- while preventing a pending v2 item from retaining an old audit payload.
CREATE FUNCTION public.prism_guard_late_audit_outbox() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.schema_version = 2 AND NEW.ingress_request_id IS NOT NULL AND EXISTS (
        SELECT 1
        FROM public.audit_retention_tombstones AS t
        WHERE t.profile_id = NEW.profile_id
          AND t.ingress_request_id = NEW.ingress_request_id
		  AND NEW.created_at < t.cutoff
    ) THEN
        NEW.audit_extension_payload := NULL;
        NEW.extension_state := 'omitted';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_runtime_telemetry_outbox_late_retention_guard
    BEFORE INSERT OR UPDATE ON public.runtime_telemetry_outbox
    FOR EACH ROW EXECUTE FUNCTION public.prism_guard_late_audit_outbox();

-- Currency migration drafts were introduced by the Pricing owner before the
-- Settings cutover.  The original additive tables intentionally kept the
-- chunk payload out of the header, but did not yet persist the bounded row
-- body needed to resume an upload after a lost response.  Complete that
-- storage contract here; the payload contains only canonical price rows and
-- never secrets.
ALTER TABLE public.pricing_currency_migration_draft_chunks
    ADD COLUMN IF NOT EXISTS payload jsonb NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE public.pricing_currency_migration_draft_chunks
    ADD CONSTRAINT ck_pcmdc_payload_array
    CHECK (jsonb_typeof(payload) = 'array' AND jsonb_array_length(payload) = row_count)
    NOT VALID;

ALTER TABLE public.pricing_currency_migration_draft_items
    ADD COLUMN IF NOT EXISTS template_name text NOT NULL DEFAULT '';
ALTER TABLE public.pricing_currency_migration_draft_items
    ADD COLUMN IF NOT EXISTS reference_count bigint NOT NULL DEFAULT 0;
ALTER TABLE public.pricing_currency_migration_draft_items
    ADD CONSTRAINT ck_pcmdi_reference_count_nonneg CHECK (reference_count >= 0) NOT VALID;
ALTER TABLE public.pricing_currency_migration_drafts
    ADD COLUMN IF NOT EXISTS expires_at timestamp with time zone NOT NULL DEFAULT (now() + interval '24 hours');

-- A draft header is mutable only through the finite upload/seal/commit/expiry
-- state machine.  The generic Pricing evidence trigger installed in 000008
-- is intentionally stricter for immutable inventory tables, so replace it
-- for this one stateful coordination header while keeping chunks/items
-- append-only below.
CREATE OR REPLACE FUNCTION public.prism_currency_migration_draft_state_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'pricing_currency_migration_drafts are not deletable' USING ERRCODE = '55000';
    END IF;
    IF NEW.draft_id IS DISTINCT FROM OLD.draft_id
       OR NEW.profile_id IS DISTINCT FROM OLD.profile_id
       OR NEW.migration_operation_id IS DISTINCT FROM OLD.migration_operation_id
       OR NEW.operation_kind IS DISTINCT FROM OLD.operation_kind
       OR NEW.target_currency_code IS DISTINCT FROM OLD.target_currency_code
       OR NEW.target_currency_symbol IS DISTINCT FROM OLD.target_currency_symbol
       OR NEW.expected_inventory_id IS DISTINCT FROM OLD.expected_inventory_id
       OR NEW.expected_inventory_hash IS DISTINCT FROM OLD.expected_inventory_hash
       OR NEW.expected_inventory_generation IS DISTINCT FROM OLD.expected_inventory_generation
       OR NEW.expected_reporting_currency_epoch IS DISTINCT FROM OLD.expected_reporting_currency_epoch
       OR NEW.expected_settings_updated_at IS DISTINCT FROM OLD.expected_settings_updated_at
       OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
       OR NEW.normalized_header_hash IS DISTINCT FROM OLD.normalized_header_hash
       OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'pricing currency migration draft identity is immutable' USING ERRCODE = '55000';
    END IF;
    IF OLD.status = 'uploading' AND NEW.status IN ('uploading', 'sealed', 'expired') THEN
        IF NEW.status = 'uploading'
           AND (NEW.draft_hash IS DISTINCT FROM OLD.draft_hash
                OR NEW.template_count IS DISTINCT FROM OLD.template_count
                OR NEW.committed_result_operation_id IS DISTINCT FROM OLD.committed_result_operation_id) THEN
            RAISE EXCEPTION 'uploading draft content cannot be sealed fields' USING ERRCODE = '55000';
        END IF;
        RETURN NEW;
    END IF;
    IF OLD.status = 'sealed' AND NEW.status = 'committed' THEN
        IF OLD.draft_hash IS NULL OR OLD.template_count IS NULL
           OR NEW.draft_hash IS DISTINCT FROM OLD.draft_hash
           OR NEW.template_count IS DISTINCT FROM OLD.template_count
           OR NEW.received_chunk_count IS DISTINCT FROM OLD.received_chunk_count
           OR NEW.committed_result_operation_id IS NULL THEN
            RAISE EXCEPTION 'sealed draft commit transition is invalid' USING ERRCODE = '55000';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.status IS NOT DISTINCT FROM OLD.status
       AND NEW.received_chunk_count IS NOT DISTINCT FROM OLD.received_chunk_count
       AND NEW.draft_hash IS NOT DISTINCT FROM OLD.draft_hash
       AND NEW.template_count IS NOT DISTINCT FROM OLD.template_count
       AND NEW.committed_result_operation_id IS NOT DISTINCT FROM OLD.committed_result_operation_id
       AND NEW.updated_at IS DISTINCT FROM OLD.updated_at THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'invalid pricing currency migration draft transition % -> %', OLD.status, NEW.status USING ERRCODE = '55000';
END;
$$;

DROP TRIGGER IF EXISTS pricing_currency_migration_drafts_append_only ON public.pricing_currency_migration_drafts;
CREATE TRIGGER pricing_currency_migration_drafts_state_guard
    BEFORE UPDATE OR DELETE ON public.pricing_currency_migration_drafts
    FOR EACH ROW EXECUTE FUNCTION public.prism_currency_migration_draft_state_guard();
CREATE TRIGGER pricing_currency_migration_draft_chunks_append_only
    BEFORE UPDATE OR DELETE ON public.pricing_currency_migration_draft_chunks
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_migration_evidence_append_only();
CREATE TRIGGER pricing_currency_migration_draft_items_append_only
    BEFORE UPDATE OR DELETE ON public.pricing_currency_migration_draft_items
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_migration_evidence_append_only();

-- ============================================================================
-- Part 14: migration history (frozen manifest markers; UXM-008 is a marker
-- with NO SQL file, CI/startup assert this)
-- ============================================================================

CREATE TABLE public.prism_migration_history (
    id bigint GENERATED ALWAYS AS IDENTITY,
    history_identity text NOT NULL,
    logical_slot text NOT NULL,
    physical_version text NOT NULL,
    owner text NOT NULL,
    kind text NOT NULL,
    filename_or_marker text NOT NULL,
    content_hash text NOT NULL,
    recorded_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_prism_migration_history PRIMARY KEY (id),
    CONSTRAINT uq_prism_migration_history_slot UNIQUE (history_identity, logical_slot)
);

-- Retained prefix through UXM-007 (the settings additive slot). The explicit
-- finalizer appends UXM-008 in a later transaction; no ordinary SQL file
-- exists for it.
INSERT INTO public.prism_migration_history (
    history_identity, logical_slot, physical_version, owner, kind, filename_or_marker, content_hash, recorded_at
) VALUES
    ('uxm_slot', 'UXM-003', '000004', 'Pricing', 'migration', '000004_pricing_cost_trust_additive.sql', 'retained', now()),
    ('uxm_slot', 'UXM-004', '000005', 'Pricing', 'migration', '000005_pricing_cost_trust_finalize.sql', 'retained', now()),
    ('uxm_slot', 'UXM-005', '000006', 'RequestsAudit', 'migration', '000006_request_logs_audit_observability.sql', 'retained', now()),
    ('uxm_slot', 'UXM-006', '000007', 'RequestsAudit', 'migration', '000007_audit_bytea_budgets.sql', 'retained', now()),
    ('uxm_slot', 'UXM-007', '000015', 'Settings', 'migration', '000015_settings_safety_additive.sql', 'retained', now())
ON CONFLICT (history_identity, logical_slot) DO NOTHING;

-- ============================================================================
-- Part 15: legacy scheduler DB fence trigger (installed last)
-- ============================================================================

DROP TRIGGER IF EXISTS trg_management_jobs_log_retention_guard ON public.management_jobs;
DROP TRIGGER IF EXISTS trg_management_jobs_log_retention_guard_write ON public.management_jobs;
DROP TRIGGER IF EXISTS trg_management_jobs_log_retention_guard_delete ON public.management_jobs;
-- INSERT/UPDATE may reference NEW; DELETE only OLD (SQLSTATE 42P17 forbids NEW
-- in a DELETE trigger's WHEN clause), so the guard is split into two triggers.
CREATE TRIGGER trg_management_jobs_log_retention_guard_write
    BEFORE INSERT OR UPDATE ON public.management_jobs
    FOR EACH ROW
    WHEN (pg_trigger_depth() = 0 AND NEW.type = 'log_retention')
    EXECUTE FUNCTION public.prism_guard_log_retention_job();
CREATE TRIGGER trg_management_jobs_log_retention_guard_delete
    BEFORE DELETE ON public.management_jobs
    FOR EACH ROW
    WHEN (pg_trigger_depth() = 0 AND OLD.type = 'log_retention')
    EXECUTE FUNCTION public.prism_guard_log_retention_job();
