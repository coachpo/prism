-- 000023_pricing_template_kind_cards
-- Destructive, fresh-only pricing-template shape. Existing pricing and
-- currency-migration state must be rebuilt before this migration is applied.
-- A price revision owns an explicit kind and immutable role-keyed cards.
-- Peak windows are user-authored, revision-owned, half-open wall-clock
-- intervals; no vendor-specific defaults or pricing knowledge live here.

DO $$
DECLARE
    table_name text;
    row_count bigint;
BEGIN
    FOREACH table_name IN ARRAY ARRAY[
        'pricing_templates',
        'pricing_template_revisions',
        'pricing_migration_inventories',
        'currency_migration_legacy_fx_evidence',
        'currency_migration_legacy_fx_assessments',
        'currency_migration_legacy_fx_dependencies',
        'pricing_migration_legacy_template_evidence',
        'pricing_migration_legacy_reporting_currency_evidence',
        'pricing_telemetry_quarantine',
        'pricing_telemetry_quarantine_resolutions',
        'pricing_mutation_operation_reservations',
        'pricing_mutation_operations',
        'pricing_mutation_result_items',
        'pricing_currency_migration_drafts',
        'pricing_currency_migration_draft_chunks',
        'pricing_currency_migration_draft_items',
        'currency_migration_ledger',
        'currency_migration_ledger_items'
    ] LOOP
        EXECUTE format('SELECT count(*) FROM public.%I', table_name) INTO row_count;
        IF row_count > 0 THEN
            RAISE EXCEPTION
                'pricing template rebuild required before 000023: public.% has % retained rows; export or discard the instance and restart with an empty database',
                table_name, row_count
                USING ERRCODE = 'P0001', HINT = 'This migration is fresh-only and does not backfill old pricing or currency-migration state';
        END IF;
    END LOOP;
END;
$$;

-- The old revision price columns are replaced by pricing_template_cards.
ALTER TABLE public.pricing_template_revisions
    ADD COLUMN template_kind character varying(16),
    ADD COLUMN pricing_schedule_timezone character varying(100),
    ADD COLUMN pricing_schedule_digest character varying(64);

ALTER TABLE public.pricing_template_revisions
    DROP CONSTRAINT ck_ptr_input_price,
    DROP CONSTRAINT ck_ptr_output_price,
    DROP CONSTRAINT ck_ptr_cached_input_price,
    DROP CONSTRAINT ck_ptr_cache_creation_price,
    DROP CONSTRAINT ck_ptr_reasoning_price,
    DROP CONSTRAINT ck_ptr_tier_input_price,
    DROP CONSTRAINT ck_ptr_tier_output_price,
    DROP CONSTRAINT ck_ptr_tier_cached_input_price,
    DROP CONSTRAINT ck_ptr_tier_cache_creation_price,
    DROP CONSTRAINT ck_ptr_tier_reasoning_price,
    DROP CONSTRAINT ck_ptr_tier_all_or_none,
    DROP CONSTRAINT ck_ptr_tier_specialty_parity;

ALTER TABLE public.pricing_template_revisions
    DROP COLUMN input_price,
    DROP COLUMN output_price,
    DROP COLUMN cached_input_price,
    DROP COLUMN cache_creation_price,
    DROP COLUMN reasoning_price,
    DROP COLUMN tier_input_price,
    DROP COLUMN tier_output_price,
    DROP COLUMN tier_cached_input_price,
    DROP COLUMN tier_cache_creation_price,
    DROP COLUMN tier_reasoning_price;

