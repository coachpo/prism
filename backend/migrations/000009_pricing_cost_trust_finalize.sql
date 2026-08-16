-- 000009 pricing cost trust: finalize.
--
-- Finalizes the pricing cost-trust schema after 000003's additive schema and
-- deterministic backfills. Runs in the same single pre-startup transaction as
-- 000003; it hard-rejects (rolling the whole upgrade back) unless every
-- finalization gate passes: schema transition singleton is `finalize_ready`,
-- generation lease acquisition is closed, no active generation lease exists,
-- and the three final projections (unresolved inventory issues, unresolved
-- telemetry quarantine, unarchived legacy FX sources) are all zero.
--
-- When the gates pass this migration:
--   1. takes the exclusive finalizer owner/token on the singleton;
--   2. promotes NOT NULL and validates the pricing CHECK constraints on
--      user_settings, request_logs and usage_request_events (parents and all
--      existing partitions);
--   3. renames the transitional v2 currency columns to the canonical final
--      names and drops the legacy raw currency columns;
--   4. drops the legacy mutable pricing_templates columns;
--   5. recursively drops billable_flag/priced_flag from the partitioned
--      request/usage parents and every partition;
--   6. takes ACCESS EXCLUSIVE on endpoint_fx_rate_settings, re-proves
--      unarchived_legacy_fx_source_count = 0 and drops the table;
--   7. sets the singleton phase to `final` and clears the finalizer owner.
--
-- The strict template pointer/epoch invariants installed by 000003 remain in
-- force; the single-transaction runner leaves no committed transitional
-- window, so the legacy-invalid null-pointer exception never materializes.

-- ============================================================================
-- Part 1: finalizer ownership and gate preflight (SPEC 6.3.3 / 11.1)
-- ============================================================================

-- Re-prove the three final projections first so a rejected upgrade reports
-- the exact typed blocker (SPEC 11.1 / 6.3.1 / 6.3.2).
DO $$
DECLARE
    unresolved_inventory bigint;
    unresolved_quarantine bigint;
    unarchived_fx bigint;
BEGIN
    SELECT count(*) INTO unresolved_inventory
    FROM public.pricing_migration_inventories AS head
    WHERE NOT EXISTS (
        SELECT 1 FROM public.pricing_migration_inventories AS successor
        WHERE successor.supersedes_inventory_id = head.inventory_id
    )
    AND head.issue_codes && ARRAY['foreign_currency_template','live_fx_dependency',
        'unsupported_pricing_unit','invalid_price_encoding',
        'invalid_reporting_currency_code','invalid_reporting_currency_symbol'];

    SELECT count(*) INTO unresolved_quarantine
    FROM public.pricing_telemetry_quarantine AS quarantine
    WHERE NOT EXISTS (
        SELECT 1 FROM public.pricing_telemetry_quarantine_resolutions AS resolution
        WHERE resolution.quarantine_id = quarantine.quarantine_id
    );

    SELECT count(*) INTO unarchived_fx
    FROM public.endpoint_fx_rate_settings AS fx
    WHERE NOT EXISTS (
        SELECT 1
        FROM public.currency_migration_ledger AS ledger
        JOIN public.pricing_migration_inventories AS inventory ON inventory.inventory_id = ledger.inventory_id
        JOIN public.currency_migration_legacy_fx_evidence AS evidence ON evidence.inventory_id = inventory.inventory_id
        JOIN public.currency_migration_legacy_fx_assessments AS assessment ON assessment.legacy_fx_evidence_id = evidence.legacy_fx_evidence_id
        JOIN public.pricing_migration_inventories AS successor ON successor.supersedes_inventory_id = inventory.inventory_id
        WHERE ledger.operation_kind IN ('currency_cutover','repair_same_currency','archive_unused_fx')
          AND evidence.source_fx_row_id = fx.id AND evidence.profile_id = fx.profile_id
          AND assessment.attribution <> 'unknown'
          AND successor.issue_codes = '{}'
    );

    IF unresolved_inventory > 0 THEN
        RAISE EXCEPTION '000009 rejected: unresolved_inventory_count=% (blocking issues remain)', unresolved_inventory
            USING ERRCODE = 'P0001';
    END IF;
    IF unresolved_quarantine > 0 THEN
        RAISE EXCEPTION '000009 rejected: unresolved_quarantine_count=% (invalid final HTTP status telemetry must be repaired first)', unresolved_quarantine
            USING ERRCODE = 'P0001';
    END IF;
    IF unarchived_fx > 0 THEN
        RAISE EXCEPTION '000009 rejected: unarchived_legacy_fx_source_count=% (legacy FX rows are not fully covered by accepted migration ledgers)', unarchived_fx
            USING ERRCODE = 'P0001';
    END IF;