ALTER TABLE public.pricing_template_revisions
    ALTER COLUMN template_kind SET NOT NULL,
    ADD CONSTRAINT ck_ptr_template_kind
        CHECK (template_kind IN ('standard', 'tiered', 'peak_valley')),
    ADD CONSTRAINT ck_ptr_tier_threshold_scope
        CHECK ((template_kind = 'tiered') = (tier_input_tokens_above IS NOT NULL)),
    ADD CONSTRAINT ck_ptr_schedule_scope
        CHECK (
            (template_kind = 'peak_valley'
                AND pricing_schedule_timezone IS NOT NULL
                AND btrim(pricing_schedule_timezone) <> ''
                AND btrim(pricing_schedule_timezone) <> 'Local'
                AND pricing_schedule_digest IS NOT NULL)
            OR (template_kind <> 'peak_valley'
                AND pricing_schedule_timezone IS NULL
                AND pricing_schedule_digest IS NULL)
        ),
    ADD CONSTRAINT uq_ptr_id_kind UNIQUE (id, template_kind);

CREATE TABLE public.pricing_template_cards (
    revision_id          bigint NOT NULL,
    template_kind        character varying(16) NOT NULL,
    card_role            character varying(16) NOT NULL,
    input_price          character varying(20) NOT NULL,
    output_price         character varying(20) NOT NULL,
    cached_input_price   character varying(20),
    cache_creation_price character varying(20),
    reasoning_price      character varying(20),
    CONSTRAINT pk_pricing_template_cards PRIMARY KEY (revision_id, card_role),
    CONSTRAINT ck_ptc_role CHECK (card_role IN ('standard', 'tier_base', 'tier_above', 'peak', 'offpeak')),
    CONSTRAINT ck_ptc_kind_role CHECK (
        (template_kind = 'standard' AND card_role = 'standard')
        OR (template_kind = 'tiered' AND card_role IN ('tier_base', 'tier_above'))
        OR (template_kind = 'peak_valley' AND card_role IN ('peak', 'offpeak'))
    ),
    CONSTRAINT ck_ptc_input_price
        CHECK (prism_pricing_exact_decimal_canonical(input_price) = input_price),
    CONSTRAINT ck_ptc_output_price
        CHECK (prism_pricing_exact_decimal_canonical(output_price) = output_price),
    CONSTRAINT ck_ptc_cached_input_price
        CHECK (cached_input_price IS NULL OR prism_pricing_exact_decimal_canonical(cached_input_price) = cached_input_price),
    CONSTRAINT ck_ptc_cache_creation_price
        CHECK (cache_creation_price IS NULL OR prism_pricing_exact_decimal_canonical(cache_creation_price) = cache_creation_price),
    CONSTRAINT ck_ptc_reasoning_price
        CHECK (reasoning_price IS NULL OR prism_pricing_exact_decimal_canonical(reasoning_price) = reasoning_price),
    CONSTRAINT pricing_template_cards_revision_kind_fkey
        FOREIGN KEY (revision_id, template_kind)
        REFERENCES public.pricing_template_revisions(id, template_kind)
        ON DELETE RESTRICT
);

CREATE INDEX idx_pricing_template_cards_revision
    ON public.pricing_template_cards (revision_id);

CREATE TABLE public.pricing_template_windows (
    id             bigint NOT NULL GENERATED ALWAYS AS IDENTITY,
    revision_id    bigint NOT NULL,
    weekday_mask   smallint NOT NULL,
    start_minute   smallint NOT NULL,
    end_minute     smallint NOT NULL,
    created_at     timestamp with time zone NOT NULL,
    CONSTRAINT pk_pricing_template_windows PRIMARY KEY (id),
    CONSTRAINT ck_ptw_weekday_mask CHECK (weekday_mask BETWEEN 1 AND 127),
    CONSTRAINT ck_ptw_start_minute CHECK (start_minute BETWEEN 0 AND 1439),
    CONSTRAINT ck_ptw_end_minute CHECK (end_minute BETWEEN 1 AND 2880),
    CONSTRAINT ck_ptw_span CHECK (end_minute > start_minute AND end_minute - start_minute <= 1440),
    CONSTRAINT uq_ptw_shape UNIQUE (revision_id, weekday_mask, start_minute, end_minute),
    CONSTRAINT pricing_template_windows_revision_fkey
        FOREIGN KEY (revision_id) REFERENCES public.pricing_template_revisions(id) ON DELETE RESTRICT
);

CREATE INDEX idx_pricing_template_windows_revision
    ON public.pricing_template_windows (revision_id, weekday_mask, start_minute, end_minute);

-- Stable digest of the normalized (sorted, duplicate-free) window shape. The
-- Go authoring path must use the same bytes: "mask,start,end" joined by LF,
-- with no trailing LF, then SHA-256 hex.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE FUNCTION public.prism_pricing_template_windows_digest(target_revision_id bigint)
RETURNS text
LANGUAGE sql
STABLE
AS $$
    SELECT encode(digest(convert_to(COALESCE(string_agg(
        format('%s,%s,%s', normalized.weekday_mask, normalized.start_minute, normalized.end_minute),
        E'\n' ORDER BY normalized.weekday_mask, normalized.start_minute, normalized.end_minute
    ), ''), 'UTF8'), 'sha256'), 'hex')
    FROM (
        SELECT DISTINCT weekday_mask, start_minute, end_minute
        FROM public.pricing_template_windows
        WHERE revision_id = target_revision_id
    ) AS normalized;
$$;

CREATE FUNCTION public.prism_pricing_revision_shape_check(target_revision_id bigint)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    revision_kind text;
    threshold integer;
    timezone_name text;
    expected_digest text;
    actual_digest text;
    card_count integer;
    window_count integer;
    actual_roles text[];
    expected_roles text[];
    specialty_shape_count integer;
BEGIN
    SELECT template_kind, tier_input_tokens_above, pricing_schedule_timezone, pricing_schedule_digest
      INTO revision_kind, threshold, timezone_name, expected_digest
      FROM public.pricing_template_revisions
     WHERE id = target_revision_id;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'pricing template shape guard: revision % does not exist', target_revision_id
            USING ERRCODE = 'P0001';
    END IF;

    SELECT count(*), array_agg(card_role ORDER BY card_role)
      INTO card_count, actual_roles
      FROM public.pricing_template_cards
     WHERE revision_id = target_revision_id;

    expected_roles := CASE revision_kind
        WHEN 'standard' THEN ARRAY['standard']::text[]
        WHEN 'tiered' THEN ARRAY['tier_above', 'tier_base']::text[]
        WHEN 'peak_valley' THEN ARRAY['offpeak', 'peak']::text[]
        ELSE ARRAY[]::text[]
    END;

    IF actual_roles IS DISTINCT FROM expected_roles THEN
        RAISE EXCEPTION 'pricing template shape guard: revision % kind % requires roles %, got %',
            target_revision_id, revision_kind, expected_roles, actual_roles
            USING ERRCODE = 'P0001';
    END IF;

    SELECT count(DISTINCT format('%s:%s:%s',
        CASE WHEN cached_input_price IS NULL THEN '1' ELSE '0' END,
        CASE WHEN cache_creation_price IS NULL THEN '1' ELSE '0' END,
        CASE WHEN reasoning_price IS NULL THEN '1' ELSE '0' END))
      INTO specialty_shape_count
      FROM public.pricing_template_cards
     WHERE revision_id = target_revision_id;
    IF specialty_shape_count <> 1 THEN
        RAISE EXCEPTION 'pricing template shape guard: revision % cards have mismatched specialty NULL shape', target_revision_id
            USING ERRCODE = 'P0001';
    END IF;

    SELECT count(*) INTO window_count
      FROM public.pricing_template_windows
     WHERE revision_id = target_revision_id;

    IF revision_kind = 'peak_valley' THEN
        IF window_count < 1 OR window_count > 32 THEN
            RAISE EXCEPTION 'pricing template shape guard: peak_valley revision % requires 1..32 windows, got %', target_revision_id, window_count
                USING ERRCODE = 'P0001';
        END IF;
        actual_digest := public.prism_pricing_template_windows_digest(target_revision_id);
        IF expected_digest IS NULL OR actual_digest IS DISTINCT FROM expected_digest THEN
            RAISE EXCEPTION 'pricing template shape guard: revision % window digest mismatch', target_revision_id
                USING ERRCODE = 'P0001';
        END IF;
    ELSE
        IF window_count <> 0 OR timezone_name IS NOT NULL OR expected_digest IS NOT NULL THEN
            RAISE EXCEPTION 'pricing template shape guard: non-peak revision % cannot carry schedule data', target_revision_id
                USING ERRCODE = 'P0001';
        END IF;
    END IF;

    IF revision_kind = 'tiered' AND (threshold IS NULL OR threshold < 1) THEN
        RAISE EXCEPTION 'pricing template shape guard: tiered revision % has invalid threshold', target_revision_id
            USING ERRCODE = 'P0001';
    END IF;

    RETURN;