END;
$$;

-- Singleton/lease preflight and exclusive finalizer owner acquisition.
DO $$
DECLARE
    phase text;
    acquisition_open boolean;
    lease_count bigint;
    migration_time timestamptz;
BEGIN
    migration_time := clock_timestamp();
    SELECT transition.phase, transition.lease_acquisition_open
    INTO phase, acquisition_open
    FROM public.pricing_schema_transition_state AS transition
    WHERE transition.id = 1;
    IF phase IS NULL THEN
        RAISE EXCEPTION '000009 rejected: pricing schema transition singleton is missing'
            USING ERRCODE = 'P0001';
    END IF;
    IF phase <> 'finalize_ready' THEN
        RAISE EXCEPTION '000009 rejected: schema transition phase is %; finalization requires finalize_ready - resolve blocking inventory/quarantine/FX issues and re-run the upgrade', phase
            USING ERRCODE = 'P0001';
    END IF;
    IF acquisition_open THEN
        RAISE EXCEPTION '000009 rejected: generation lease acquisition is still open'
            USING ERRCODE = 'P0001';
    END IF;
    SELECT count(*) INTO lease_count
    FROM public.pricing_schema_generation_leases
    WHERE released_at IS NULL;
    IF lease_count > 0 THEN
        RAISE EXCEPTION '000009 rejected: % active schema generation lease(s) exist', lease_count
            USING ERRCODE = 'P0001';
    END IF;
    -- Acquire the unique finalizer owner/token for this finalizing process.
    UPDATE public.pricing_schema_transition_state
    SET finalizer_owner_id = gen_random_uuid(),
        finalizer_expires_at = migration_time + interval '1 hour',
        finalizer_fencing_token = finalizer_fencing_token + 1,
        updated_at = migration_time
    WHERE id = 1;
END;
$$;

-- Re-verify template pointer and epoch invariants explicitly so a violation
-- surfaces as a precise migration error before destructive steps (the
-- deferred triggers enforce the same invariants at commit).
DO $$
DECLARE
    bad_pointer integer;
    bad_epoch integer;
BEGIN
    SELECT count(*) INTO bad_pointer
    FROM public.pricing_templates AS templates
    WHERE templates.deleted_at IS NULL
      AND templates.current_revision_id IS DISTINCT FROM (
          SELECT revisions.id
          FROM public.pricing_template_revisions AS revisions
          WHERE revisions.template_id = templates.id
          ORDER BY revisions.version DESC
          LIMIT 1
      );
    IF bad_pointer > 0 THEN
        RAISE EXCEPTION '000009 rejected: % active template(s) violate the current-revision pointer invariant', bad_pointer
            USING ERRCODE = 'P0001';
    END IF;
    SELECT count(*) INTO bad_epoch
    FROM public.user_settings AS settings
    WHERE settings.current_reporting_currency_epoch_id IS NOT NULL
      AND NOT EXISTS (
          SELECT 1 FROM public.reporting_currency_epochs AS epochs
          WHERE epochs.id = settings.current_reporting_currency_epoch_id
            AND epochs.profile_id = settings.profile_id
            AND epochs.superseded_at IS NULL
      );
    IF bad_epoch > 0 THEN
        RAISE EXCEPTION '000009 rejected: % settings row(s) point at a missing or superseded epoch', bad_epoch
            USING ERRCODE = 'P0001';
    END IF;
END;
$$;

-- ============================================================================
-- Part 2: NOT NULL promotion and constraint validation (SPEC 11.1 step 2)
-- ============================================================================

-- Fire and clear every deferred constraint trigger pending from 000003's
-- backfills (pointer/epoch/revision/inventory invariants are consistent at
-- this point because the gates passed); PostgreSQL refuses ALTER TABLE on a
-- table with pending trigger events.
SET CONSTRAINTS ALL IMMEDIATE;

ALTER TABLE public.user_settings
    ALTER COLUMN pricing_migration_state SET NOT NULL,
    ALTER COLUMN current_reporting_currency_epoch_id SET NOT NULL,
    ALTER COLUMN pricing_report_currency_code_v2 SET NOT NULL,
    ALTER COLUMN pricing_report_currency_symbol_v2 SET NOT NULL;