END;
$$;

CREATE FUNCTION public.prism_pricing_revision_shape_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM public.prism_pricing_revision_shape_check(NEW.id);
    RETURN NULL;
END;
$$;

CREATE FUNCTION public.prism_pricing_revision_child_shape_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM public.prism_pricing_revision_shape_check(NEW.revision_id);
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER pricing_template_revision_shape_guard
    AFTER INSERT ON public.pricing_template_revisions
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_revision_shape_guard();

CREATE CONSTRAINT TRIGGER pricing_template_card_shape_guard
    AFTER INSERT ON public.pricing_template_cards
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_revision_child_shape_guard();

CREATE CONSTRAINT TRIGGER pricing_template_window_shape_guard
    AFTER INSERT ON public.pricing_template_windows
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_revision_child_shape_guard();

CREATE FUNCTION public.prism_pricing_template_child_append_only()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'pricing template child rows are append-only'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER pricing_template_cards_append_only
    BEFORE UPDATE OR DELETE ON public.pricing_template_cards
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_template_child_append_only();

CREATE TRIGGER pricing_template_windows_append_only
    BEFORE UPDATE OR DELETE ON public.pricing_template_windows
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_template_child_append_only();

-- Currency-migration draft and ledger rows are empty under the fresh-only
-- guard. Their price payload is moved into role-keyed child tables so a future
-- cutover cannot silently discard one card.
ALTER TABLE public.pricing_currency_migration_draft_items
    DROP CONSTRAINT ck_pcmdi_input_price,
    DROP CONSTRAINT ck_pcmdi_output_price,
    DROP CONSTRAINT ck_pcmdi_cached_input_price,
    DROP CONSTRAINT ck_pcmdi_cache_creation_price,
    DROP CONSTRAINT ck_pcmdi_reasoning_price,
    DROP COLUMN input_price,
    DROP COLUMN output_price,
    DROP COLUMN cached_input_price,
    DROP COLUMN cache_creation_price,
    DROP COLUMN reasoning_price,
    ADD COLUMN template_kind character varying(16) NOT NULL,
    ADD CONSTRAINT ck_pcmdi_template_kind CHECK (template_kind IN ('standard', 'tiered', 'peak_valley'));

CREATE TABLE public.pricing_currency_migration_draft_cards (
    draft_id             uuid NOT NULL,
    template_id          integer NOT NULL,
    card_role            character varying(16) NOT NULL,
    input_price          character varying(20) NOT NULL,
    output_price         character varying(20) NOT NULL,
    cached_input_price   character varying(20),
    cache_creation_price character varying(20),
    reasoning_price      character varying(20),
    CONSTRAINT pk_pricing_currency_migration_draft_cards PRIMARY KEY (draft_id, template_id, card_role),
    CONSTRAINT ck_pcmdc_role CHECK (card_role IN ('standard', 'tier_base', 'tier_above', 'peak', 'offpeak')),
    CONSTRAINT ck_pcmdc_input_price CHECK (prism_pricing_exact_decimal_canonical(input_price) = input_price),
    CONSTRAINT ck_pcmdc_output_price CHECK (prism_pricing_exact_decimal_canonical(output_price) = output_price),
    CONSTRAINT ck_pcmdc_cached_input_price CHECK (cached_input_price IS NULL OR prism_pricing_exact_decimal_canonical(cached_input_price) = cached_input_price),
    CONSTRAINT ck_pcmdc_cache_creation_price CHECK (cache_creation_price IS NULL OR prism_pricing_exact_decimal_canonical(cache_creation_price) = cache_creation_price),
    CONSTRAINT ck_pcmdc_reasoning_price CHECK (reasoning_price IS NULL OR prism_pricing_exact_decimal_canonical(reasoning_price) = reasoning_price),
    CONSTRAINT pricing_currency_migration_draft_cards_draft_fkey
        FOREIGN KEY (draft_id) REFERENCES public.pricing_currency_migration_drafts(draft_id) ON DELETE RESTRICT
);