ALTER TABLE public.user_settings
    VALIDATE CONSTRAINT ck_us_pricing_migration_state,
    VALIDATE CONSTRAINT ck_us_migration_issues,
    VALIDATE CONSTRAINT ck_us_ready_has_no_issues,
    VALIDATE CONSTRAINT ck_us_v2_code_canonical,
    VALIDATE CONSTRAINT ck_us_v2_symbol_valid,
    VALIDATE CONSTRAINT ck_us_template_generation_nonneg,
    VALIDATE CONSTRAINT ck_us_reference_generation_nonneg;

-- Partitioned status columns: parent SET NOT NULL and VALIDATE recurse to
-- every existing partition (PostgreSQL 16), so only parent operations run.

ALTER TABLE public.request_logs
    ALTER COLUMN pricing_status SET NOT NULL,
    ALTER COLUMN pricing_evidence_trust SET NOT NULL;

ALTER TABLE public.usage_request_events
    ALTER COLUMN pricing_status SET NOT NULL,
    ALTER COLUMN pricing_evidence_trust SET NOT NULL;

ALTER TABLE public.request_logs
    VALIDATE CONSTRAINT pricing_status_check,
    VALIDATE CONSTRAINT pricing_evidence_trust_check,
    VALIDATE CONSTRAINT pricing_unknown_requires_untrusted_check,
    VALIDATE CONSTRAINT pricing_trusted_requires_known_check,
    VALIDATE CONSTRAINT pricing_unpriced_reason_check,
    VALIDATE CONSTRAINT pricing_resolution_kind_check,
    VALIDATE CONSTRAINT pricing_missing_components_check,
    VALIDATE CONSTRAINT pricing_epoch_nonneg_check,
    VALIDATE CONSTRAINT pricing_costs_coherence_check;

ALTER TABLE public.usage_request_events
    VALIDATE CONSTRAINT pricing_status_check,
    VALIDATE CONSTRAINT pricing_evidence_trust_check,
    VALIDATE CONSTRAINT pricing_unknown_requires_untrusted_check,
    VALIDATE CONSTRAINT pricing_trusted_requires_known_check,
    VALIDATE CONSTRAINT pricing_unpriced_reason_check,
    VALIDATE CONSTRAINT pricing_resolution_kind_check,
    VALIDATE CONSTRAINT pricing_missing_components_check,
    VALIDATE CONSTRAINT pricing_epoch_nonneg_check,
    VALIDATE CONSTRAINT pricing_costs_coherence_check;

-- ============================================================================
-- Part 3: canonical currency column rename (SPEC 11.1 step 4)
-- ============================================================================

ALTER TABLE public.user_settings DROP COLUMN report_currency_code;
ALTER TABLE public.user_settings DROP COLUMN report_currency_symbol;
ALTER TABLE public.user_settings RENAME COLUMN pricing_report_currency_code_v2 TO report_currency_code;
ALTER TABLE public.user_settings RENAME COLUMN pricing_report_currency_symbol_v2 TO report_currency_symbol;

-- Recreate the settings/epoch coherence trigger with the canonical final
-- column names; its PL/pgSQL body referenced the transitional v2 columns.
CREATE OR REPLACE FUNCTION public.prism_pricing_settings_epoch_coherence()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    row record;
    epoch_code text;
    epoch_symbol text;
    epoch_active boolean;