ALTER TABLE public.currency_migration_ledger_items
    DROP CONSTRAINT ck_cmli_input_price,
    DROP CONSTRAINT ck_cmli_output_price,
    DROP CONSTRAINT ck_cmli_cached_input_price,
    DROP CONSTRAINT ck_cmli_cache_creation_price,
    DROP CONSTRAINT ck_cmli_reasoning_price,
    DROP CONSTRAINT uq_cmli_operation_template,
    DROP COLUMN input_price,
    DROP COLUMN output_price,
    DROP COLUMN cached_input_price,
    DROP COLUMN cache_creation_price,
    DROP COLUMN reasoning_price,
    ADD COLUMN template_kind character varying(16) NOT NULL,
    ADD CONSTRAINT ck_cmli_template_kind CHECK (template_kind IN ('standard', 'tiered', 'peak_valley'));

CREATE TABLE public.currency_migration_ledger_cards (
    operation_id         uuid NOT NULL,
    ordinal              integer NOT NULL,
    card_role            character varying(16) NOT NULL,
    input_price          character varying(20) NOT NULL,
    output_price         character varying(20) NOT NULL,
    cached_input_price   character varying(20),
    cache_creation_price character varying(20),
    reasoning_price      character varying(20),
    CONSTRAINT pk_currency_migration_ledger_cards PRIMARY KEY (operation_id, ordinal, card_role),
    CONSTRAINT ck_cmlc_role CHECK (card_role IN ('standard', 'tier_base', 'tier_above', 'peak', 'offpeak')),
    CONSTRAINT ck_cmlc_input_price CHECK (prism_pricing_exact_decimal_canonical(input_price) = input_price),
    CONSTRAINT ck_cmlc_output_price CHECK (prism_pricing_exact_decimal_canonical(output_price) = output_price),
    CONSTRAINT ck_cmlc_cached_input_price CHECK (cached_input_price IS NULL OR prism_pricing_exact_decimal_canonical(cached_input_price) = cached_input_price),
    CONSTRAINT ck_cmlc_cache_creation_price CHECK (cache_creation_price IS NULL OR prism_pricing_exact_decimal_canonical(cache_creation_price) = cache_creation_price),
    CONSTRAINT ck_cmlc_reasoning_price CHECK (reasoning_price IS NULL OR prism_pricing_exact_decimal_canonical(reasoning_price) = reasoning_price),
    CONSTRAINT currency_migration_ledger_cards_item_fkey
        FOREIGN KEY (operation_id, ordinal) REFERENCES public.currency_migration_ledger_items(operation_id, ordinal) ON DELETE RESTRICT
);

-- Replace tier evidence with generic selector evidence on both partitioned
-- parents. NOT VALID avoids a historical partition scan; new rows are still
-- checked immediately.
ALTER TABLE public.request_logs
    DROP CONSTRAINT pricing_tier_evidence_check,
    DROP CONSTRAINT pricing_tier_applied_check,
    DROP COLUMN pricing_tier_applied,
    DROP COLUMN pricing_tier_threshold_tokens,
    DROP COLUMN pricing_tier_basis_tokens,
    ADD COLUMN pricing_template_kind character varying(16),
    ADD COLUMN pricing_selector_threshold_tokens integer,
    ADD COLUMN pricing_selector_basis_tokens bigint,
    ADD COLUMN pricing_selection_state character varying(20),
    ADD COLUMN pricing_card_role character varying(16),
    ADD COLUMN pricing_schedule_decided_at timestamp with time zone,
    ADD COLUMN pricing_schedule_timezone character varying(100),
    ADD COLUMN pricing_schedule_local_weekday smallint,
    ADD COLUMN pricing_schedule_local_minute smallint,
    ADD COLUMN pricing_schedule_digest character varying(64);

ALTER TABLE public.usage_request_events
    DROP CONSTRAINT pricing_tier_evidence_check,
    DROP CONSTRAINT pricing_tier_applied_check,
    DROP COLUMN pricing_tier_applied,
    DROP COLUMN pricing_tier_threshold_tokens,
    DROP COLUMN pricing_tier_basis_tokens,
    ADD COLUMN pricing_template_kind character varying(16),
    ADD COLUMN pricing_selector_threshold_tokens integer,
    ADD COLUMN pricing_selector_basis_tokens bigint,
    ADD COLUMN pricing_selection_state character varying(20),
    ADD COLUMN pricing_card_role character varying(16),
    ADD COLUMN pricing_schedule_decided_at timestamp with time zone,
    ADD COLUMN pricing_schedule_timezone character varying(100),
    ADD COLUMN pricing_schedule_local_weekday smallint,
    ADD COLUMN pricing_schedule_local_minute smallint,
    ADD COLUMN pricing_schedule_digest character varying(64);

ALTER TABLE public.request_logs
    ADD CONSTRAINT pricing_template_kind_evidence_check CHECK (
        pricing_template_kind IS NULL
        OR pricing_template_kind IN ('standard', 'tiered', 'peak_valley')
    ) NOT VALID,
    ADD CONSTRAINT pricing_selection_state_check CHECK (
        pricing_selection_state IS NULL
        OR pricing_selection_state IN ('not_evaluated', 'not_applicable', 'selected', 'unresolved')
    ) NOT VALID,
    ADD CONSTRAINT pricing_card_role_check CHECK (
        pricing_card_role IS NULL
        OR pricing_card_role IN ('standard', 'tier_base', 'tier_above', 'peak', 'offpeak')
    ) NOT VALID,
    ADD CONSTRAINT pricing_selector_evidence_check CHECK (
        (pricing_selection_state IS NULL
            AND pricing_card_role IS NULL
            AND pricing_selector_threshold_tokens IS NULL
            AND pricing_selector_basis_tokens IS NULL)
        OR (pricing_selection_state = 'not_evaluated'
            AND pricing_card_role IS NULL
            AND pricing_selector_threshold_tokens IS NULL
            AND pricing_selector_basis_tokens IS NULL)
        OR (pricing_selection_state = 'not_applicable'
            AND pricing_card_role = 'tier_base'
            AND pricing_selector_threshold_tokens IS NULL
            AND pricing_selector_basis_tokens IS NULL)
        OR (pricing_selection_state = 'selected'
            AND pricing_card_role IS NOT NULL
            AND ((pricing_card_role IN ('tier_base', 'tier_above')
                AND pricing_selector_threshold_tokens IS NOT NULL
                AND pricing_selector_basis_tokens IS NOT NULL)
              OR (pricing_card_role NOT IN ('tier_base', 'tier_above')
                AND pricing_selector_threshold_tokens IS NULL
                AND pricing_selector_basis_tokens IS NULL)))
        OR (pricing_selection_state = 'unresolved'
            AND pricing_card_role IS NULL
            AND pricing_selector_threshold_tokens IS NULL
            AND pricing_selector_basis_tokens IS NULL)
    ) NOT VALID,
    ADD CONSTRAINT pricing_schedule_evidence_check CHECK (
        pricing_template_kind IS NULL
        OR pricing_template_kind <> 'peak_valley'
        OR pricing_selection_state IS NULL
        OR pricing_selection_state NOT IN ('selected', 'unresolved')
        OR (pricing_schedule_decided_at IS NOT NULL
            AND pricing_schedule_timezone IS NOT NULL
            AND pricing_schedule_local_weekday IS NULL
            AND pricing_schedule_local_minute IS NULL
            AND pricing_schedule_digest IS NOT NULL)
        OR (pricing_schedule_decided_at IS NOT NULL
            AND pricing_schedule_timezone IS NOT NULL
            AND pricing_schedule_local_weekday IS NOT NULL
            AND pricing_schedule_local_minute IS NOT NULL
            AND pricing_schedule_digest IS NOT NULL)
    ) NOT VALID,
    ADD CONSTRAINT pricing_schedule_local_weekday_check CHECK (pricing_schedule_local_weekday IS NULL OR pricing_schedule_local_weekday BETWEEN 1 AND 7) NOT VALID,
    ADD CONSTRAINT pricing_schedule_local_minute_check CHECK (pricing_schedule_local_minute IS NULL OR pricing_schedule_local_minute BETWEEN 0 AND 1439) NOT VALID;