BEGIN
    FOR row IN
        SELECT settings.profile_id,
               settings.current_reporting_currency_epoch_id,
               settings.report_currency_code,
               settings.report_currency_symbol,
               settings.pricing_migration_state,
               settings.legacy_migration_issues
        FROM public.user_settings AS settings
    LOOP
        IF row.current_reporting_currency_epoch_id IS NOT NULL THEN
            SELECT epochs.currency_code, epochs.currency_symbol, epochs.superseded_at IS NULL
            INTO epoch_code, epoch_symbol, epoch_active
            FROM public.reporting_currency_epochs AS epochs
            WHERE epochs.id = row.current_reporting_currency_epoch_id;
            IF NOT FOUND OR NOT epoch_active THEN
                RAISE EXCEPTION 'pricing_settings_epoch_coherence: profile % points at a non-active epoch', row.profile_id
                    USING ERRCODE = 'P0001';
            END IF;
            IF epoch_code IS DISTINCT FROM row.report_currency_code
               OR epoch_symbol IS DISTINCT FROM row.report_currency_symbol THEN
                RAISE EXCEPTION 'pricing_settings_epoch_coherence: profile % settings currency diverges from active epoch', row.profile_id
                    USING ERRCODE = 'P0001';
            END IF;
        ELSE
            IF row.report_currency_code IS NOT NULL OR row.report_currency_symbol IS NOT NULL THEN
                RAISE EXCEPTION 'pricing_settings_epoch_coherence: profile % has canonical currency without an active epoch pointer', row.profile_id
                    USING ERRCODE = 'P0001';
            END IF;
            IF row.pricing_migration_state = 'ready' THEN
                RAISE EXCEPTION 'pricing_settings_epoch_coherence: ready profile % has no active epoch', row.profile_id
                    USING ERRCODE = 'P0001';
            END IF;
            IF NOT (row.legacy_migration_issues @> ARRAY['invalid_reporting_currency_code']::text[]
                    OR row.legacy_migration_issues @> ARRAY['invalid_reporting_currency_symbol']::text[]) THEN
                RAISE EXCEPTION 'pricing_settings_epoch_coherence: profile % has no epoch and no typed invalid currency evidence', row.profile_id
                    USING ERRCODE = 'P0001';
            END IF;
        END IF;
    END LOOP;
    RETURN NULL;
END;
$$;

-- ============================================================================
-- Part 4: drop legacy mutable pricing template columns (SPEC 11.1 step 4)
-- ============================================================================

ALTER TABLE public.pricing_templates
    DROP COLUMN pricing_unit,
    DROP COLUMN pricing_currency_code,
    DROP COLUMN input_price,
    DROP COLUMN output_price,
    DROP COLUMN cached_input_price,
    DROP COLUMN cache_creation_price,
    DROP COLUMN reasoning_price,
    DROP COLUMN version;

-- ============================================================================
-- Part 5: drop legacy billable/priced flags from the partitioned parents
-- (recurses to every existing partition; SPEC 6.5 / 11.1 step 4)
-- ============================================================================

ALTER TABLE public.request_logs DROP COLUMN billable_flag, DROP COLUMN priced_flag;
ALTER TABLE public.usage_request_events DROP COLUMN billable_flag, DROP COLUMN priced_flag;

-- ============================================================================
-- Part 6: legacy FX cutoff (SPEC 11.1 step 5 / 11.5 step 7)
-- ============================================================================

-- Take ACCESS EXCLUSIVE inside the migration transaction, re-prove that every
-- source row is covered by an accepted ledger with a clean successor head,
-- then drop the active FX table. Ledger and request/usage FX snapshots remain
-- read-only evidence; nothing cascades.
LOCK TABLE public.endpoint_fx_rate_settings IN ACCESS EXCLUSIVE MODE;

DO $$
DECLARE
    unarchived_fx bigint;
BEGIN
    SELECT count(*) INTO unarchived_fx
    FROM public.endpoint_fx_rate_settings AS fx
    WHERE NOT EXISTS (
        SELECT 1
        FROM public.currency_migration_ledger AS ledger
        JOIN public.pricing_migration_inventories AS inventory ON inventory.inventory_id = ledger.inventory_id
        JOIN public.currency_migration_legacy_fx_evidence AS evidence ON evidence.inventory_id = inventory.inventory_id
        JOIN public.currency_migration_legacy_fx_assessments AS assessment ON assessment.legacy_fx_evidence_id = evidence.legacy_fx_evidence_id
        JOIN public.pricing_migration_inventories AS successor ON successor.supersedes_inventory_id = inventory.inventory_id
        WHERE ledger.operation_kind IN ('currency_cutover','repair_same_currency','archive_unused_fx')
          AND evidence.source_fx_row_id = fx.id AND evidence.profile_id = fx.profile_id
          AND assessment.attribution <> 'unknown'
          AND successor.issue_codes = '{}'
    );
    IF unarchived_fx > 0 THEN
        RAISE EXCEPTION '000009 rejected: unarchived_legacy_fx_source_count=% under ACCESS EXCLUSIVE lock', unarchived_fx
            USING ERRCODE = 'P0001';
    END IF;
END;
$$;

DROP TABLE public.endpoint_fx_rate_settings;

-- ============================================================================
-- Part 7: final singleton transition (SPEC 6.3.3 / 11.1)
-- ============================================================================

UPDATE public.pricing_schema_transition_state
SET phase = 'final',
    finalizer_owner_id = NULL,
    updated_at = clock_timestamp()
WHERE id = 1;