ALTER TABLE public.usage_request_events
    ADD CONSTRAINT pricing_template_kind_evidence_check CHECK (
        pricing_template_kind IS NULL
        OR pricing_template_kind IN ('standard', 'tiered', 'peak_valley')
    ) NOT VALID,
    ADD CONSTRAINT pricing_selection_state_check CHECK (
        pricing_selection_state IS NULL
        OR pricing_selection_state IN ('not_evaluated', 'not_applicable', 'selected', 'unresolved')
    ) NOT VALID,
    ADD CONSTRAINT pricing_card_role_check CHECK (
        pricing_card_role IS NULL
        OR pricing_card_role IN ('standard', 'tier_base', 'tier_above', 'peak', 'offpeak')
    ) NOT VALID,
    ADD CONSTRAINT pricing_selector_evidence_check CHECK (
        (pricing_selection_state IS NULL
            AND pricing_card_role IS NULL
            AND pricing_selector_threshold_tokens IS NULL
            AND pricing_selector_basis_tokens IS NULL)
        OR (pricing_selection_state = 'not_evaluated'
            AND pricing_card_role IS NULL
            AND pricing_selector_threshold_tokens IS NULL
            AND pricing_selector_basis_tokens IS NULL)
        OR (pricing_selection_state = 'not_applicable'
            AND pricing_card_role = 'tier_base'
            AND pricing_selector_threshold_tokens IS NULL
            AND pricing_selector_basis_tokens IS NULL)
        OR (pricing_selection_state = 'selected'
            AND pricing_card_role IS NOT NULL
            AND ((pricing_card_role IN ('tier_base', 'tier_above')
                AND pricing_selector_threshold_tokens IS NOT NULL
                AND pricing_selector_basis_tokens IS NOT NULL)
              OR (pricing_card_role NOT IN ('tier_base', 'tier_above')
                AND pricing_selector_threshold_tokens IS NULL
                AND pricing_selector_basis_tokens IS NULL)))
        OR (pricing_selection_state = 'unresolved'
            AND pricing_card_role IS NULL
            AND pricing_selector_threshold_tokens IS NULL
            AND pricing_selector_basis_tokens IS NULL)
    ) NOT VALID,
    ADD CONSTRAINT pricing_schedule_evidence_check CHECK (
        pricing_template_kind IS NULL
        OR pricing_template_kind <> 'peak_valley'
        OR pricing_selection_state IS NULL
        OR pricing_selection_state NOT IN ('selected', 'unresolved')
        OR (pricing_schedule_decided_at IS NOT NULL
            AND pricing_schedule_timezone IS NOT NULL
            AND pricing_schedule_local_weekday IS NULL
            AND pricing_schedule_local_minute IS NULL
            AND pricing_schedule_digest IS NOT NULL)
        OR (pricing_schedule_decided_at IS NOT NULL
            AND pricing_schedule_timezone IS NOT NULL
            AND pricing_schedule_local_weekday IS NOT NULL
            AND pricing_schedule_local_minute IS NOT NULL
            AND pricing_schedule_digest IS NOT NULL)
    ) NOT VALID,
    ADD CONSTRAINT pricing_schedule_local_weekday_check CHECK (pricing_schedule_local_weekday IS NULL OR pricing_schedule_local_weekday BETWEEN 1 AND 7) NOT VALID,
    ADD CONSTRAINT pricing_schedule_local_minute_check CHECK (pricing_schedule_local_minute IS NULL OR pricing_schedule_local_minute BETWEEN 0 AND 1439) NOT VALID;

ALTER TABLE public.request_logs
    ADD CONSTRAINT pricing_templates_selection_kind_role_check CHECK (
        (pricing_selection_state IS NULL AND pricing_card_role IS NULL)
        OR (pricing_selection_state = 'not_evaluated' AND pricing_card_role IS NULL)
        OR (pricing_selection_state = 'unresolved' AND pricing_template_kind IS NOT NULL AND pricing_card_role IS NULL)
        OR (pricing_selection_state = 'not_applicable' AND pricing_template_kind = 'tiered' AND pricing_card_role = 'tier_base')
        OR (pricing_selection_state = 'selected' AND (
            (pricing_template_kind = 'standard' AND pricing_card_role = 'standard')
            OR (pricing_template_kind = 'tiered' AND pricing_card_role IN ('tier_base', 'tier_above'))
            OR (pricing_template_kind = 'peak_valley' AND pricing_card_role IN ('peak', 'offpeak'))
        ))
    ) NOT VALID;

ALTER TABLE public.usage_request_events
    ADD CONSTRAINT pricing_templates_selection_kind_role_check CHECK (
        (pricing_selection_state IS NULL AND pricing_card_role IS NULL)
        OR (pricing_selection_state = 'not_evaluated' AND pricing_card_role IS NULL)
        OR (pricing_selection_state = 'unresolved' AND pricing_template_kind IS NOT NULL AND pricing_card_role IS NULL)
        OR (pricing_selection_state = 'not_applicable' AND pricing_template_kind = 'tiered' AND pricing_card_role = 'tier_base')
        OR (pricing_selection_state = 'selected' AND (
            (pricing_template_kind = 'standard' AND pricing_card_role = 'standard')
            OR (pricing_template_kind = 'tiered' AND pricing_card_role IN ('tier_base', 'tier_above'))
            OR (pricing_template_kind = 'peak_valley' AND pricing_card_role IN ('peak', 'offpeak'))
        ))
    ) NOT VALID;

ALTER TABLE public.request_logs
    DROP CONSTRAINT pricing_resolution_kind_check,
    ADD CONSTRAINT pricing_resolution_kind_check CHECK (
        (unpriced_reason IS NOT NULL AND unpriced_reason = 'MISSING_PRICE_DATA' AND pricing_resolution_kind IS NOT NULL AND pricing_resolution_kind IN ('missing_component','currency_migration_required','unsupported_unit','snapshot_incoherent','schedule_unresolved'))
        OR (unpriced_reason IS DISTINCT FROM 'MISSING_PRICE_DATA' AND pricing_resolution_kind IS NULL)
    ) NOT VALID;

ALTER TABLE public.usage_request_events
    DROP CONSTRAINT pricing_resolution_kind_check,
    ADD CONSTRAINT pricing_resolution_kind_check CHECK (
        (unpriced_reason IS NOT NULL AND unpriced_reason = 'MISSING_PRICE_DATA' AND pricing_resolution_kind IS NOT NULL AND pricing_resolution_kind IN ('missing_component','currency_migration_required','unsupported_unit','snapshot_incoherent','schedule_unresolved'))
        OR (unpriced_reason IS DISTINCT FROM 'MISSING_PRICE_DATA' AND pricing_resolution_kind IS NULL)
    ) NOT VALID;
