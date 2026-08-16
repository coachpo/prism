-- 000008 pricing cost trust: additive schema + deterministic upgrade backfill.
--
-- Implements the additive half of the pricing cost-trust upgrade contract
-- (SPEC sections 5, 6, 11). This migration never runs while any application
-- traffic exists: the repository migrator applies every pending migration in
-- a single pre-startup transaction, so all additive schema, canonical
-- backfills, inventories and phase transitions below are executed before the
-- first reader or writer starts. Fresh zero-schema databases transition
-- straight to `finalize_ready`; existing-data upgrades run the deterministic
-- backfills inside this same transaction and only reach `finalize_ready`
-- when every finalization gate passes. 000004 then validates the gates and
-- finalizes the schema.

-- ============================================================================
-- Part 0: canonical helpers (shared by CHECK constraints, triggers, backfills
-- and the application). Every helper returns a non-NULL boolean or a NULL
-- canonical value so three-valued SQL logic can never silently pass a check.
-- ============================================================================

-- Trim Unicode White_Space from both ends by explicit code point set
-- (Go strings.TrimSpace equivalent): 0009-000D, 0020, 0085, 00A0, 1680,
-- 2000-200A, 2028, 2029, 202F, 205F, 3000.
CREATE FUNCTION public.prism_pricing_trim_unicode_whitespace(value text)
RETURNS text
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    head integer;
    tail integer;
BEGIN
    IF value IS NULL THEN
        RETURN NULL;
    END IF;
    -- Walk the string by character codes.
    head := 1;
    tail := char_length(value);
    WHILE head <= tail AND prism_pricing_unicode_whitespace_code(prism_pricing_char_to_int(substr(value, head, 1))) LOOP
        head := head + 1;
    END LOOP;
    WHILE tail >= head AND prism_pricing_unicode_whitespace_code(prism_pricing_char_to_int(substr(value, tail, 1))) LOOP
        tail := tail - 1;
    END LOOP;
    IF head > tail THEN
        RETURN '';
    END IF;
    RETURN substr(value, head, tail - head + 1);
END;
$$;

CREATE FUNCTION public.prism_pricing_unicode_whitespace_code(codepoint integer)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT codepoint IS NOT NULL AND (
        (codepoint >= 9 AND codepoint <= 13)
        OR codepoint = 32
        OR codepoint = 133
        OR codepoint = 160
        OR codepoint = 5760
        OR (codepoint >= 8192 AND codepoint <= 8202)
        OR codepoint = 8232
        OR codepoint = 8233
        OR codepoint = 8239
        OR codepoint = 8287
        OR codepoint = 12288
    )
$$;

CREATE FUNCTION public.prism_pricing_char_to_int(char_text text)
RETURNS integer
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT ascii(char_text)
$$;

-- Canonical 3-letter uppercase currency code, or NULL when invalid
-- (SPEC 5.1): reject any Unicode control/format and internal whitespace,
-- allow only exactly three ASCII [A-Za-z] after trimming outer Unicode
-- whitespace, then uppercase. Locale-sensitive case folding is never used.
CREATE FUNCTION public.prism_pricing_currency_code_canonical(value text)
RETURNS text
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    trimmed text;
    codepoint integer;
    index integer;
BEGIN
    IF value IS NULL THEN
        RETURN NULL;
    END IF;
    trimmed := prism_pricing_trim_unicode_whitespace(value);
    IF char_length(trimmed) <> 3 THEN
        RETURN NULL;
    END IF;
    FOR index IN 1..3 LOOP
        codepoint := prism_pricing_char_to_int(substr(trimmed, index, 1));
        IF codepoint IS NULL OR NOT ((codepoint >= 65 AND codepoint <= 90) OR (codepoint >= 97 AND codepoint <= 122)) THEN
            RETURN NULL;
        END IF;
    END LOOP;
    RETURN upper(trimmed);
END;
$$;

-- Currency symbol canonical boundary check (SPEC 5.1): the stored value must
-- already be canonicalized (Unicode NFC + outer whitespace trimmed by the
-- application), so this total check rejects blank values, any Cc/Cf/Zl/Zp
-- control/format/separator code point anywhere, more than 5 Unicode scalar
-- values, more than 20 UTF-8 octets, and any leading/trailing Unicode
-- whitespace.
CREATE FUNCTION public.prism_pricing_currency_symbol_valid(value text)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    codepoint integer;
    index integer;
    count integer;
BEGIN
    IF value IS NULL OR octet_length(value) > 20 THEN
        RETURN FALSE;
    END IF;
    count := char_length(value);
    IF count < 1 OR count > 5 THEN
        RETURN FALSE;
    END IF;
    IF prism_pricing_trim_unicode_whitespace(value) <> value THEN
        RETURN FALSE;
    END IF;
    FOR index IN 1..count LOOP
        codepoint := prism_pricing_char_to_int(substr(value, index, 1));
        IF codepoint IS NULL OR prism_pricing_control_or_separator_code(codepoint) THEN
            RETURN FALSE;
        END IF;
    END LOOP;
    RETURN TRUE;
END;
$$;

-- Cc (0000-001F, 007F-009F), Cf (00AD, 0600-0605, 061C, 06DD, 070F, 0890-0891,
-- 08E2, 180E, 200B-200F, 202A-202E, 2060-2064, 2066-206F, FEFF, FFF9-FFFB,
-- 110BD, 110CD, 13430-13438, 1BCA0-1BCA3, 1D173-1D17A, E0001, E0020-E007F),
-- Zl (2028), Zp (2029). Bidi override/isolate controls (202A-202E,
-- 2066-2069, 061C, 200E, 200F) are rejected separately by the name check but
-- are also Cf so covered here.
CREATE FUNCTION public.prism_pricing_control_or_separator_code(codepoint integer)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT codepoint IS NOT NULL AND (
        (codepoint <= 31) OR (codepoint >= 127 AND codepoint <= 159)
        OR codepoint = 173
        OR (codepoint >= 1536 AND codepoint <= 1541)
        OR codepoint = 1564
        OR codepoint = 1757
        OR codepoint = 1807
        OR (codepoint >= 2192 AND codepoint <= 2193)
        OR codepoint = 2274
        OR codepoint = 6158
        OR (codepoint >= 8203 AND codepoint <= 8207)
        OR (codepoint >= 8234 AND codepoint <= 8238)
        OR (codepoint >= 8288 AND codepoint <= 8292)
        OR (codepoint >= 8298 AND codepoint <= 8303)
        OR codepoint = 65279
        OR (codepoint >= 65529 AND codepoint <= 65532)
        OR codepoint = 69821
        OR codepoint = 69837
        OR (codepoint >= 78896 AND codepoint <= 78904)
        OR (codepoint >= 113824 AND codepoint <= 113827)
        OR (codepoint >= 917760 AND codepoint <= 917999)
        OR codepoint = 8232
        OR codepoint = 8233
    )
$$;

-- Canonical display name (SPEC 6.1 PricingTemplateNameIdentity): strict valid
-- UTF-8 text, outer Unicode whitespace trimmed, 1..128 Unicode code points,
-- at most 512 UTF-8 bytes, no NUL / Cc controls / Zl/Zp separators / bidi
-- override or isolate controls. Returns the canonical (trimmed) name or NULL
-- when invalid. NFC is not applied: byte-exact identity is the contract.
CREATE FUNCTION public.prism_pricing_template_name_canonical(value text)
RETURNS text
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    trimmed text;
    codepoint integer;
    index integer;
    count integer;
BEGIN
    IF value IS NULL THEN
        RETURN NULL;
    END IF;
    trimmed := prism_pricing_trim_unicode_whitespace(value);
    count := char_length(trimmed);
    IF count < 1 OR count > 128 OR octet_length(trimmed) > 512 THEN
        RETURN NULL;
    END IF;
    FOR index IN 1..count LOOP
        codepoint := prism_pricing_char_to_int(substr(trimmed, index, 1));
        IF codepoint IS NULL
           OR codepoint = 0
           OR prism_pricing_control_or_separator_code(codepoint)
           OR prism_pricing_bidi_control_code(codepoint) THEN
            RETURN NULL;
        END IF;
    END LOOP;
    RETURN trimmed;
END;
$$;

CREATE FUNCTION public.prism_pricing_bidi_control_code(codepoint integer)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT codepoint IS NOT NULL AND (
        codepoint = 1564
        OR codepoint = 8206
        OR codepoint = 8207
        OR (codepoint >= 8234 AND codepoint <= 8238)
        OR (codepoint >= 8294 AND codepoint <= 8297)
        OR (codepoint >= 8298 AND codepoint <= 8303)
    )
$$;

-- Canonical non-negative exact decimal (SPEC 4.2): matches ^\d+(\.\d+)?$,
-- at most 20 ASCII chars, no sign/exponent/grouping/NaN/infinity; returns the
-- canonical encoding (leading zeros, trailing zeros and pointless decimal
-- points removed; every numeric zero -> "0") or NULL when invalid. The
-- canonical form must itself stay within 1..20 ASCII chars.
CREATE FUNCTION public.prism_pricing_exact_decimal_canonical(value text)
RETURNS text
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    integral text;
    fractional text;
    result text;
BEGIN
    IF value IS NULL OR length(value) < 1 OR length(value) > 20 THEN
        RETURN NULL;
    END IF;
    IF value !~ '^[0-9]+(\.[0-9]+)?$' THEN
        RETURN NULL;
    END IF;
    IF position('.' in value) = 0 THEN
        integral := value;
        fractional := NULL;
    ELSE
        integral := split_part(value, '.', 1);
        fractional := split_part(value, '.', 2);
    END IF;
    -- strip leading zeros from the integral part (keep a single zero)
    integral := regexp_replace(integral, '^0+(?=[0-9])', '');
    IF integral = '' THEN
        integral := '0';
    END IF;
    -- strip trailing zeros from the fractional part
    IF fractional IS NOT NULL THEN
        fractional := regexp_replace(fractional, '0+$', '');
        IF fractional = '' THEN
            fractional := NULL;
        END IF;
    END IF;
    IF fractional IS NULL THEN
        result := integral;
    ELSE
        result := integral || '.' || fractional;
    END IF;
    IF length(result) < 1 OR length(result) > 20 THEN
        RETURN NULL;
    END IF;
    RETURN result;
END;
$$;

-- Nullable price snapshot text is canonical when NULL or an exact canonical
-- decimal (SPEC 4.2).
CREATE FUNCTION public.prism_pricing_snapshot_price_canonical(value text)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT value IS NULL OR prism_pricing_exact_decimal_canonical(value) = value
$$;

-- Nullable currency snapshot text is canonical when NULL or a canonical
-- 3-letter code.
CREATE FUNCTION public.prism_pricing_snapshot_currency_canonical(value text)
RETURNS boolean
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT value IS NULL OR prism_pricing_currency_code_canonical(value) = value
$$;

-- Fixed five canonical component literals in canonical order, non-empty,
-- de-duplicated (SPEC 6.5). Returns FALSE for NULL/empty arrays.
CREATE FUNCTION public.prism_pricing_components_are_canonical(components text[])
RETURNS boolean
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT components IS NOT NULL
       AND cardinality(components) > 0
       AND components = ARRAY(
            SELECT component
            FROM unnest(components) AS component
            WHERE component IN ('input_price','output_price','cached_input_price','cache_creation_price','reasoning_price')
            ORDER BY array_position(ARRAY['input_price','output_price','cached_input_price','cache_creation_price','reasoning_price'], component)
        )
       AND cardinality(components) = (
            SELECT count(DISTINCT component) FROM unnest(components) AS component
       )
$$;

-- Trusted cost coherence guard (SPEC 6.5): for trusted rows a priced event
-- carries all five component costs plus both totals; any other status carries
-- no trusted cost at all. `costs` must be exactly seven elements in the
-- canonical order input/output/cache-read/cache-creation/reasoning/original
-- total/user-currency total. Never returns NULL.
CREATE FUNCTION public.prism_pricing_costs_are_canonical_all_or_none(status text, costs bigint[])
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
    index integer;
    present boolean;
BEGIN
    IF costs IS NULL OR cardinality(costs) <> 7 THEN
        RETURN FALSE;
    END IF;
    present := FALSE;
    FOR index IN 1..7 LOOP
        IF costs[index] IS NOT NULL THEN
            present := TRUE;
            EXIT;
        END IF;
    END LOOP;
    IF status = 'priced' THEN
        RETURN present AND costs[1] IS NOT NULL AND costs[2] IS NOT NULL AND costs[3] IS NOT NULL
           AND costs[4] IS NOT NULL AND costs[5] IS NOT NULL AND costs[6] IS NOT NULL AND costs[7] IS NOT NULL;
    END IF;
    RETURN NOT present;
END;
$$;

-- Legacy snapshot coherence projection (SPEC 11.3): all pricing snapshot /
-- currency / FX / version evidence is canonical and mutually coherent.
-- Never returns NULL.
CREATE FUNCTION public.prism_pricing_legacy_snapshots_coherent(
    snapshot_unit text,
    snapshot_input text,
    snapshot_output text,
    snapshot_cache_read text,
    snapshot_cache_creation text,
    snapshot_reasoning text,
    currency_original text,
    report_currency text,
    report_symbol text,
    fx_rate text,
    fx_source text,
    config_version integer
)
RETURNS boolean
LANGUAGE plpgsql
IMMUTABLE
AS $$
BEGIN
    IF snapshot_unit IS NOT NULL AND snapshot_unit <> 'PER_1M' THEN
        RETURN FALSE;
    END IF;
    IF NOT prism_pricing_snapshot_price_canonical(snapshot_input)
       OR NOT prism_pricing_snapshot_price_canonical(snapshot_output)
       OR NOT prism_pricing_snapshot_price_canonical(snapshot_cache_read)
       OR NOT prism_pricing_snapshot_price_canonical(snapshot_cache_creation)
       OR NOT prism_pricing_snapshot_price_canonical(snapshot_reasoning) THEN
        RETURN FALSE;
    END IF;
    IF NOT prism_pricing_snapshot_currency_canonical(currency_original)
       OR NOT prism_pricing_snapshot_currency_canonical(report_currency) THEN
        RETURN FALSE;
    END IF;
    IF report_symbol IS NOT NULL AND NOT prism_pricing_currency_symbol_valid(report_symbol) THEN
        RETURN FALSE;
    END IF;
    IF fx_rate IS NOT NULL AND prism_pricing_exact_decimal_canonical(fx_rate) IS NULL THEN
        RETURN FALSE;
    END IF;
    IF fx_source IS NOT NULL AND fx_source NOT IN ('ENDPOINT_SPECIFIC','DEFAULT_1_TO_1') THEN
        RETURN FALSE;
    END IF;
    -- FX pair coherence
    IF fx_source = 'DEFAULT_1_TO_1' AND fx_rate IS NOT NULL AND prism_pricing_exact_decimal_canonical(fx_rate) <> '1' THEN
        RETURN FALSE;
    END IF;
    IF fx_source = 'ENDPOINT_SPECIFIC' AND fx_rate IS NULL THEN
        RETURN FALSE;
    END IF;
    IF fx_source IS NULL AND fx_rate IS NOT NULL THEN
        RETURN FALSE;
    END IF;
    IF config_version IS NOT NULL AND config_version < 1 THEN
        RETURN FALSE;
    END IF;
    RETURN TRUE;
END;
$$;

-- ============================================================================
-- Part 1: schema transition singleton and generation leases (SPEC 6.3.3).
-- Created before any profile/user_settings seed; exactly one row, id=1.
-- ============================================================================

CREATE TABLE public.pricing_schema_transition_state (
    id integer NOT NULL,
    phase character varying(32) NOT NULL,
    schema_generation bigint NOT NULL,
    writer_fence_generation bigint NOT NULL,
    lease_generation bigint NOT NULL,
    lease_acquisition_open boolean NOT NULL,
    finalizer_owner_id uuid,
    finalizer_expires_at timestamp with time zone,
    finalizer_fencing_token bigint NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pricing_schema_transition_state_pkey PRIMARY KEY (id),
    CONSTRAINT ck_pst_singleton CHECK ((id = 1)),
    CONSTRAINT ck_pst_phase CHECK (
        phase IN ('legacy_writer_open','fenced','reader_ready','finalize_ready','final')
    ),
    CONSTRAINT ck_pst_generations CHECK (
        schema_generation >= 1 AND writer_fence_generation >= 0
        AND lease_generation >= 1 AND finalizer_fencing_token >= 0
    )
);

CREATE TABLE public.pricing_schema_generation_leases (
    lease_id uuid NOT NULL,
    instance_id uuid NOT NULL,
    mode character varying(16) NOT NULL,
    schema_generation bigint NOT NULL,
    fencing_token bigint NOT NULL,
    acquired_at timestamp with time zone NOT NULL,
    heartbeat_at timestamp with time zone,
    expires_at timestamp with time zone NOT NULL,
    released_at timestamp with time zone,
    CONSTRAINT pricing_schema_generation_leases_pkey PRIMARY KEY (lease_id),
    CONSTRAINT ck_psgl_mode CHECK ((mode IN ('read','write','guard')))
);

CREATE UNIQUE INDEX uq_pricing_schema_generation_leases_open
    ON public.pricing_schema_generation_leases USING btree (instance_id, mode, schema_generation)
    WHERE (released_at IS NULL);

-- ============================================================================
-- Part 2: pricing_templates logical-row transition (SPEC 6.1)
-- ============================================================================

ALTER TABLE public.pricing_templates
    ADD COLUMN current_revision_id bigint,
    ADD COLUMN deleted_at timestamp with time zone,
    ADD COLUMN name_identity bytea;

-- Canonical name materializer + validator. Runs for any INSERT or UPDATE of
-- `name`, canonicalizes the display name and materializes byte-exact
-- name_identity. Invalid names raise the typed pricing_template_name_invalid
-- error.
CREATE FUNCTION public.prism_pricing_templates_name_identity_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    canonical text;
BEGIN
    canonical := prism_pricing_template_name_canonical(NEW.name);
    IF canonical IS NULL THEN
        RAISE EXCEPTION 'pricing_template_name_invalid (id=%)', NEW.id
            USING ERRCODE = '22023';
    END IF;
    NEW.name := canonical;
    NEW.name_identity := convert_to(canonical, 'UTF8');
    RETURN NEW;
END;
$$;

CREATE TRIGGER pricing_templates_name_identity_guard
    BEFORE INSERT OR UPDATE OF name ON public.pricing_templates
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_templates_name_identity_guard();

-- Backfill + preflight: every existing active name must be canonical and the
-- active identity set must be collision-free, otherwise the whole migration
-- rejects with the offending template IDs (SPEC 6.1). This also installs the
-- canonical name as the display name, so later app writes use the canonical
-- identity everywhere.
DO $$
DECLARE
    invalid_ids integer[];
    collision_ids integer[];
    row record;
    canonical text;
BEGIN
    FOR row IN SELECT id, name FROM public.pricing_templates ORDER BY id ASC LOOP
        canonical := prism_pricing_template_name_canonical(row.name);
        IF canonical IS NULL THEN
            invalid_ids := array_append(invalid_ids, row.id);
        END IF;
    END LOOP;
    IF invalid_ids IS NOT NULL THEN
        RAISE EXCEPTION '000008 rejected: % active pricing template(s) have invalid canonical names: %; rename them before upgrading', cardinality(invalid_ids), invalid_ids::text;
    END IF;

    UPDATE public.pricing_templates SET name = prism_pricing_template_name_canonical(name);
    UPDATE public.pricing_templates SET name_identity = convert_to(name, 'UTF8');
    ALTER TABLE public.pricing_templates ALTER COLUMN name_identity SET NOT NULL;

    SELECT array_agg(collision_id ORDER BY collision_id) INTO collision_ids
    FROM (
        SELECT MIN(id) AS collision_id
        FROM public.pricing_templates
        WHERE deleted_at IS NULL
        GROUP BY profile_id, name_identity
        HAVING count(*) > 1
    ) AS collisions;
    IF collision_ids IS NOT NULL THEN
        RAISE EXCEPTION '000008 rejected: canonical name identity collision across active pricing templates: %', collision_ids::text;
    END IF;
END;
$$;

ALTER TABLE public.pricing_templates DROP CONSTRAINT IF EXISTS uq_pricing_templates_profile_name;
CREATE UNIQUE INDEX uq_pricing_templates_profile_name_identity
    ON public.pricing_templates USING btree (profile_id, name_identity)
    WHERE (deleted_at IS NULL);

-- ============================================================================
-- Part 3: relax legacy mutable pricing columns (SPEC 11.1 step 4). They stay
-- as backfill evidence until 000004 physically drops them; new writers never
-- touch them after the fence.
-- ============================================================================

ALTER TABLE public.pricing_templates
    ALTER COLUMN pricing_unit DROP NOT NULL,
    ALTER COLUMN pricing_currency_code DROP NOT NULL,
    ALTER COLUMN input_price DROP NOT NULL,
    ALTER COLUMN output_price DROP NOT NULL,
    ALTER COLUMN cached_input_price DROP DEFAULT,
    ALTER COLUMN cached_input_price DROP NOT NULL,
    ALTER COLUMN cache_creation_price DROP DEFAULT,
    ALTER COLUMN cache_creation_price DROP NOT NULL,
    ALTER COLUMN reasoning_price DROP DEFAULT,
    ALTER COLUMN reasoning_price DROP NOT NULL,
    ALTER COLUMN version DROP NOT NULL;

-- ============================================================================
-- Part 4: reporting_currency_epochs (SPEC 6.3)
-- ============================================================================

CREATE TABLE public.reporting_currency_epochs (
    id bigint NOT NULL,
    profile_id integer NOT NULL,
    epoch integer NOT NULL,
    currency_code character varying(3) NOT NULL,
    currency_symbol character varying(5) NOT NULL,
    effective_at timestamp with time zone,
    superseded_at timestamp with time zone,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT reporting_currency_epochs_pkey PRIMARY KEY (id),
    CONSTRAINT ck_rce_epoch_positive CHECK ((epoch >= 1)),
    CONSTRAINT ck_rce_code_canonical CHECK ((prism_pricing_currency_code_canonical(currency_code) = currency_code)),
    CONSTRAINT ck_rce_symbol_valid CHECK (prism_pricing_currency_symbol_valid(currency_symbol)),
    CONSTRAINT uq_reporting_currency_epochs_profile_epoch UNIQUE (profile_id, epoch)
);

CREATE SEQUENCE public.reporting_currency_epochs_id_seq
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.reporting_currency_epochs_id_seq OWNED BY public.reporting_currency_epochs.id;
ALTER TABLE ONLY public.reporting_currency_epochs ALTER COLUMN id SET DEFAULT nextval('public.reporting_currency_epochs_id_seq'::regclass);

CREATE UNIQUE INDEX uq_reporting_currency_epochs_active
    ON public.reporting_currency_epochs USING btree (profile_id)
    WHERE (superseded_at IS NULL);

ALTER TABLE ONLY public.reporting_currency_epochs
    ADD CONSTRAINT reporting_currency_epochs_profile_id_fkey
    FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE CASCADE;

-- ============================================================================
-- Part 5: pricing_template_revisions (SPEC 6.2) - append-only rate versions
-- ============================================================================

CREATE TABLE public.pricing_template_revisions (
    id bigint NOT NULL,
    template_id integer NOT NULL,
    version integer NOT NULL,
    pricing_unit character varying(20) NOT NULL,
    currency_code character varying(3) NOT NULL,
    reporting_currency_epoch_id bigint,
    reporting_currency_epoch integer,
    currency_attribution character varying(24) NOT NULL,
    input_price character varying(20) NOT NULL,
    output_price character varying(20) NOT NULL,
    cached_input_price character varying(20),
    cache_creation_price character varying(20),
    reasoning_price character varying(20),
    effective_at timestamp with time zone,
    created_at timestamp with time zone NOT NULL,
    created_by_kind character varying(32) NOT NULL,
    created_by_operation_id uuid,
    CONSTRAINT pricing_template_revisions_pkey PRIMARY KEY (id),
    CONSTRAINT ck_ptr_version_positive CHECK ((version >= 1)),
    CONSTRAINT ck_ptr_unit CHECK ((pricing_unit = 'PER_1M')),
    CONSTRAINT ck_ptr_attribution CHECK ((currency_attribution IN ('active_epoch','legacy_foreign','pre_epoch_pending'))),
    CONSTRAINT ck_ptr_attribution_epoch CHECK (
        ((currency_attribution = 'active_epoch')
            AND (reporting_currency_epoch_id IS NOT NULL) AND (reporting_currency_epoch IS NOT NULL))
        OR ((currency_attribution IN ('legacy_foreign','pre_epoch_pending'))
            AND (reporting_currency_epoch_id IS NULL) AND (reporting_currency_epoch IS NULL))
    ),
    CONSTRAINT ck_ptr_code_canonical CHECK ((prism_pricing_currency_code_canonical(currency_code) = currency_code)),
    CONSTRAINT ck_ptr_input_price CHECK ((prism_pricing_exact_decimal_canonical(input_price) = input_price)),
    CONSTRAINT ck_ptr_output_price CHECK ((prism_pricing_exact_decimal_canonical(output_price) = output_price)),
    CONSTRAINT ck_ptr_cached_input_price CHECK ((cached_input_price IS NULL OR prism_pricing_exact_decimal_canonical(cached_input_price) = cached_input_price)),
    CONSTRAINT ck_ptr_cache_creation_price CHECK ((cache_creation_price IS NULL OR prism_pricing_exact_decimal_canonical(cache_creation_price) = cache_creation_price)),
    CONSTRAINT ck_ptr_reasoning_price CHECK ((reasoning_price IS NULL OR prism_pricing_exact_decimal_canonical(reasoning_price) = reasoning_price)),
    CONSTRAINT ck_ptr_created_by_kind CHECK ((created_by_kind IN ('manual_create','manual_edit','import','currency_migration','legacy_migration_repair','legacy_backfill'))),
    CONSTRAINT ck_ptr_legacy_backfill_operation CHECK (((created_by_kind = 'legacy_backfill') = (created_by_operation_id IS NULL))),
    CONSTRAINT ck_ptr_non_legacy_effective CHECK ((created_by_kind = 'legacy_backfill' OR effective_at IS NOT NULL)),
    CONSTRAINT uq_ptr_template_version UNIQUE (template_id, version)
);

CREATE SEQUENCE public.pricing_template_revisions_id_seq
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.pricing_template_revisions_id_seq OWNED BY public.pricing_template_revisions.id;
ALTER TABLE ONLY public.pricing_template_revisions ALTER COLUMN id SET DEFAULT nextval('public.pricing_template_revisions_id_seq'::regclass);

ALTER TABLE ONLY public.pricing_template_revisions
    ADD CONSTRAINT pricing_template_revisions_template_id_fkey
    FOREIGN KEY (template_id) REFERENCES public.pricing_templates(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.pricing_template_revisions
    ADD CONSTRAINT pricing_template_revisions_epoch_id_fkey
    FOREIGN KEY (reporting_currency_epoch_id) REFERENCES public.reporting_currency_epochs(id) ON DELETE RESTRICT;

CREATE INDEX idx_pricing_template_revisions_template
    ON public.pricing_template_revisions USING btree (template_id, version);

-- Revisions are append-only: no UPDATE, no DELETE, no reassignment.
CREATE FUNCTION public.prism_pricing_revisions_append_only()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION 'pricing_template_revisions are append-only'
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER pricing_template_revisions_append_only
    BEFORE UPDATE OR DELETE ON public.pricing_template_revisions
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_revisions_append_only();

-- ============================================================================
-- Part 6: user_settings additive costing surface (SPEC 5.2/5.3/6.3)
-- ============================================================================

ALTER TABLE public.user_settings
    ADD COLUMN current_reporting_currency_epoch_id bigint,
    ADD COLUMN pricing_migration_state character varying(48) NOT NULL DEFAULT 'ready',
    ADD COLUMN legacy_migration_issues text[] NOT NULL DEFAULT '{}',
    ADD COLUMN pricing_report_currency_code_v2 character varying(3),
    ADD COLUMN pricing_report_currency_symbol_v2 character varying(5),
    ADD COLUMN pricing_template_generation bigint NOT NULL DEFAULT 0,
    ADD COLUMN pricing_reference_generation bigint NOT NULL DEFAULT 0;

ALTER TABLE ONLY public.user_settings
    ADD CONSTRAINT user_settings_current_epoch_fkey
    FOREIGN KEY (current_reporting_currency_epoch_id) REFERENCES public.reporting_currency_epochs(id) ON DELETE RESTRICT;

ALTER TABLE public.user_settings
    ADD CONSTRAINT ck_us_pricing_migration_state CHECK ((pricing_migration_state IN ('ready','legacy_pricing_migration_required'))),
    ADD CONSTRAINT ck_us_migration_issues CHECK ((legacy_migration_issues <@ ARRAY[
        'foreign_currency_template','live_fx_dependency','unsupported_pricing_unit',
        'invalid_price_encoding','invalid_reporting_currency_code','invalid_reporting_currency_symbol'
    ]::text[])),
    ADD CONSTRAINT ck_us_ready_has_no_issues CHECK ((pricing_migration_state <> 'ready' OR legacy_migration_issues = '{}')),
    ADD CONSTRAINT ck_us_v2_code_canonical CHECK ((pricing_report_currency_code_v2 IS NULL OR prism_pricing_currency_code_canonical(pricing_report_currency_code_v2) = pricing_report_currency_code_v2)),
    ADD CONSTRAINT ck_us_v2_symbol_valid CHECK ((pricing_report_currency_symbol_v2 IS NULL OR prism_pricing_currency_symbol_valid(pricing_report_currency_symbol_v2))),
    ADD CONSTRAINT ck_us_template_generation_nonneg CHECK ((pricing_template_generation >= 0)),
    ADD CONSTRAINT ck_us_reference_generation_nonneg CHECK ((pricing_reference_generation >= 0));

-- ============================================================================
-- Part 7: migration machinery tables (SPEC 6.3.1/6.3.2)
-- ============================================================================

-- Immutable migration inventories (system scan output, generation head).
CREATE TABLE public.pricing_migration_inventories (
    inventory_id bigint NOT NULL,
    profile_id integer NOT NULL,
    generation bigint NOT NULL,
    supersedes_inventory_id bigint,
    settings_generation bigint NOT NULL,
    epoch_generation bigint,
    template_generation bigint NOT NULL,
    reference_generation bigint NOT NULL,
    issue_codes text[] NOT NULL DEFAULT '{}',
    fx_evidence_count integer NOT NULL DEFAULT 0,
    fx_assessment_count integer NOT NULL DEFAULT 0,
    fx_dependency_count integer NOT NULL DEFAULT 0,
    template_evidence_count integer NOT NULL DEFAULT 0,
    reporting_currency_evidence_count integer NOT NULL DEFAULT 0,
    fx_evidence_hash_root text,
    template_evidence_hash_root text,
    reporting_currency_evidence_hash_root text,
    legacy_fx_source_count bigint NOT NULL DEFAULT 0,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT pricing_migration_inventories_pkey PRIMARY KEY (inventory_id),
    CONSTRAINT ck_pmi_counts_nonneg CHECK (
        fx_evidence_count >= 0 AND fx_assessment_count >= 0 AND fx_dependency_count >= 0
        AND template_evidence_count >= 0 AND legacy_fx_source_count >= 0
    ),
    CONSTRAINT ck_pmi_reporting_currency_evidence_count CHECK ((reporting_currency_evidence_count IN (0, 1))),
    CONSTRAINT ck_pmi_generation_positive CHECK ((generation >= 1)),
    CONSTRAINT ck_pmi_issue_codes CHECK ((issue_codes <@ ARRAY[
        'foreign_currency_template','live_fx_dependency','unsupported_pricing_unit',
        'invalid_price_encoding','invalid_reporting_currency_code','invalid_reporting_currency_symbol',
        'unused_fx_evidence'
    ]::text[])),
    CONSTRAINT uq_pmi_profile_generation UNIQUE (profile_id, generation)
);

CREATE SEQUENCE public.pricing_migration_inventories_id_seq
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.pricing_migration_inventories_id_seq OWNED BY public.pricing_migration_inventories.inventory_id;
ALTER TABLE ONLY public.pricing_migration_inventories ALTER COLUMN inventory_id SET DEFAULT nextval('public.pricing_migration_inventories_id_seq'::regclass);

ALTER TABLE ONLY public.pricing_migration_inventories
    ADD CONSTRAINT pricing_migration_inventories_profile_id_fkey
    FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.pricing_migration_inventories
    ADD CONSTRAINT pricing_migration_inventories_supersedes_fkey
    FOREIGN KEY (supersedes_inventory_id) REFERENCES public.pricing_migration_inventories(inventory_id) ON DELETE RESTRICT;

-- At most one direct successor per inventory (head chain).
CREATE UNIQUE INDEX uq_pricing_migration_inventories_successor
    ON public.pricing_migration_inventories (supersedes_inventory_id)
    WHERE (supersedes_inventory_id IS NOT NULL);

CREATE INDEX idx_pricing_migration_inventories_profile
    ON public.pricing_migration_inventories USING btree (profile_id, generation);

-- Immutable raw legacy FX evidence (SPEC 6.3.1).
CREATE TABLE public.currency_migration_legacy_fx_evidence (
    legacy_fx_evidence_id bigint NOT NULL,
    inventory_id bigint NOT NULL,
    source_fx_row_id integer NOT NULL,
    profile_id integer NOT NULL,
    model_id character varying(200) NOT NULL,
    endpoint_id integer NOT NULL,
    fx_rate character varying(20) NOT NULL,
    source_created_at timestamp with time zone NOT NULL,
    source_updated_at timestamp with time zone NOT NULL,
    row_hash text NOT NULL,
    recorded_at timestamp with time zone NOT NULL,
    CONSTRAINT currency_migration_legacy_fx_evidence_pkey PRIMARY KEY (legacy_fx_evidence_id),
    CONSTRAINT uq_lfxe_inventory_source UNIQUE (inventory_id, source_fx_row_id)
);

CREATE SEQUENCE public.currency_migration_legacy_fx_evidence_id_seq
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.currency_migration_legacy_fx_evidence_id_seq OWNED BY public.currency_migration_legacy_fx_evidence.legacy_fx_evidence_id;
ALTER TABLE ONLY public.currency_migration_legacy_fx_evidence ALTER COLUMN legacy_fx_evidence_id SET DEFAULT nextval('public.currency_migration_legacy_fx_evidence_id_seq'::regclass);

ALTER TABLE ONLY public.currency_migration_legacy_fx_evidence
    ADD CONSTRAINT currency_migration_legacy_fx_evidence_inventory_fkey
    FOREIGN KEY (inventory_id) REFERENCES public.pricing_migration_inventories(inventory_id) ON DELETE RESTRICT;

-- Exactly one assessment per evidence row.
CREATE TABLE public.currency_migration_legacy_fx_assessments (
    legacy_fx_assessment_id bigint NOT NULL,
    legacy_fx_evidence_id bigint NOT NULL,
    attribution character varying(16) NOT NULL,
    scan_proof_code character varying(64) NOT NULL,
    scan_proof_hash text NOT NULL,
    evaluated_at timestamp with time zone NOT NULL,
    CONSTRAINT currency_migration_legacy_fx_assessments_pkey PRIMARY KEY (legacy_fx_assessment_id),
    CONSTRAINT ck_lfxa_attribution CHECK ((attribution IN ('has_live','unused','unknown'))),
    CONSTRAINT uq_lfxa_evidence UNIQUE (legacy_fx_evidence_id)
);

CREATE SEQUENCE public.currency_migration_legacy_fx_assessments_id_seq
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.currency_migration_legacy_fx_assessments_id_seq OWNED BY public.currency_migration_legacy_fx_assessments.legacy_fx_assessment_id;
ALTER TABLE ONLY public.currency_migration_legacy_fx_assessments ALTER COLUMN legacy_fx_assessment_id SET DEFAULT nextval('public.currency_migration_legacy_fx_assessments_id_seq'::regclass);

ALTER TABLE ONLY public.currency_migration_legacy_fx_assessments
    ADD CONSTRAINT currency_migration_legacy_fx_assessments_evidence_fkey
    FOREIGN KEY (legacy_fx_evidence_id) REFERENCES public.currency_migration_legacy_fx_evidence(legacy_fx_evidence_id) ON DELETE RESTRICT;

-- One row per actual live dependency of a raw FX row.
CREATE TABLE public.currency_migration_legacy_fx_dependencies (
    legacy_fx_dependency_id bigint NOT NULL,
    inventory_id bigint NOT NULL,
    legacy_fx_evidence_id bigint NOT NULL,
    connection_id integer NOT NULL,
    template_id integer NOT NULL,
    model_config_id integer NOT NULL,
    endpoint_id integer NOT NULL,
    source_template_currency character varying(3),
    target_report_currency character varying(3),
    proof_hash text NOT NULL,
    CONSTRAINT currency_migration_legacy_fx_dependencies_pkey PRIMARY KEY (legacy_fx_dependency_id),
    CONSTRAINT uq_lfxd_inventory_evidence_connection_template UNIQUE (inventory_id, legacy_fx_evidence_id, connection_id, template_id)
);

CREATE SEQUENCE public.currency_migration_legacy_fx_dependencies_id_seq
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.currency_migration_legacy_fx_dependencies_id_seq OWNED BY public.currency_migration_legacy_fx_dependencies.legacy_fx_dependency_id;
ALTER TABLE ONLY public.currency_migration_legacy_fx_dependencies ALTER COLUMN legacy_fx_dependency_id SET DEFAULT nextval('public.currency_migration_legacy_fx_dependencies_id_seq'::regclass);

ALTER TABLE ONLY public.currency_migration_legacy_fx_dependencies
    ADD CONSTRAINT currency_migration_legacy_fx_dependencies_inventory_fkey
    FOREIGN KEY (inventory_id) REFERENCES public.pricing_migration_inventories(inventory_id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.currency_migration_legacy_fx_dependencies
    ADD CONSTRAINT currency_migration_legacy_fx_dependencies_evidence_fkey
    FOREIGN KEY (legacy_fx_evidence_id) REFERENCES public.currency_migration_legacy_fx_evidence(legacy_fx_evidence_id) ON DELETE RESTRICT;

-- Immutable raw legacy template evidence (SPEC 6.3.1 / 11.2).
CREATE TABLE public.pricing_migration_legacy_template_evidence (
    legacy_template_evidence_id bigint NOT NULL,
    inventory_id bigint NOT NULL,
    template_id integer NOT NULL,
    profile_id integer NOT NULL,
    public_version integer NOT NULL,
    pricing_unit character varying(20),
    currency_code character varying(3),
    input_price character varying(20),
    output_price character varying(20),
    cached_input_price character varying(20),
    cache_creation_price character varying(20),
    reasoning_price character varying(20),
    issue_codes text[] NOT NULL DEFAULT '{}',
    recorded_at timestamp with time zone NOT NULL,
    row_hash text NOT NULL,
    CONSTRAINT pricing_migration_legacy_template_evidence_pkey PRIMARY KEY (legacy_template_evidence_id),
    CONSTRAINT uq_lte_inventory_template UNIQUE (inventory_id, template_id),
    CONSTRAINT ck_lte_issue_codes CHECK ((issue_codes <@ ARRAY[
        'unsupported_pricing_unit','invalid_price_encoding','foreign_currency_template'
    ]::text[]))
);

CREATE SEQUENCE public.pricing_migration_legacy_template_evidence_id_seq
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.pricing_migration_legacy_template_evidence_id_seq OWNED BY public.pricing_migration_legacy_template_evidence.legacy_template_evidence_id;
ALTER TABLE ONLY public.pricing_migration_legacy_template_evidence ALTER COLUMN legacy_template_evidence_id SET DEFAULT nextval('public.pricing_migration_legacy_template_evidence_id_seq'::regclass);

ALTER TABLE ONLY public.pricing_migration_legacy_template_evidence
    ADD CONSTRAINT pricing_migration_legacy_template_evidence_inventory_fkey
    FOREIGN KEY (inventory_id) REFERENCES public.pricing_migration_inventories(inventory_id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.pricing_migration_legacy_template_evidence
    ADD CONSTRAINT pricing_migration_legacy_template_evidence_template_fkey
    FOREIGN KEY (template_id) REFERENCES public.pricing_templates(id) ON DELETE RESTRICT;

-- Immutable raw legacy reporting-currency evidence (SPEC 6.3.1 / 11.4).
CREATE TABLE public.pricing_migration_legacy_reporting_currency_evidence (
    legacy_reporting_currency_evidence_id bigint NOT NULL,
    inventory_id bigint NOT NULL,
    profile_id integer NOT NULL,
    raw_report_currency_code text NOT NULL,
    raw_report_currency_symbol text NOT NULL,
    settings_updated_at timestamp with time zone NOT NULL,
    validation_codes text[] NOT NULL,
    recorded_at timestamp with time zone NOT NULL,
    row_hash text NOT NULL,
    CONSTRAINT pricing_migration_legacy_reporting_currency_evidence_pkey PRIMARY KEY (legacy_reporting_currency_evidence_id),
    CONSTRAINT uq_lrce_inventory UNIQUE (inventory_id),
    CONSTRAINT ck_lrce_validation_codes CHECK ((validation_codes <@ ARRAY[
        'invalid_reporting_currency_code','invalid_reporting_currency_symbol'
    ]::text[]))
);

CREATE SEQUENCE public.pricing_migration_legacy_reporting_currency_evidence_id_seq
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.pricing_migration_legacy_reporting_currency_evidence_id_seq OWNED BY public.pricing_migration_legacy_reporting_currency_evidence.legacy_reporting_currency_evidence_id;
ALTER TABLE ONLY public.pricing_migration_legacy_reporting_currency_evidence ALTER COLUMN legacy_reporting_currency_evidence_id SET DEFAULT nextval('public.pricing_migration_legacy_reporting_currency_evidence_id_seq'::regclass);

ALTER TABLE ONLY public.pricing_migration_legacy_reporting_currency_evidence
    ADD CONSTRAINT pricing_migration_legacy_reporting_currency_evidence_inventory_fkey
    FOREIGN KEY (inventory_id) REFERENCES public.pricing_migration_inventories(inventory_id) ON DELETE RESTRICT;

-- Append-only guards shared by all immutable evidence/inventory tables.
CREATE FUNCTION public.prism_pricing_migration_evidence_append_only()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% are append-only', TG_TABLE_NAME
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER pricing_migration_inventories_append_only
    BEFORE UPDATE OR DELETE ON public.pricing_migration_inventories
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_migration_evidence_append_only();

CREATE TRIGGER currency_migration_legacy_fx_evidence_append_only
    BEFORE UPDATE OR DELETE ON public.currency_migration_legacy_fx_evidence
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_migration_evidence_append_only();

CREATE TRIGGER currency_migration_legacy_fx_assessments_append_only
    BEFORE UPDATE OR DELETE ON public.currency_migration_legacy_fx_assessments
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_migration_evidence_append_only();

CREATE TRIGGER currency_migration_legacy_fx_dependencies_append_only
    BEFORE UPDATE OR DELETE ON public.currency_migration_legacy_fx_dependencies
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_migration_evidence_append_only();

CREATE TRIGGER pricing_migration_legacy_template_evidence_append_only
    BEFORE UPDATE OR DELETE ON public.pricing_migration_legacy_template_evidence
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_migration_evidence_append_only();

CREATE TRIGGER pricing_migration_legacy_reporting_currency_evidence_append_only
    BEFORE UPDATE OR DELETE ON public.pricing_migration_legacy_reporting_currency_evidence
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_migration_evidence_append_only();

-- Telemetry migration quarantine (SPEC 6.3.2).
CREATE TABLE public.pricing_telemetry_quarantine (
    quarantine_id bigint NOT NULL,
    profile_id integer NOT NULL,
    source_kind character varying(32) NOT NULL,
    issue_code character varying(48) NOT NULL,
    source_identity_snapshot jsonb NOT NULL,
    source_identity_hash text NOT NULL,
    evidence_snapshot jsonb NOT NULL,
    payload_hash text NOT NULL,
    detected_at timestamp with time zone NOT NULL,
    CONSTRAINT pricing_telemetry_quarantine_pkey PRIMARY KEY (quarantine_id),
    CONSTRAINT ck_ptq_source_kind CHECK ((source_kind IN ('request_log','usage_event','telemetry_outbox'))),
    CONSTRAINT ck_ptq_issue_code CHECK ((issue_code IN ('missing_final_http_status','invalid_final_http_status','streaming_accepted_orphan','legacy_payload_unclassifiable'))),
    CONSTRAINT uq_ptq_identity UNIQUE (profile_id, source_kind, issue_code, source_identity_hash, payload_hash)
);

CREATE SEQUENCE public.pricing_telemetry_quarantine_id_seq
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.pricing_telemetry_quarantine_id_seq OWNED BY public.pricing_telemetry_quarantine.quarantine_id;
ALTER TABLE ONLY public.pricing_telemetry_quarantine ALTER COLUMN quarantine_id SET DEFAULT nextval('public.pricing_telemetry_quarantine_id_seq'::regclass);

CREATE TABLE public.pricing_telemetry_quarantine_resolutions (
    resolution_id bigint NOT NULL,
    quarantine_id bigint NOT NULL,
    resolution_kind character varying(32) NOT NULL,
    proof_kind character varying(64) NOT NULL,
    proof_identity text NOT NULL,
    proof_hash text NOT NULL,
    successor_quarantine_id bigint,
    final_http_eligibility boolean,
    final_status_projection integer,
    resolver_code character varying(64) NOT NULL,
    resolved_at timestamp with time zone NOT NULL,
    CONSTRAINT pricing_telemetry_quarantine_resolutions_pkey PRIMARY KEY (resolution_id),
    CONSTRAINT ck_ptqr_resolution_kind CHECK ((resolution_kind IN ('authoritative_fix','superseded_by_verified_evidence'))),
    CONSTRAINT uq_ptqr_quarantine UNIQUE (quarantine_id)
);

CREATE SEQUENCE public.pricing_telemetry_quarantine_resolutions_id_seq
    AS bigint
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE public.pricing_telemetry_quarantine_resolutions_id_seq OWNED BY public.pricing_telemetry_quarantine_resolutions.resolution_id;
ALTER TABLE ONLY public.pricing_telemetry_quarantine_resolutions ALTER COLUMN resolution_id SET DEFAULT nextval('public.pricing_telemetry_quarantine_resolutions_id_seq'::regclass);

ALTER TABLE ONLY public.pricing_telemetry_quarantine_resolutions
    ADD CONSTRAINT pricing_telemetry_quarantine_resolutions_quarantine_fkey
    FOREIGN KEY (quarantine_id) REFERENCES public.pricing_telemetry_quarantine(quarantine_id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.pricing_telemetry_quarantine_resolutions
    ADD CONSTRAINT pricing_telemetry_quarantine_resolutions_successor_fkey
    FOREIGN KEY (successor_quarantine_id) REFERENCES public.pricing_telemetry_quarantine(quarantine_id) ON DELETE RESTRICT;

CREATE TRIGGER pricing_telemetry_quarantine_append_only
    BEFORE UPDATE OR DELETE ON public.pricing_telemetry_quarantine
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_migration_evidence_append_only();

CREATE TRIGGER pricing_telemetry_quarantine_resolutions_append_only
    BEFORE UPDATE OR DELETE ON public.pricing_telemetry_quarantine_resolutions
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_migration_evidence_append_only();

-- Global idempotency reservations and committed mutation operations
-- (SPEC 6.3.1 / 7.6.1).
CREATE TABLE public.pricing_mutation_operation_reservations (
    operation_id uuid NOT NULL,
    profile_id integer NOT NULL,
    intended_result_kind character varying(48) NOT NULL,
    normalized_identity_hash text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT pricing_mutation_operation_reservations_pkey PRIMARY KEY (operation_id),
    CONSTRAINT ck_pmor_result_kind CHECK ((intended_result_kind IN ('template_create','template_update','template_import','currency_cutover','repair_same_currency','archive_unused_fx')))
);

CREATE TABLE public.pricing_mutation_operations (
    operation_id uuid NOT NULL,
    profile_id integer NOT NULL,
    result_kind character varying(48) NOT NULL,
    normalized_payload_hash text NOT NULL,
    preview_hash text NOT NULL,
    operation_recorded_at timestamp with time zone NOT NULL,
    success_summary jsonb NOT NULL,
    result_hash text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT pricing_mutation_operations_pkey PRIMARY KEY (operation_id),
    CONSTRAINT ck_pmo_result_kind CHECK ((result_kind IN ('template_create','template_update','template_import','currency_cutover','repair_same_currency','archive_unused_fx')))
);

ALTER TABLE ONLY public.pricing_mutation_operations
    ADD CONSTRAINT pricing_mutation_operations_reservation_fkey
    FOREIGN KEY (operation_id) REFERENCES public.pricing_mutation_operation_reservations(operation_id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE public.pricing_mutation_result_items (
    operation_id uuid NOT NULL,
    ordinal integer NOT NULL,
    template_id integer NOT NULL,
    action character varying(32) NOT NULL,
    version integer,
    revision_id bigint,
    revision_effective_at timestamp with time zone,
    template_name_snapshot text NOT NULL,
    CONSTRAINT pricing_mutation_result_items_pkey PRIMARY KEY (operation_id, ordinal),
    CONSTRAINT ck_pmri_action CHECK ((action IN ('created','metadata_updated','revision_created','metadata_and_revision','no_op')))
);

ALTER TABLE ONLY public.pricing_mutation_result_items
    ADD CONSTRAINT pricing_mutation_result_items_operation_fkey
    FOREIGN KEY (operation_id) REFERENCES public.pricing_mutation_operations(operation_id)
    DEFERRABLE INITIALLY DEFERRED;

CREATE TRIGGER pricing_mutation_operation_reservations_append_only
    BEFORE UPDATE OR DELETE ON public.pricing_mutation_operation_reservations
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_migration_evidence_append_only();

CREATE TRIGGER pricing_mutation_operations_append_only
    BEFORE UPDATE OR DELETE ON public.pricing_mutation_operations
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_migration_evidence_append_only();

CREATE TRIGGER pricing_mutation_result_items_append_only
    BEFORE UPDATE OR DELETE ON public.pricing_mutation_result_items
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_migration_evidence_append_only();

-- Revision operation ownership FK: created after the operations table exists.
ALTER TABLE ONLY public.pricing_template_revisions
    ADD CONSTRAINT pricing_template_revisions_operation_fkey
    FOREIGN KEY (created_by_operation_id) REFERENCES public.pricing_mutation_operations(operation_id)
    DEFERRABLE INITIALLY DEFERRED;

-- Chunked currency migration drafts (SPEC 5.4/6.3.1).
CREATE TABLE public.pricing_currency_migration_drafts (
    draft_id uuid NOT NULL,
    profile_id integer NOT NULL,
    migration_operation_id uuid NOT NULL,
    operation_kind character varying(32) NOT NULL,
    target_currency_code character varying(3) NOT NULL,
    target_currency_symbol character varying(5) NOT NULL,
    expected_inventory_id bigint,
    expected_inventory_hash text,
    expected_inventory_generation bigint,
    expected_reporting_currency_epoch bigint,
    expected_settings_updated_at timestamp with time zone NOT NULL,
    status character varying(16) NOT NULL,
    normalized_header_hash text NOT NULL,
    received_chunk_count integer NOT NULL DEFAULT 0,
    draft_hash text,
    template_count integer,
    committed_result_operation_id uuid,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pricing_currency_migration_drafts_pkey PRIMARY KEY (draft_id),
    CONSTRAINT ck_pcmd_operation_kind CHECK ((operation_kind IN ('currency_cutover','repair_same_currency'))),
    CONSTRAINT ck_pcmd_status CHECK ((status IN ('uploading','sealed','committed','expired'))),
    CONSTRAINT ck_pcmd_target_code_canonical CHECK ((prism_pricing_currency_code_canonical(target_currency_code) = target_currency_code)),
    CONSTRAINT ck_pcmd_target_symbol_valid CHECK (prism_pricing_currency_symbol_valid(target_currency_symbol)),
    CONSTRAINT ck_pcmd_received_chunks_nonneg CHECK ((received_chunk_count >= 0)),
    CONSTRAINT uq_pcmd_profile_operation UNIQUE (profile_id, migration_operation_id)
);

ALTER TABLE ONLY public.pricing_currency_migration_drafts
    ADD CONSTRAINT pricing_currency_migration_drafts_operation_fkey
    FOREIGN KEY (migration_operation_id) REFERENCES public.pricing_mutation_operation_reservations(operation_id)
    DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE ONLY public.pricing_currency_migration_drafts
    ADD CONSTRAINT pricing_currency_migration_drafts_inventory_fkey
    FOREIGN KEY (expected_inventory_id) REFERENCES public.pricing_migration_inventories(inventory_id) ON DELETE RESTRICT;

CREATE TABLE public.pricing_currency_migration_draft_chunks (
    draft_id uuid NOT NULL,
    ordinal integer NOT NULL,
    row_count integer NOT NULL,
    content_hash text NOT NULL,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT pricing_currency_migration_draft_chunks_pkey PRIMARY KEY (draft_id, ordinal),
    CONSTRAINT ck_pcmdc_ordinal_positive CHECK ((ordinal >= 1)),
    CONSTRAINT ck_pcmdc_row_count CHECK ((row_count BETWEEN 1 AND 100))
);

ALTER TABLE ONLY public.pricing_currency_migration_draft_chunks
    ADD CONSTRAINT pricing_currency_migration_draft_chunks_draft_fkey
    FOREIGN KEY (draft_id) REFERENCES public.pricing_currency_migration_drafts(draft_id) ON DELETE RESTRICT;

CREATE TABLE public.pricing_currency_migration_draft_items (
    draft_id uuid NOT NULL,
    template_id integer NOT NULL,
    expected_version integer NOT NULL,
    expected_updated_at timestamp with time zone NOT NULL,
    input_price character varying(20) NOT NULL,
    output_price character varying(20) NOT NULL,
    cached_input_price character varying(20),
    cache_creation_price character varying(20),
    reasoning_price character varying(20),
    CONSTRAINT pricing_currency_migration_draft_items_pkey PRIMARY KEY (draft_id, template_id),
    CONSTRAINT ck_pcmdi_input_price CHECK ((prism_pricing_exact_decimal_canonical(input_price) = input_price)),
    CONSTRAINT ck_pcmdi_output_price CHECK ((prism_pricing_exact_decimal_canonical(output_price) = output_price)),
    CONSTRAINT ck_pcmdi_cached_input_price CHECK ((cached_input_price IS NULL OR prism_pricing_exact_decimal_canonical(cached_input_price) = cached_input_price)),
    CONSTRAINT ck_pcmdi_cache_creation_price CHECK ((cache_creation_price IS NULL OR prism_pricing_exact_decimal_canonical(cache_creation_price) = cache_creation_price)),
    CONSTRAINT ck_pcmdi_reasoning_price CHECK ((reasoning_price IS NULL OR prism_pricing_exact_decimal_canonical(reasoning_price) = reasoning_price))
);

ALTER TABLE ONLY public.pricing_currency_migration_draft_items
    ADD CONSTRAINT pricing_currency_migration_draft_items_draft_fkey
    FOREIGN KEY (draft_id) REFERENCES public.pricing_currency_migration_drafts(draft_id) ON DELETE RESTRICT;

-- Immutable currency migration ledger (SPEC 5.5/6.3.1).
CREATE TABLE public.currency_migration_ledger (
    operation_id uuid NOT NULL,
    operation_kind character varying(32) NOT NULL,
    profile_id integer NOT NULL,
    old_epoch_id bigint,
    old_epoch bigint,
    new_epoch_id bigint,
    new_epoch bigint,
    legacy_reporting_currency_evidence_id bigint,
    normalized_payload_hash text NOT NULL,
    inventory_id bigint,
    inventory_hash text NOT NULL,
    item_count integer NOT NULL,
    items_hash text NOT NULL,
    committed_result jsonb NOT NULL,
    committed_at timestamp with time zone NOT NULL,
    CONSTRAINT currency_migration_ledger_pkey PRIMARY KEY (operation_id),
    CONSTRAINT ck_cml_operation_kind CHECK ((operation_kind IN ('currency_cutover','repair_same_currency','archive_unused_fx'))),
    CONSTRAINT ck_cml_item_count_nonneg CHECK ((item_count >= 0)),
    CONSTRAINT ck_cml_new_epoch_presence CHECK (
        ((operation_kind = 'archive_unused_fx') AND (new_epoch_id IS NULL) AND (new_epoch IS NULL))
        OR ((operation_kind <> 'archive_unused_fx') AND (new_epoch_id IS NOT NULL) AND (new_epoch IS NOT NULL))
    ),
    CONSTRAINT ck_cml_old_epoch_presence CHECK (((old_epoch_id IS NULL) = (old_epoch IS NULL)))
);

ALTER TABLE ONLY public.currency_migration_ledger
    ADD CONSTRAINT currency_migration_ledger_operation_fkey
    FOREIGN KEY (operation_id) REFERENCES public.pricing_mutation_operations(operation_id)
    DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE ONLY public.currency_migration_ledger
    ADD CONSTRAINT currency_migration_ledger_inventory_fkey
    FOREIGN KEY (inventory_id) REFERENCES public.pricing_migration_inventories(inventory_id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.currency_migration_ledger
    ADD CONSTRAINT currency_migration_ledger_old_epoch_fkey
    FOREIGN KEY (old_epoch_id) REFERENCES public.reporting_currency_epochs(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.currency_migration_ledger
    ADD CONSTRAINT currency_migration_ledger_new_epoch_fkey
    FOREIGN KEY (new_epoch_id) REFERENCES public.reporting_currency_epochs(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.currency_migration_ledger
    ADD CONSTRAINT currency_migration_ledger_evidence_fkey
    FOREIGN KEY (legacy_reporting_currency_evidence_id) REFERENCES public.pricing_migration_legacy_reporting_currency_evidence(legacy_reporting_currency_evidence_id) ON DELETE RESTRICT;

CREATE UNIQUE INDEX uq_currency_migration_ledger_new_epoch
    ON public.currency_migration_ledger (new_epoch_id)
    WHERE (new_epoch_id IS NOT NULL);

CREATE TABLE public.currency_migration_ledger_items (
    operation_id uuid NOT NULL,
    ordinal integer NOT NULL,
    template_id integer NOT NULL,
    template_name_snapshot text NOT NULL,
    old_version integer NOT NULL,
    new_version integer NOT NULL,
    old_revision_id bigint,
    old_template_evidence_id bigint,
    new_revision_id bigint NOT NULL,
    input_price character varying(20) NOT NULL,
    output_price character varying(20) NOT NULL,
    cached_input_price character varying(20),
    cache_creation_price character varying(20),
    reasoning_price character varying(20),
    CONSTRAINT currency_migration_ledger_items_pkey PRIMARY KEY (operation_id, ordinal),
    CONSTRAINT ck_cmli_ordinal_positive CHECK ((ordinal >= 1)),
    CONSTRAINT ck_cmli_old_identity CHECK (((old_revision_id IS NULL) <> (old_template_evidence_id IS NULL))),
    CONSTRAINT ck_cmli_input_price CHECK ((prism_pricing_exact_decimal_canonical(input_price) = input_price)),
    CONSTRAINT ck_cmli_output_price CHECK ((prism_pricing_exact_decimal_canonical(output_price) = output_price)),
    CONSTRAINT ck_cmli_cached_input_price CHECK ((cached_input_price IS NULL OR prism_pricing_exact_decimal_canonical(cached_input_price) = cached_input_price)),
    CONSTRAINT ck_cmli_cache_creation_price CHECK ((cache_creation_price IS NULL OR prism_pricing_exact_decimal_canonical(cache_creation_price) = cache_creation_price)),
    CONSTRAINT ck_cmli_reasoning_price CHECK ((reasoning_price IS NULL OR prism_pricing_exact_decimal_canonical(reasoning_price) = reasoning_price)),
    CONSTRAINT uq_cmli_operation_template UNIQUE (operation_id, template_id)
);

ALTER TABLE ONLY public.currency_migration_ledger_items
    ADD CONSTRAINT currency_migration_ledger_items_operation_fkey
    FOREIGN KEY (operation_id) REFERENCES public.currency_migration_ledger(operation_id)
    DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE ONLY public.currency_migration_ledger_items
    ADD CONSTRAINT currency_migration_ledger_items_new_revision_fkey
    FOREIGN KEY (new_revision_id) REFERENCES public.pricing_template_revisions(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.currency_migration_ledger_items
    ADD CONSTRAINT currency_migration_ledger_items_old_revision_fkey
    FOREIGN KEY (old_revision_id) REFERENCES public.pricing_template_revisions(id) ON DELETE RESTRICT;

ALTER TABLE ONLY public.currency_migration_ledger_items
    ADD CONSTRAINT currency_migration_ledger_items_old_evidence_fkey
    FOREIGN KEY (old_template_evidence_id) REFERENCES public.pricing_migration_legacy_template_evidence(legacy_template_evidence_id) ON DELETE RESTRICT;

CREATE TRIGGER currency_migration_ledger_append_only
    BEFORE UPDATE OR DELETE ON public.currency_migration_ledger
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_migration_evidence_append_only();

CREATE TRIGGER currency_migration_ledger_items_append_only
    BEFORE UPDATE OR DELETE ON public.currency_migration_ledger_items
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_migration_evidence_append_only();

CREATE TRIGGER pricing_currency_migration_drafts_append_only
    BEFORE UPDATE OR DELETE ON public.pricing_currency_migration_drafts
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_migration_evidence_append_only();

-- ============================================================================
-- Part 8: endpoint_fx_rate_settings FK becomes RESTRICT (SPEC 11.1 step 5).
-- Endpoint deletes must fail closed while the active FX table is still the
-- cutoff evidence source; 000004 drops the table in the same transaction that
-- removes this guard.
-- ============================================================================

ALTER TABLE public.endpoint_fx_rate_settings DROP CONSTRAINT endpoint_fx_rate_settings_endpoint_id_fkey;
ALTER TABLE ONLY public.endpoint_fx_rate_settings
    ADD CONSTRAINT endpoint_fx_rate_settings_endpoint_id_fkey
    FOREIGN KEY (endpoint_id) REFERENCES public.endpoints(id) ON DELETE RESTRICT;

-- ============================================================================
-- Part 9: request_logs / usage_request_events additive pricing columns and
-- constraints (SPEC 6.4/6.5). Applied to the partitioned parents and to every
-- existing partition. Constraints are added NOT VALID and validated by 000004
-- after the status backfill and NOT NULL promotion.
-- ============================================================================

-- PostgreSQL 16 recurses ALTER TABLE parent ADD COLUMN / ADD CONSTRAINT
-- (NOT VALID) / SET NOT NULL / VALIDATE CONSTRAINT to every existing
-- partition, and enforces parent CHECK constraints on partition rows, so the
-- whole pricing surface is applied on the parents only. Per-partition
-- indexes are still created explicitly below (indexes do not recurse).

ALTER TABLE public.request_logs
    ADD COLUMN pricing_status character varying(20),
    ADD COLUMN pricing_resolution_kind character varying(50),
    ADD COLUMN pricing_evidence_trust character varying(24),
    ADD COLUMN missing_price_components text[],
    ADD COLUMN pricing_template_id_used integer,
    ADD COLUMN pricing_template_name_snapshot text,
    ADD COLUMN pricing_template_revision_id_used bigint,
    ADD COLUMN pricing_version_effective_at timestamp with time zone,
    ADD COLUMN reporting_currency_epoch integer;

ALTER TABLE public.usage_request_events
    ADD COLUMN pricing_status character varying(20),
    ADD COLUMN pricing_resolution_kind character varying(50),
    ADD COLUMN pricing_evidence_trust character varying(24),
    ADD COLUMN missing_price_components text[],
    ADD COLUMN pricing_template_id_used integer,
    ADD COLUMN pricing_template_name_snapshot text,
    ADD COLUMN pricing_template_revision_id_used bigint,
    ADD COLUMN pricing_version_effective_at timestamp with time zone,
    ADD COLUMN reporting_currency_epoch integer;

-- Shared status/reason/trust constraint set for both parents (SPEC 6.5).
-- All checks are NULL-tolerant so legacy rows pending backfill stay legal;
-- 000004 promotes NOT NULL and validates.

ALTER TABLE public.request_logs
    ADD CONSTRAINT pricing_status_check CHECK (pricing_status IN ('priced','unpriced','ineligible','unknown')) NOT VALID,
    ADD CONSTRAINT pricing_evidence_trust_check CHECK (pricing_evidence_trust IN ('trusted','legacy_untrusted')) NOT VALID,
    ADD CONSTRAINT pricing_unknown_requires_untrusted_check CHECK (pricing_status <> 'unknown' OR pricing_evidence_trust = 'legacy_untrusted') NOT VALID,
    ADD CONSTRAINT pricing_trusted_requires_known_check CHECK (pricing_evidence_trust = 'trusted' OR pricing_status IN ('unknown','ineligible')) NOT VALID,
    ADD CONSTRAINT pricing_unpriced_reason_check CHECK (
        (pricing_status = 'unpriced' AND unpriced_reason IS NOT NULL AND unpriced_reason IN ('PRICING_DISABLED','MISSING_TOKEN_USAGE','STREAM_USAGE_UNAVAILABLE','MISSING_PRICE_DATA'))
        OR (pricing_status <> 'unpriced' AND unpriced_reason IS NULL)
    ) NOT VALID,
    ADD CONSTRAINT pricing_resolution_kind_check CHECK (
        (unpriced_reason IS NOT NULL AND unpriced_reason = 'MISSING_PRICE_DATA' AND pricing_resolution_kind IS NOT NULL AND pricing_resolution_kind IN ('missing_component','currency_migration_required','unsupported_unit','snapshot_incoherent'))
        OR (unpriced_reason IS DISTINCT FROM 'MISSING_PRICE_DATA' AND pricing_resolution_kind IS NULL)
    ) NOT VALID,
    ADD CONSTRAINT pricing_missing_components_check CHECK (
        (pricing_resolution_kind IS NOT NULL AND pricing_resolution_kind = 'missing_component' AND missing_price_components IS NOT NULL AND cardinality(missing_price_components) > 0 AND public.prism_pricing_components_are_canonical(missing_price_components) IS TRUE)
        OR (pricing_resolution_kind IS DISTINCT FROM 'missing_component' AND missing_price_components IS NULL)
    ) NOT VALID,
    ADD CONSTRAINT pricing_epoch_nonneg_check CHECK (reporting_currency_epoch IS NULL OR reporting_currency_epoch >= 1) NOT VALID,
    ADD CONSTRAINT pricing_costs_coherence_check CHECK (
        pricing_evidence_trust = 'legacy_untrusted'
        OR public.prism_pricing_costs_are_canonical_all_or_none(pricing_status, ARRAY[
            input_cost_micros, output_cost_micros, reasoning_cost_micros,
            cache_read_input_cost_micros, cache_creation_input_cost_micros,
            total_cost_original_micros, total_cost_user_currency_micros
        ]) IS TRUE
    ) NOT VALID;

ALTER TABLE public.usage_request_events
    ADD CONSTRAINT pricing_status_check CHECK (pricing_status IN ('priced','unpriced','ineligible','unknown')) NOT VALID,
    ADD CONSTRAINT pricing_evidence_trust_check CHECK (pricing_evidence_trust IN ('trusted','legacy_untrusted')) NOT VALID,
    ADD CONSTRAINT pricing_unknown_requires_untrusted_check CHECK (pricing_status <> 'unknown' OR pricing_evidence_trust = 'legacy_untrusted') NOT VALID,
    ADD CONSTRAINT pricing_trusted_requires_known_check CHECK (pricing_evidence_trust = 'trusted' OR pricing_status IN ('unknown','ineligible')) NOT VALID,
    ADD CONSTRAINT pricing_unpriced_reason_check CHECK (
        (pricing_status = 'unpriced' AND unpriced_reason IS NOT NULL AND unpriced_reason IN ('PRICING_DISABLED','MISSING_TOKEN_USAGE','STREAM_USAGE_UNAVAILABLE','MISSING_PRICE_DATA'))
        OR (pricing_status <> 'unpriced' AND unpriced_reason IS NULL)
    ) NOT VALID,
    ADD CONSTRAINT pricing_resolution_kind_check CHECK (
        (unpriced_reason IS NOT NULL AND unpriced_reason = 'MISSING_PRICE_DATA' AND pricing_resolution_kind IS NOT NULL AND pricing_resolution_kind IN ('missing_component','currency_migration_required','unsupported_unit','snapshot_incoherent'))
        OR (unpriced_reason IS DISTINCT FROM 'MISSING_PRICE_DATA' AND pricing_resolution_kind IS NULL)
    ) NOT VALID,
    ADD CONSTRAINT pricing_missing_components_check CHECK (
        (pricing_resolution_kind IS NOT NULL AND pricing_resolution_kind = 'missing_component' AND missing_price_components IS NOT NULL AND cardinality(missing_price_components) > 0 AND public.prism_pricing_components_are_canonical(missing_price_components) IS TRUE)
        OR (pricing_resolution_kind IS DISTINCT FROM 'missing_component' AND missing_price_components IS NULL)
    ) NOT VALID,
    ADD CONSTRAINT pricing_epoch_nonneg_check CHECK (reporting_currency_epoch IS NULL OR reporting_currency_epoch >= 1) NOT VALID,
    ADD CONSTRAINT pricing_costs_coherence_check CHECK (
        pricing_evidence_trust = 'legacy_untrusted'
        OR public.prism_pricing_costs_are_canonical_all_or_none(pricing_status, ARRAY[
            input_cost_micros, output_cost_micros, reasoning_cost_micros,
            cache_read_input_cost_micros, cache_creation_input_cost_micros,
            total_cost_original_micros, total_cost_user_currency_micros
        ]) IS TRUE
    ) NOT VALID;

-- ============================================================================
-- Part 10: deferred invariant triggers (SPEC 6.1/6.2/6.3/6.3.1)
-- ============================================================================

-- Template current pointer: every active logical template points at the
-- highest public version revision of the same template; no orphan revision
-- may exist for an active template. The single-transaction runner leaves no
-- committed transitional window, so the strict invariant is installed now and
-- 000004 re-validates it as part of its finalize gate.
CREATE FUNCTION public.prism_pricing_template_pointer_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    row record;
    expected_id bigint;
BEGIN
    FOR row IN
        SELECT templates.id
        FROM public.pricing_templates AS templates
        WHERE templates.deleted_at IS NULL
    LOOP
        SELECT revisions.id INTO expected_id
        FROM public.pricing_template_revisions AS revisions
        WHERE revisions.template_id = row.id
        ORDER BY revisions.version DESC
        LIMIT 1;
        IF expected_id IS NULL THEN
            IF EXISTS (
                SELECT 1 FROM public.pricing_templates AS templates
                WHERE templates.id = row.id AND templates.current_revision_id IS NOT NULL
            ) THEN
                RAISE EXCEPTION 'pricing_template_pointer_invariant: template % has a current revision pointer but no revisions', row.id
                    USING ERRCODE = 'P0001';
            END IF;
        ELSE
            IF NOT EXISTS (
                SELECT 1 FROM public.pricing_templates AS templates
                WHERE templates.id = row.id AND templates.current_revision_id = expected_id
            ) THEN
                RAISE EXCEPTION 'pricing_template_pointer_invariant: template % current_revision_id must point at revision % (highest version)', row.id, expected_id
                    USING ERRCODE = 'P0001';
            END IF;
        END IF;
    END LOOP;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER pricing_template_pointer_guard
    AFTER INSERT OR UPDATE ON public.pricing_templates
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_template_pointer_guard();

CREATE CONSTRAINT TRIGGER pricing_template_pointer_guard_revision
    AFTER INSERT ON public.pricing_template_revisions
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_template_pointer_guard();

-- Revision ownership: a revision's created_by_operation_id must reference a
-- committed operation of the matching profile and result kind, and the
-- operation must carry a result item for this template with a revision
-- creating action. legacy_backfill rows never reference an operation.
CREATE FUNCTION public.prism_pricing_revision_operation_ownership()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    row record;
    kind_mapping text;
    item_action text;
BEGIN
    FOR row IN
        SELECT revisions.id, revisions.template_id, revisions.created_by_kind, revisions.created_by_operation_id
        FROM public.pricing_template_revisions AS revisions
    LOOP
        IF row.created_by_operation_id IS NULL THEN
            IF row.created_by_kind <> 'legacy_backfill' THEN
                RAISE EXCEPTION 'pricing_revision_ownership: revision % has no operation but kind %', row.id, row.created_by_kind
                    USING ERRCODE = 'P0001';
            END IF;
            CONTINUE;
        END IF;
        kind_mapping := CASE row.created_by_kind
            WHEN 'manual_create' THEN 'template_create'
            WHEN 'manual_edit' THEN 'template_update'
            WHEN 'import' THEN 'template_import'
            WHEN 'currency_migration' THEN 'currency_cutover'
            WHEN 'legacy_migration_repair' THEN 'repair_same_currency'
            ELSE NULL
        END;
        IF kind_mapping IS NULL THEN
            RAISE EXCEPTION 'pricing_revision_ownership: revision % kind % cannot reference an operation', row.id, row.created_by_kind
                USING ERRCODE = 'P0001';
        END IF;
        SELECT result_kind, (
            SELECT action FROM public.pricing_mutation_result_items AS items
            WHERE items.operation_id = operations.operation_id AND items.template_id = row.template_id
            ORDER BY items.ordinal ASC LIMIT 1
        ) INTO kind_mapping, item_action
        FROM public.pricing_mutation_operations AS operations
        WHERE operations.operation_id = row.created_by_operation_id;
        IF kind_mapping IS NULL THEN
            RAISE EXCEPTION 'pricing_revision_ownership: revision % references unknown operation %', row.id, row.created_by_operation_id
                USING ERRCODE = 'P0001';
        END IF;
        IF item_action IS NULL OR item_action NOT IN ('created','revision_created','metadata_and_revision') THEN
            RAISE EXCEPTION 'pricing_revision_ownership: operation % has no revision-creating result item for template %', row.created_by_operation_id, row.template_id
                USING ERRCODE = 'P0001';
        END IF;
        -- profile consistency is verified against the template owner
        IF NOT EXISTS (
            SELECT 1 FROM public.pricing_templates AS templates
            JOIN public.pricing_mutation_operations AS operations ON operations.operation_id = row.created_by_operation_id
            WHERE templates.id = row.template_id AND operations.profile_id = templates.profile_id
        ) THEN
            RAISE EXCEPTION 'pricing_revision_ownership: operation profile mismatch for revision %', row.id
                USING ERRCODE = 'P0001';
        END IF;
    END LOOP;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER pricing_revision_operation_ownership
    AFTER INSERT ON public.pricing_template_revisions
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_revision_operation_ownership();

-- active_epoch revisions must reference an epoch row of the same profile
-- whose currency code matches the revision currency code.
CREATE FUNCTION public.prism_pricing_revision_epoch_attribution()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    row record;
BEGIN
    FOR row IN
        SELECT revisions.id, revisions.template_id, revisions.currency_attribution,
               revisions.currency_code, revisions.reporting_currency_epoch_id
        FROM public.pricing_template_revisions AS revisions
        WHERE revisions.currency_attribution = 'active_epoch'
    LOOP
        IF NOT EXISTS (
            SELECT 1 FROM public.reporting_currency_epochs AS epochs
            JOIN public.pricing_templates AS templates ON templates.id = row.template_id
            WHERE epochs.id = row.reporting_currency_epoch_id
              AND epochs.profile_id = templates.profile_id
              AND epochs.currency_code = row.currency_code
        ) THEN
            RAISE EXCEPTION 'pricing_revision_epoch_attribution: revision % epoch attribution mismatch', row.id
                USING ERRCODE = 'P0001';
        END IF;
    END LOOP;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER pricing_revision_epoch_attribution
    AFTER INSERT ON public.pricing_template_revisions
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_revision_epoch_attribution();

-- Settings/epoch coherence: when a settings row points at an active epoch the
-- epoch must be the unique active row of the same profile and the canonical
-- v2 currency code/symbol must match the epoch exactly. Pending invalid
-- reporting-currency profiles may hold a null pointer only while v2 columns
-- are null and the typed issues prove the source (SPEC 5.1/5.2).
CREATE FUNCTION public.prism_pricing_settings_epoch_coherence()
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
               settings.pricing_report_currency_code_v2,
               settings.pricing_report_currency_symbol_v2,
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
            IF epoch_code IS DISTINCT FROM row.pricing_report_currency_code_v2
               OR epoch_symbol IS DISTINCT FROM row.pricing_report_currency_symbol_v2 THEN
                RAISE EXCEPTION 'pricing_settings_epoch_coherence: profile % settings currency diverges from active epoch', row.profile_id
                    USING ERRCODE = 'P0001';
            END IF;
        ELSE
            IF row.pricing_report_currency_code_v2 IS NOT NULL OR row.pricing_report_currency_symbol_v2 IS NOT NULL THEN
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

CREATE CONSTRAINT TRIGGER pricing_settings_epoch_coherence
    AFTER INSERT OR UPDATE ON public.user_settings
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_settings_epoch_coherence();

CREATE CONSTRAINT TRIGGER pricing_settings_epoch_coherence_epochs
    AFTER INSERT OR UPDATE ON public.reporting_currency_epochs
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_settings_epoch_coherence();

-- Epoch lineage guards: no DELETE; profile/epoch/code/effective_at immutable
-- after insert; superseded_at only transitions NULL -> value once; active
-- row symbol-only updates allowed (SPEC 6.3).
CREATE FUNCTION public.prism_pricing_epochs_mutable_guard()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'reporting_currency_epochs are append-only'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.profile_id IS DISTINCT FROM NEW.profile_id
       OR OLD.epoch IS DISTINCT FROM NEW.epoch
       OR OLD.currency_code IS DISTINCT FROM NEW.currency_code
       OR OLD.effective_at IS DISTINCT FROM NEW.effective_at
       OR OLD.created_at IS DISTINCT FROM NEW.created_at THEN
        RAISE EXCEPTION 'reporting_currency_epochs identity fields are immutable'
            USING ERRCODE = '55000';
    END IF;
    IF OLD.superseded_at IS NOT NULL AND NEW.superseded_at IS DISTINCT FROM OLD.superseded_at THEN
        RAISE EXCEPTION 'reporting_currency_epochs superseded_at is immutable once set'
            USING ERRCODE = '55000';
    END IF;
    IF NEW.superseded_at IS NOT NULL AND OLD.superseded_at IS NULL THEN
        IF NEW.updated_at = OLD.updated_at THEN
            NEW.updated_at := clock_timestamp();
        END IF;
        RETURN NEW;
    END IF;
    IF OLD.currency_symbol IS DISTINCT FROM NEW.currency_symbol OR OLD.updated_at IS DISTINCT FROM NEW.updated_at THEN
        IF OLD.superseded_at IS NULL THEN
            RETURN NEW;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER reporting_currency_epochs_mutable_guard
    BEFORE UPDATE OR DELETE ON public.reporting_currency_epochs
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_epochs_mutable_guard();

-- Inventory child reconciliation: parent counts and hash roots must match the
-- actual child rows exactly (SPEC 6.3.1). Hash roots are md5 over the ordered
-- child row_hash list, matching the backfill computation.
CREATE FUNCTION public.prism_pricing_inventory_reconciliation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    row record;
    actual_fx_count integer;
    actual_assessment_count integer;
    actual_dependency_count integer;
    actual_template_count integer;
    actual_reporting_count integer;
    actual_fx_hash text;
    actual_template_hash text;
    actual_reporting_hash text;
BEGIN
    FOR row IN
        SELECT inventory_id, fx_evidence_count, fx_assessment_count, fx_dependency_count,
               template_evidence_count, reporting_currency_evidence_count,
               fx_evidence_hash_root, template_evidence_hash_root,
               reporting_currency_evidence_hash_root
        FROM public.pricing_migration_inventories
    LOOP
        SELECT count(*), md5(string_agg(row_hash, '' ORDER BY source_fx_row_id))
        INTO actual_fx_count, actual_fx_hash
        FROM public.currency_migration_legacy_fx_evidence
        WHERE inventory_id = row.inventory_id;
        SELECT count(*)
        INTO actual_assessment_count
        FROM public.currency_migration_legacy_fx_assessments AS assessments
        JOIN public.currency_migration_legacy_fx_evidence AS evidence ON evidence.legacy_fx_evidence_id = assessments.legacy_fx_evidence_id
        WHERE evidence.inventory_id = row.inventory_id;
        SELECT count(*)
        INTO actual_dependency_count
        FROM public.currency_migration_legacy_fx_dependencies
        WHERE inventory_id = row.inventory_id;
        SELECT count(*), md5(string_agg(row_hash, '' ORDER BY template_id))
        INTO actual_template_count, actual_template_hash
        FROM public.pricing_migration_legacy_template_evidence
        WHERE inventory_id = row.inventory_id;
        SELECT count(*), md5(string_agg(row_hash, '' ORDER BY legacy_reporting_currency_evidence_id))
        INTO actual_reporting_count, actual_reporting_hash
        FROM public.pricing_migration_legacy_reporting_currency_evidence
        WHERE inventory_id = row.inventory_id;
        IF row.fx_evidence_count IS DISTINCT FROM actual_fx_count
           OR row.fx_assessment_count IS DISTINCT FROM actual_assessment_count
           OR row.fx_dependency_count IS DISTINCT FROM actual_dependency_count
           OR row.template_evidence_count IS DISTINCT FROM actual_template_count
           OR row.reporting_currency_evidence_count IS DISTINCT FROM actual_reporting_count
           OR row.fx_evidence_hash_root IS DISTINCT FROM actual_fx_hash
           OR row.template_evidence_hash_root IS DISTINCT FROM actual_template_hash
           OR row.reporting_currency_evidence_hash_root IS DISTINCT FROM actual_reporting_hash THEN
            RAISE EXCEPTION 'pricing_migration_inventory_reconciliation: inventory % counts/hashes do not match children', row.inventory_id
                USING ERRCODE = 'P0001';
        END IF;
    END LOOP;
    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER pricing_migration_inventory_reconciliation
    AFTER INSERT OR UPDATE ON public.pricing_migration_inventories
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_inventory_reconciliation();

CREATE CONSTRAINT TRIGGER pricing_migration_inventory_reconciliation_children
    AFTER INSERT ON public.currency_migration_legacy_fx_evidence
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_inventory_reconciliation();

CREATE CONSTRAINT TRIGGER pricing_migration_inventory_reconciliation_children_2
    AFTER INSERT ON public.pricing_migration_legacy_template_evidence
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_inventory_reconciliation();

CREATE CONSTRAINT TRIGGER pricing_migration_inventory_reconciliation_children_3
    AFTER INSERT ON public.pricing_migration_legacy_reporting_currency_evidence
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_inventory_reconciliation();

CREATE CONSTRAINT TRIGGER pricing_migration_inventory_reconciliation_children_4
    AFTER INSERT ON public.currency_migration_legacy_fx_assessments
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_inventory_reconciliation();

CREATE CONSTRAINT TRIGGER pricing_migration_inventory_reconciliation_children_5
    AFTER INSERT ON public.currency_migration_legacy_fx_dependencies
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION public.prism_pricing_inventory_reconciliation();

-- ============================================================================
-- Part 11: deterministic upgrade backfills (SPEC 11.2/11.3/11.4/11.5)
-- ============================================================================

-- Provenance: the migrator passes the transaction-local fresh marker. A
-- missing marker is a hard fail; migrations are never applied outside the
-- runner.
DO $$
DECLARE
    fresh text;
BEGIN
    fresh := current_setting('prism.migration_fresh_from_zero', true);
    IF fresh IS NULL THEN
        RAISE EXCEPTION '000003 requires the transaction-local prism.migration_fresh_from_zero provenance from the migrator'
            USING ERRCODE = 'P0001';
    END IF;
END;
$$;

-- Freshness cross-proof: profiles/user_settings and the template/event/FX
-- parents must all be empty before the fresh path may initialize the final
-- singleton state (SPEC 11.1). Anything else goes through the upgrade path.
DO $$
DECLARE
    fresh_setting text;
    fresh boolean;
    profile_count bigint;
    settings_count bigint;
    template_count bigint;
    request_count bigint;
    usage_count bigint;
    fx_count bigint;
    migration_time timestamptz;
BEGIN
    fresh_setting := current_setting('prism.migration_fresh_from_zero', true);
    SELECT count(*) INTO profile_count FROM public.profiles;
    SELECT count(*) INTO settings_count FROM public.user_settings;
    SELECT count(*) INTO template_count FROM public.pricing_templates;
    SELECT count(*) INTO request_count FROM public.request_logs;
    SELECT count(*) INTO usage_count FROM public.usage_request_events;
    SELECT count(*) INTO fx_count FROM public.endpoint_fx_rate_settings;
    fresh := fresh_setting = 'true'
        AND profile_count = 0 AND settings_count = 0 AND template_count = 0
        AND request_count = 0 AND usage_count = 0 AND fx_count = 0;
    migration_time := clock_timestamp();

    IF fresh THEN
        INSERT INTO public.pricing_schema_transition_state (
            id, phase, schema_generation, writer_fence_generation, lease_generation,
            lease_acquisition_open, finalizer_owner_id, finalizer_expires_at,
            finalizer_fencing_token, created_at, updated_at
        ) VALUES (
            1, 'finalize_ready', 1, 0, 1, FALSE, NULL, NULL, 1, migration_time, migration_time
        );
    ELSE
        INSERT INTO public.pricing_schema_transition_state (
            id, phase, schema_generation, writer_fence_generation, lease_generation,
            lease_acquisition_open, finalizer_owner_id, finalizer_expires_at,
            finalizer_fencing_token, created_at, updated_at
        ) VALUES (
            1, 'legacy_writer_open', 1, 0, 1, TRUE, NULL, NULL, 0, migration_time, migration_time
        );
    END IF;
END;
$$;

-- Temporary staging table for reporting-currency evidence (inserted into
-- the immutable evidence table by 11.6 once the inventory parent exists).
CREATE UNLOGGED TABLE public.prism_pricing_migration_reporting_stage (
    profile_id integer NOT NULL,
    raw_report_currency_code text NOT NULL,
    raw_report_currency_symbol text NOT NULL,
    settings_updated_at timestamp with time zone NOT NULL,
    validation_codes text[] NOT NULL,
    row_hash text NOT NULL
);

-- 11.1 epoch backfill: canonical legacy report settings produce epoch 1 and
-- the canonical v2 projection; invalid raw settings freeze immutable
-- reporting-currency evidence and keep the profile pending with a null
-- pointer (SPEC 11.4 / 5.6).
DO $$
DECLARE
    row record;
    canonical_code text;
    canonical_symbol text;
    validation_codes text[];
    epoch_id bigint;
    migration_time timestamptz;
    evidence_hash text;
    inventory_id bigint;
BEGIN
    migration_time := clock_timestamp();
    FOR row IN
        SELECT settings.profile_id, settings.report_currency_code, settings.report_currency_symbol,
               settings.updated_at
        FROM public.user_settings AS settings
        ORDER BY settings.profile_id ASC
    LOOP
        canonical_code := prism_pricing_currency_code_canonical(row.report_currency_code);
        canonical_symbol := prism_pricing_trim_unicode_whitespace(row.report_currency_symbol);
        validation_codes := ARRAY[]::text[];
        IF canonical_code IS NULL THEN
            validation_codes := array_append(validation_codes, 'invalid_reporting_currency_code');
        END IF;
        IF canonical_symbol IS NULL OR canonical_symbol = '' OR NOT prism_pricing_currency_symbol_valid(canonical_symbol) THEN
            validation_codes := array_append(validation_codes, 'invalid_reporting_currency_symbol');
            canonical_symbol := NULL;
        END IF;

        IF cardinality(validation_codes) = 0 THEN
            INSERT INTO public.reporting_currency_epochs (
                profile_id, epoch, currency_code, currency_symbol, effective_at,
                superseded_at, created_at, updated_at
            ) VALUES (
                row.profile_id, 1, canonical_code, canonical_symbol, NULL, NULL,
                migration_time, migration_time
            ) RETURNING id INTO epoch_id;
            UPDATE public.user_settings
            SET current_reporting_currency_epoch_id = epoch_id,
                pricing_report_currency_code_v2 = canonical_code,
                pricing_report_currency_symbol_v2 = canonical_symbol,
                updated_at = migration_time
            WHERE profile_id = row.profile_id;
        ELSE
            evidence_hash := md5(concat_ws(E'\x1f',
                row.profile_id::text,
                row.report_currency_code,
                row.report_currency_symbol,
                row.updated_at::text,
                validation_codes::text));
            INSERT INTO public.prism_pricing_migration_reporting_stage (
                profile_id, raw_report_currency_code, raw_report_currency_symbol,
                settings_updated_at, validation_codes, row_hash
            ) VALUES (
                row.profile_id, row.report_currency_code, row.report_currency_symbol,
                row.updated_at, validation_codes, evidence_hash
            );
        END IF;
    END LOOP;
END;
$$;

-- Temporary staging table used by the template backfill (dropped at the end
-- of this migration): keeps template evidence payloads deterministic until
-- the inventory parent with precomputed counts/hashes is inserted.
CREATE UNLOGGED TABLE public.prism_pricing_migration_template_stage (
    template_id integer NOT NULL,
    profile_id integer NOT NULL,
    public_version integer NOT NULL,
    pricing_unit character varying(20),
    currency_code character varying(3),
    input_price character varying(20),
    output_price character varying(20),
    cached_input_price character varying(20),
    cache_creation_price character varying(20),
    reasoning_price character varying(20),
    issue_codes text[] NOT NULL,
    row_hash text NOT NULL
);

-- 11.2 template/revision backfill: canonical same-currency templates get one
-- active-epoch vN baseline revision; foreign-currency templates get an
-- isolated legacy_foreign vN; templates on invalid-currency profiles get a
-- pre_epoch_pending vN; invalid unit/price templates get evidence only and a
-- controlled null pointer. Every legacy template also keeps an immutable
-- raw-value evidence row for audit (SPEC 11.2). This pass only collects rows;
-- inventory parents are inserted afterwards with precomputed counts/hashes.
DO $$
DECLARE
    row record;
    canonical_prices text[];
    issue_codes text[];
    profile_code text;
    profile_epoch_id bigint;
    attribution text;
    revision_id bigint;
    evidence_hash text;
    raw_price_values text[];
    migration_time timestamptz;
BEGIN
    migration_time := clock_timestamp();
    FOR row IN
        SELECT templates.id, templates.profile_id, templates.pricing_unit,
               templates.pricing_currency_code, templates.input_price, templates.output_price,
               templates.cached_input_price, templates.cache_creation_price,
               templates.reasoning_price, templates.version
        FROM public.pricing_templates AS templates
        ORDER BY templates.profile_id ASC, templates.id ASC
    LOOP
        SELECT epochs.id, epochs.currency_code
        INTO profile_epoch_id, profile_code
        FROM public.reporting_currency_epochs AS epochs
        JOIN public.user_settings AS settings ON settings.current_reporting_currency_epoch_id = epochs.id
        WHERE settings.profile_id = row.profile_id;

        issue_codes := ARRAY[]::text[];
        canonical_prices := ARRAY[
            prism_pricing_exact_decimal_canonical(row.input_price),
            prism_pricing_exact_decimal_canonical(row.output_price),
            prism_pricing_exact_decimal_canonical(row.cached_input_price),
            prism_pricing_exact_decimal_canonical(row.cache_creation_price),
            prism_pricing_exact_decimal_canonical(row.reasoning_price)
        ];
        IF row.pricing_unit IS NULL OR trim(row.pricing_unit) <> 'PER_1M' THEN
            issue_codes := array_append(issue_codes, 'unsupported_pricing_unit');
        END IF;
        IF canonical_prices[1] IS NULL OR canonical_prices[2] IS NULL
           OR canonical_prices[3] IS NULL OR canonical_prices[4] IS NULL OR canonical_prices[5] IS NULL THEN
            issue_codes := array_append(issue_codes, 'invalid_price_encoding');
        END IF;
        IF prism_pricing_currency_code_canonical(row.pricing_currency_code) IS NULL THEN
            issue_codes := array_append(issue_codes, 'invalid_price_encoding');
        END IF;

        -- Foreign-currency templates are blocking issues; detect before the
        -- evidence hash is computed so the staged evidence and inventory
        -- hashes stay deterministic.
        IF prism_pricing_currency_code_canonical(row.pricing_currency_code) IS NOT NULL
           AND profile_code IS NOT NULL
           AND prism_pricing_currency_code_canonical(row.pricing_currency_code) IS DISTINCT FROM profile_code
           AND NOT (issue_codes @> ARRAY['foreign_currency_template']::text[]) THEN
            issue_codes := array_append(issue_codes, 'foreign_currency_template');
        END IF;

        evidence_hash := md5(concat_ws(E'\x1f',
            row.id::text, row.profile_id::text, COALESCE(row.version, 1)::text,
            COALESCE(row.pricing_unit, ''), COALESCE(row.pricing_currency_code, ''),
            COALESCE(row.input_price, ''), COALESCE(row.output_price, ''),
            COALESCE(row.cached_input_price, ''), COALESCE(row.cache_creation_price, ''),
            COALESCE(row.reasoning_price, ''), COALESCE(issue_codes::text, '{}')));

        -- Template evidence rows are inserted later, once the inventory
        -- parent exists; store the computed payload in a temporary staging
        -- table so counts/hashes stay deterministic.
        INSERT INTO public.prism_pricing_migration_template_stage (
            template_id, profile_id, public_version, pricing_unit, currency_code,
            input_price, output_price, cached_input_price, cache_creation_price,
            reasoning_price, issue_codes, row_hash
        ) VALUES (
            row.id, row.profile_id, COALESCE(row.version, 1), row.pricing_unit,
            row.pricing_currency_code, row.input_price, row.output_price,
            row.cached_input_price, row.cache_creation_price, row.reasoning_price,
            issue_codes, evidence_hash
        );

        IF cardinality(issue_codes) > 0 OR profile_epoch_id IS NULL THEN
            -- Invalid evidence or no active epoch: controlled null pointer.
            -- Valid prices on invalid-currency profiles still get a
            -- pre_epoch_pending baseline revision.
            IF cardinality(issue_codes) = 0 AND profile_epoch_id IS NULL THEN
                INSERT INTO public.pricing_template_revisions (
                    template_id, version, pricing_unit, currency_code,
                    reporting_currency_epoch_id, reporting_currency_epoch,
                    currency_attribution, input_price, output_price,
                    cached_input_price, cache_creation_price, reasoning_price,
                    effective_at, created_at, created_by_kind, created_by_operation_id
                ) VALUES (
                    row.id, COALESCE(row.version, 1), 'PER_1M',
                    prism_pricing_currency_code_canonical(row.pricing_currency_code),
                    NULL, NULL, 'pre_epoch_pending',
                    canonical_prices[1], canonical_prices[2], canonical_prices[3],
                    canonical_prices[4], canonical_prices[5],
                    NULL, migration_time, 'legacy_backfill', NULL
                ) RETURNING id INTO revision_id;
                UPDATE public.pricing_templates SET current_revision_id = revision_id WHERE id = row.id;
            END IF;
            CONTINUE;
        END IF;

        attribution := 'active_epoch';
        IF prism_pricing_currency_code_canonical(row.pricing_currency_code) IS DISTINCT FROM profile_code THEN
            attribution := 'legacy_foreign';
        END IF;
        INSERT INTO public.pricing_template_revisions (
            template_id, version, pricing_unit, currency_code,
            reporting_currency_epoch_id, reporting_currency_epoch,
            currency_attribution, input_price, output_price,
            cached_input_price, cache_creation_price, reasoning_price,
            effective_at, created_at, created_by_kind, created_by_operation_id
        ) VALUES (
            row.id, COALESCE(row.version, 1), 'PER_1M',
            prism_pricing_currency_code_canonical(row.pricing_currency_code),
            CASE WHEN attribution = 'active_epoch' THEN profile_epoch_id ELSE NULL END,
            CASE WHEN attribution = 'active_epoch' THEN 1 ELSE NULL END,
            attribution,
            canonical_prices[1], canonical_prices[2], canonical_prices[3],
            canonical_prices[4], canonical_prices[5],
            NULL, migration_time, 'legacy_backfill', NULL
        ) RETURNING id INTO revision_id;
        UPDATE public.pricing_templates SET current_revision_id = revision_id WHERE id = row.id;
    END LOOP;
END;
$$;



-- 11.3 FX evidence/assessment/dependency backfill (SPEC 11.5 / 6.3.1).
-- A raw FX row is a live dependency when a model access target from the same
-- source model id routes to a connection on the same endpoint id that owns a
-- pricing template whose currency differs from the canonical report currency
-- (the only path the legacy runtime could consume the FX rate). Everything
-- else is assessed `unused`. Rows are staged so the inventory parent can be
-- inserted with precomputed counts and hash roots.
CREATE UNLOGGED TABLE public.prism_pricing_migration_fx_stage (
    source_fx_row_id integer NOT NULL,
    profile_id integer NOT NULL,
    model_id character varying(200) NOT NULL,
    endpoint_id integer NOT NULL,
    fx_rate character varying(20) NOT NULL,
    source_created_at timestamp with time zone NOT NULL,
    source_updated_at timestamp with time zone NOT NULL,
    row_hash text NOT NULL,
    assessment character varying(16) NOT NULL,
    dependency_count integer NOT NULL,
    report_code character varying(3)
);

DO $$
DECLARE
    row record;
    dep record;
    assessment text;
    evidence_hash text;
    dependency_count integer;
    migration_time timestamptz;
BEGIN
    migration_time := clock_timestamp();
    FOR row IN
        SELECT fx.id, fx.profile_id, fx.model_id, fx.endpoint_id, fx.fx_rate,
               fx.created_at, fx.updated_at,
               COALESCE(settings.pricing_report_currency_code_v2, '') AS report_code
        FROM public.endpoint_fx_rate_settings AS fx
        LEFT JOIN public.user_settings AS settings ON settings.profile_id = fx.profile_id
        ORDER BY fx.profile_id ASC, fx.id ASC
    LOOP
        evidence_hash := md5(concat_ws(E'\x1f',
            row.id::text, row.profile_id::text, row.model_id, row.endpoint_id::text,
            row.fx_rate, row.created_at::text, row.updated_at::text));

        SELECT count(*) INTO dependency_count
        FROM public.model_access_targets AS targets
        JOIN public.model_configs AS source_models ON source_models.id = targets.source_model_config_id
        JOIN public.connections AS connections ON connections.id = targets.target_connection_id
        JOIN public.pricing_templates AS templates ON templates.id = connections.pricing_template_id
        WHERE targets.profile_id = row.profile_id
          AND source_models.model_id = row.model_id
          AND connections.endpoint_id = row.endpoint_id
          AND templates.deleted_at IS NULL
          AND templates.pricing_currency_code IS NOT NULL
          AND prism_pricing_currency_code_canonical(templates.pricing_currency_code) IS DISTINCT FROM row.report_code;

        IF dependency_count > 0 THEN
            assessment := 'has_live';
        ELSE
            assessment := 'unused';
        END IF;

        INSERT INTO public.prism_pricing_migration_fx_stage (
            source_fx_row_id, profile_id, model_id, endpoint_id, fx_rate,
            source_created_at, source_updated_at, row_hash, assessment,
            dependency_count, report_code
        ) VALUES (
            row.id, row.profile_id, row.model_id, row.endpoint_id, row.fx_rate,
            row.created_at, row.updated_at, evidence_hash, assessment,
            dependency_count, row.report_code
        );
    END LOOP;
END;
$$;

-- 11.4 status backfill (SPEC 11.3): request_logs and usage_request_events
-- rows get the conservative legacy projection. Non-2xx -> ineligible;
-- canonical/coherent 2xx with priced_flag -> priced or unpriced (reason
-- preserved when typed and valid); every other known 2xx with any non-
-- canonical/partial/incoherent evidence -> unknown + legacy_untrusted.
-- Invalid final HTTP statuses never enter the four states: they are
-- quarantined in 11.5.
DO $$
BEGIN
    UPDATE public.request_logs
    SET pricing_status = CASE
            WHEN status_code < 100 OR status_code > 599 THEN NULL
            WHEN status_code NOT BETWEEN 200 AND 299 THEN 'ineligible'
            WHEN NOT public.prism_pricing_legacy_snapshots_coherent(
                pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output,
                pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input,
                pricing_snapshot_reasoning, currency_code_original, report_currency_code,
                report_currency_symbol, fx_rate_used, fx_rate_source, pricing_config_version_used)
                THEN 'unknown'
            WHEN priced_flag = TRUE
                AND input_cost_micros IS NOT NULL AND output_cost_micros IS NOT NULL
                AND reasoning_cost_micros IS NOT NULL AND cache_read_input_cost_micros IS NOT NULL
                AND cache_creation_input_cost_micros IS NOT NULL
                AND total_cost_original_micros IS NOT NULL AND total_cost_user_currency_micros IS NOT NULL
                THEN 'priced'
            WHEN priced_flag = FALSE
                AND unpriced_reason IN ('PRICING_DISABLED','MISSING_TOKEN_USAGE','STREAM_USAGE_UNAVAILABLE')
                AND input_cost_micros IS NULL AND output_cost_micros IS NULL
                AND reasoning_cost_micros IS NULL AND cache_read_input_cost_micros IS NULL
                AND cache_creation_input_cost_micros IS NULL
                AND total_cost_original_micros IS NULL AND total_cost_user_currency_micros IS NULL
                THEN 'unpriced'
            ELSE 'unknown'
        END,
        unpriced_reason = CASE
            WHEN status_code < 100 OR status_code > 599 THEN NULL
            WHEN status_code NOT BETWEEN 200 AND 299 THEN NULL
            WHEN NOT public.prism_pricing_legacy_snapshots_coherent(
                pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output,
                pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input,
                pricing_snapshot_reasoning, currency_code_original, report_currency_code,
                report_currency_symbol, fx_rate_used, fx_rate_source, pricing_config_version_used)
                THEN NULL
            WHEN priced_flag = FALSE
                AND unpriced_reason IN ('PRICING_DISABLED','MISSING_TOKEN_USAGE','STREAM_USAGE_UNAVAILABLE')
                AND input_cost_micros IS NULL AND output_cost_micros IS NULL
                AND reasoning_cost_micros IS NULL AND cache_read_input_cost_micros IS NULL
                AND cache_creation_input_cost_micros IS NULL
                AND total_cost_original_micros IS NULL AND total_cost_user_currency_micros IS NULL
                THEN unpriced_reason
            ELSE NULL
        END,
        pricing_evidence_trust = CASE
            WHEN status_code < 100 OR status_code > 599 THEN NULL
            WHEN public.prism_pricing_legacy_snapshots_coherent(
                pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output,
                pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input,
                pricing_snapshot_reasoning, currency_code_original, report_currency_code,
                report_currency_symbol, fx_rate_used, fx_rate_source, pricing_config_version_used)
                AND (
                    (status_code NOT BETWEEN 200 AND 299
                     AND input_cost_micros IS NULL AND output_cost_micros IS NULL
                     AND reasoning_cost_micros IS NULL AND cache_read_input_cost_micros IS NULL
                     AND cache_creation_input_cost_micros IS NULL
                     AND total_cost_original_micros IS NULL AND total_cost_user_currency_micros IS NULL)
                    OR (status_code BETWEEN 200 AND 299 AND priced_flag = TRUE
                        AND input_cost_micros IS NOT NULL AND output_cost_micros IS NOT NULL
                        AND reasoning_cost_micros IS NOT NULL AND cache_read_input_cost_micros IS NOT NULL
                        AND cache_creation_input_cost_micros IS NOT NULL
                        AND total_cost_original_micros IS NOT NULL AND total_cost_user_currency_micros IS NOT NULL)
                    OR (status_code BETWEEN 200 AND 299 AND priced_flag = FALSE
                        AND unpriced_reason IN ('PRICING_DISABLED','MISSING_TOKEN_USAGE','STREAM_USAGE_UNAVAILABLE')
                        AND input_cost_micros IS NULL AND output_cost_micros IS NULL
                        AND reasoning_cost_micros IS NULL AND cache_read_input_cost_micros IS NULL
                        AND cache_creation_input_cost_micros IS NULL
                        AND total_cost_original_micros IS NULL AND total_cost_user_currency_micros IS NULL)
                )
                THEN 'trusted'
            ELSE 'legacy_untrusted'
        END
    WHERE pricing_status IS NULL;

    UPDATE public.usage_request_events
    SET pricing_status = CASE
            WHEN status_code < 100 OR status_code > 599 THEN NULL
            WHEN status_code NOT BETWEEN 200 AND 299 THEN 'ineligible'
            WHEN NOT public.prism_pricing_legacy_snapshots_coherent(
                pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output,
                pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input,
                pricing_snapshot_reasoning, currency_code_original, report_currency_code,
                report_currency_symbol, fx_rate_used, fx_rate_source, pricing_config_version_used)
                THEN 'unknown'
            WHEN priced_flag = TRUE
                AND input_cost_micros IS NOT NULL AND output_cost_micros IS NOT NULL
                AND reasoning_cost_micros IS NOT NULL AND cache_read_input_cost_micros IS NOT NULL
                AND cache_creation_input_cost_micros IS NOT NULL
                AND total_cost_original_micros IS NOT NULL AND total_cost_user_currency_micros IS NOT NULL
                THEN 'priced'
            WHEN priced_flag = FALSE
                AND unpriced_reason IN ('PRICING_DISABLED','MISSING_TOKEN_USAGE','STREAM_USAGE_UNAVAILABLE')
                AND input_cost_micros IS NULL AND output_cost_micros IS NULL
                AND reasoning_cost_micros IS NULL AND cache_read_input_cost_micros IS NULL
                AND cache_creation_input_cost_micros IS NULL
                AND total_cost_original_micros IS NULL AND total_cost_user_currency_micros IS NULL
                THEN 'unpriced'
            ELSE 'unknown'
        END,
        unpriced_reason = CASE
            WHEN status_code < 100 OR status_code > 599 THEN NULL
            WHEN status_code NOT BETWEEN 200 AND 299 THEN NULL
            WHEN NOT public.prism_pricing_legacy_snapshots_coherent(
                pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output,
                pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input,
                pricing_snapshot_reasoning, currency_code_original, report_currency_code,
                report_currency_symbol, fx_rate_used, fx_rate_source, pricing_config_version_used)
                THEN NULL
            WHEN priced_flag = FALSE
                AND unpriced_reason IN ('PRICING_DISABLED','MISSING_TOKEN_USAGE','STREAM_USAGE_UNAVAILABLE')
                AND input_cost_micros IS NULL AND output_cost_micros IS NULL
                AND reasoning_cost_micros IS NULL AND cache_read_input_cost_micros IS NULL
                AND cache_creation_input_cost_micros IS NULL
                AND total_cost_original_micros IS NULL AND total_cost_user_currency_micros IS NULL
                THEN unpriced_reason
            ELSE NULL
        END,
        pricing_evidence_trust = CASE
            WHEN status_code < 100 OR status_code > 599 THEN NULL
            WHEN public.prism_pricing_legacy_snapshots_coherent(
                pricing_snapshot_unit, pricing_snapshot_input, pricing_snapshot_output,
                pricing_snapshot_cache_read_input, pricing_snapshot_cache_creation_input,
                pricing_snapshot_reasoning, currency_code_original, report_currency_code,
                report_currency_symbol, fx_rate_used, fx_rate_source, pricing_config_version_used)
                AND (
                    (status_code NOT BETWEEN 200 AND 299
                     AND input_cost_micros IS NULL AND output_cost_micros IS NULL
                     AND reasoning_cost_micros IS NULL AND cache_read_input_cost_micros IS NULL
                     AND cache_creation_input_cost_micros IS NULL
                     AND total_cost_original_micros IS NULL AND total_cost_user_currency_micros IS NULL)
                    OR (status_code BETWEEN 200 AND 299 AND priced_flag = TRUE
                        AND input_cost_micros IS NOT NULL AND output_cost_micros IS NOT NULL
                        AND reasoning_cost_micros IS NOT NULL AND cache_read_input_cost_micros IS NOT NULL
                        AND cache_creation_input_cost_micros IS NOT NULL
                        AND total_cost_original_micros IS NOT NULL AND total_cost_user_currency_micros IS NOT NULL)
                    OR (status_code BETWEEN 200 AND 299 AND priced_flag = FALSE
                        AND unpriced_reason IN ('PRICING_DISABLED','MISSING_TOKEN_USAGE','STREAM_USAGE_UNAVAILABLE')
                        AND input_cost_micros IS NULL AND output_cost_micros IS NULL
                        AND reasoning_cost_micros IS NULL AND cache_read_input_cost_micros IS NULL
                        AND cache_creation_input_cost_micros IS NULL
                        AND total_cost_original_micros IS NULL AND total_cost_user_currency_micros IS NULL)
                )
                THEN 'trusted'
            ELSE 'legacy_untrusted'
        END
    WHERE pricing_status IS NULL;
END;
$$;

-- 11.5 telemetry quarantine: rows with invalid final HTTP status cannot enter
-- the four states and block finalization until authoritative evidence is
-- restored (SPEC 11.3 / 6.3.2).
DO $$
DECLARE
    row record;
    quarantine_identity_hash text;
    quarantine_payload_hash text;
    migration_time timestamptz;
BEGIN
    migration_time := clock_timestamp();
    FOR row IN
        SELECT profile_id, id, created_at, status_code
        FROM public.request_logs
        WHERE status_code < 100 OR status_code > 599
    LOOP
        quarantine_identity_hash := md5(concat_ws(E'\x1f', row.profile_id::text, 'request_log', row.id::text, row.created_at::text));
        quarantine_payload_hash := md5(concat_ws(E'\x1f', row.id::text, row.created_at::text, row.status_code::text));
        INSERT INTO public.pricing_telemetry_quarantine (
            profile_id, source_kind, issue_code, source_identity_snapshot,
            source_identity_hash, evidence_snapshot, payload_hash, detected_at
        ) VALUES (
            row.profile_id, 'request_log', 'invalid_final_http_status',
            jsonb_build_object('request_log_id', row.id, 'created_at', row.created_at),
            quarantine_identity_hash,
            jsonb_build_object('status_code', row.status_code),
            quarantine_payload_hash, migration_time
        ) ON CONFLICT (profile_id, source_kind, issue_code, source_identity_hash, payload_hash) DO NOTHING;
    END LOOP;
    FOR row IN
        SELECT profile_id, id, created_at, status_code
        FROM public.usage_request_events
        WHERE status_code < 100 OR status_code > 599
    LOOP
        quarantine_identity_hash := md5(concat_ws(E'\x1f', row.profile_id::text, 'usage_event', row.id::text, row.created_at::text));
        quarantine_payload_hash := md5(concat_ws(E'\x1f', row.id::text, row.created_at::text, row.status_code::text));
        INSERT INTO public.pricing_telemetry_quarantine (
            profile_id, source_kind, issue_code, source_identity_snapshot,
            source_identity_hash, evidence_snapshot, payload_hash, detected_at
        ) VALUES (
            row.profile_id, 'usage_event', 'invalid_final_http_status',
            jsonb_build_object('usage_event_id', row.id, 'created_at', row.created_at),
            quarantine_identity_hash,
            jsonb_build_object('status_code', row.status_code),
            quarantine_payload_hash, migration_time
        ) ON CONFLICT (profile_id, source_kind, issue_code, source_identity_hash, payload_hash) DO NOTHING;
    END LOOP;
END;
$$;

-- 11.6 inventory parents + archive ledgers + migration state resolution
-- (SPEC 11.5 / 6.3.1). One generation-1 inventory per profile collects the
-- immutable evidence staged above. Profiles with only unused FX rows and no
-- blocking template/currency issues receive an archive_unused_fx ledger and a
-- superseding clean generation-2 head. Profiles with any blocking issue stay
-- pending and block finalization (000004 rejects, rolling the upgrade back).
DO $$
DECLARE
    profile_row record;
    new_inventory_id bigint;
    next_inventory_id bigint;
    issue_codes text[];
    blocking_issue boolean;
    fx_count integer;
    fx_dependency_total bigint;
    template_count integer;
    reporting_count integer;
    fx_hash text;
    template_hash text;
    reporting_hash text;
    legacy_fx_source_count bigint;
    operation_id uuid;
    migration_time timestamptz;
BEGIN
    migration_time := clock_timestamp();
    FOR profile_row IN
        SELECT DISTINCT profile_id FROM public.user_settings
        UNION
        SELECT DISTINCT profile_id FROM public.pricing_templates
        UNION
        SELECT DISTINCT profile_id FROM public.endpoint_fx_rate_settings
        ORDER BY profile_id ASC
    LOOP
        -- Per-profile issue set from staged template evidence.
        issue_codes := ARRAY[]::text[];
        SELECT array_agg(DISTINCT code ORDER BY code) INTO issue_codes
        FROM (
            SELECT unnest(stage.issue_codes) AS code
            FROM public.prism_pricing_migration_template_stage AS stage
            WHERE stage.profile_id = profile_row.profile_id
        ) AS codes
        WHERE code IN ('unsupported_pricing_unit','invalid_price_encoding','foreign_currency_template');
        IF issue_codes IS NULL THEN
            issue_codes := ARRAY[]::text[];
        END IF;

        SELECT count(*) INTO reporting_count
        FROM public.prism_pricing_migration_reporting_stage
        WHERE profile_id = profile_row.profile_id;
        IF reporting_count > 0 THEN
            SELECT array_agg(code ORDER BY code) INTO issue_codes
            FROM (
                SELECT unnest(validation_codes) AS code
                FROM public.prism_pricing_migration_reporting_stage
                WHERE profile_id = profile_row.profile_id
            ) AS codes;
        END IF;

        SELECT count(*), COALESCE(md5(string_agg(row_hash, '' ORDER BY source_fx_row_id)), NULL)
        INTO fx_count, fx_hash
        FROM public.prism_pricing_migration_fx_stage
        WHERE profile_id = profile_row.profile_id;

        SELECT count(*), COALESCE(md5(string_agg(row_hash, '' ORDER BY template_id)), NULL)
        INTO template_count, template_hash
        FROM public.prism_pricing_migration_template_stage
        WHERE profile_id = profile_row.profile_id;

        SELECT count(*), COALESCE(md5(string_agg(row_hash, '' ORDER BY raw_report_currency_code)), NULL)
        INTO reporting_count, reporting_hash
        FROM public.prism_pricing_migration_reporting_stage
        WHERE profile_id = profile_row.profile_id;

        SELECT count(*) INTO legacy_fx_source_count
        FROM public.endpoint_fx_rate_settings
        WHERE profile_id = profile_row.profile_id;

        blocking_issue := EXISTS (
            SELECT 1 FROM unnest(issue_codes) AS code
            WHERE code IN ('foreign_currency_template','live_fx_dependency','unsupported_pricing_unit',
                           'invalid_price_encoding','invalid_reporting_currency_code',
                           'invalid_reporting_currency_symbol')
        );
        IF EXISTS (
            SELECT 1 FROM public.prism_pricing_migration_fx_stage
            WHERE profile_id = profile_row.profile_id AND assessment = 'has_live'
        ) AND NOT (issue_codes @> ARRAY['live_fx_dependency']::text[]) THEN
            issue_codes := array_append(issue_codes, 'live_fx_dependency');
            blocking_issue := TRUE;
        END IF;
        IF NOT blocking_issue AND fx_count > 0 THEN
            -- unused FX rows still need an archive ledger; the non-blocking
            -- marker documents them on the generation-1 inventory.
            issue_codes := array_append(issue_codes, 'unused_fx_evidence');
        END IF;

        -- Total live dependency rows across has_live evidence (0 otherwise).
        SELECT COALESCE(SUM(dependency_count), 0) INTO fx_dependency_total
        FROM public.prism_pricing_migration_fx_stage
        WHERE profile_id = profile_row.profile_id AND assessment = 'has_live';

        INSERT INTO public.pricing_migration_inventories (
            profile_id, generation, supersedes_inventory_id, settings_generation,
            epoch_generation, template_generation, reference_generation,
            issue_codes, fx_evidence_count, fx_assessment_count, fx_dependency_count,
            template_evidence_count, reporting_currency_evidence_count,
            fx_evidence_hash_root, template_evidence_hash_root,
            reporting_currency_evidence_hash_root, legacy_fx_source_count, created_at
        ) VALUES (
            profile_row.profile_id, 1, NULL, 1, 1, 1, 1,
            issue_codes, fx_count, fx_count, fx_dependency_total,
            template_count, reporting_count,
            fx_hash, template_hash, reporting_hash, legacy_fx_source_count,
            migration_time
        ) RETURNING inventory_id INTO new_inventory_id;

        INSERT INTO public.currency_migration_legacy_fx_evidence (
            inventory_id, source_fx_row_id, profile_id, model_id, endpoint_id,
            fx_rate, source_created_at, source_updated_at, row_hash, recorded_at
        )
        SELECT new_inventory_id, source_fx_row_id, profile_id, model_id, endpoint_id,
               fx_rate, source_created_at, source_updated_at, row_hash, migration_time
        FROM public.prism_pricing_migration_fx_stage
        WHERE profile_id = profile_row.profile_id;

        INSERT INTO public.currency_migration_legacy_fx_assessments (
            legacy_fx_evidence_id, attribution, scan_proof_code, scan_proof_hash, evaluated_at
        )
        SELECT evidence.legacy_fx_evidence_id, stage.assessment,
               CASE WHEN stage.assessment = 'has_live' THEN 'live_dependency_scan' ELSE 'zero_dependency_scan' END,
               md5(concat_ws(E'\x1f', stage.assessment, stage.dependency_count::text, stage.report_code)),
               migration_time
        FROM public.currency_migration_legacy_fx_evidence AS evidence
        JOIN public.prism_pricing_migration_fx_stage AS stage
          ON stage.source_fx_row_id = evidence.source_fx_row_id
         AND stage.profile_id = evidence.profile_id
        WHERE evidence.inventory_id = new_inventory_id;

        -- One dependency row per actual live dependency of a has_live evidence
        -- row (SPEC 6.3.1); has_live always blocks finalization, but the
        -- evidence stays complete and reconciles if the operator fixes the
        -- source data and re-runs the upgrade.
        INSERT INTO public.currency_migration_legacy_fx_dependencies (
            inventory_id, legacy_fx_evidence_id, connection_id, template_id,
            model_config_id, endpoint_id, source_template_currency,
            target_report_currency, proof_hash
        )
        SELECT new_inventory_id, evidence.legacy_fx_evidence_id,
               connections.id, connections.pricing_template_id,
               targets.source_model_config_id, connections.endpoint_id,
               prism_pricing_currency_code_canonical(templates.pricing_currency_code),
               NULLIF(stage.report_code, ''),
               md5(concat_ws(E'\x1f',
                   evidence.legacy_fx_evidence_id::text,
                   connections.id::text,
                   connections.pricing_template_id::text,
                   targets.source_model_config_id::text,
                   COALESCE(prism_pricing_currency_code_canonical(templates.pricing_currency_code), ''),
                   COALESCE(stage.report_code, '')))
        FROM public.currency_migration_legacy_fx_evidence AS evidence
        JOIN public.prism_pricing_migration_fx_stage AS stage
          ON stage.source_fx_row_id = evidence.source_fx_row_id
         AND stage.profile_id = evidence.profile_id
        JOIN public.model_access_targets AS targets ON targets.profile_id = evidence.profile_id
        JOIN public.model_configs AS source_models ON source_models.id = targets.source_model_config_id
        JOIN public.connections AS connections ON connections.id = targets.target_connection_id
        JOIN public.pricing_templates AS templates ON templates.id = connections.pricing_template_id
        WHERE evidence.inventory_id = new_inventory_id
          AND stage.assessment = 'has_live'
          AND source_models.model_id = stage.model_id
          AND connections.endpoint_id = stage.endpoint_id
          AND templates.deleted_at IS NULL
          AND templates.pricing_currency_code IS NOT NULL
          AND prism_pricing_currency_code_canonical(templates.pricing_currency_code) IS DISTINCT FROM stage.report_code;

        INSERT INTO public.pricing_migration_legacy_template_evidence (
            inventory_id, template_id, profile_id, public_version, pricing_unit,
            currency_code, input_price, output_price, cached_input_price,
            cache_creation_price, reasoning_price, issue_codes, recorded_at, row_hash
        )
        SELECT new_inventory_id, stage.template_id, stage.profile_id, stage.public_version, stage.pricing_unit,
               stage.currency_code, stage.input_price, stage.output_price, stage.cached_input_price,
               stage.cache_creation_price, stage.reasoning_price, stage.issue_codes, migration_time, stage.row_hash
        FROM public.prism_pricing_migration_template_stage AS stage
        WHERE stage.profile_id = profile_row.profile_id;

        INSERT INTO public.pricing_migration_legacy_reporting_currency_evidence (
            inventory_id, profile_id, raw_report_currency_code, raw_report_currency_symbol,
            settings_updated_at, validation_codes, recorded_at, row_hash
        )
        SELECT new_inventory_id, profile_id, raw_report_currency_code, raw_report_currency_symbol,
               settings_updated_at, validation_codes, migration_time, row_hash
        FROM public.prism_pricing_migration_reporting_stage
        WHERE profile_id = profile_row.profile_id;

        IF NOT blocking_issue THEN
            IF fx_count > 0 THEN
                -- archive_unused_fx ledger for the unused FX evidence set.
                operation_id := gen_random_uuid();
                INSERT INTO public.pricing_mutation_operation_reservations (
                    operation_id, profile_id, intended_result_kind, normalized_identity_hash, created_at
                ) VALUES (
                    operation_id, profile_row.profile_id, 'archive_unused_fx',
                    md5(concat_ws(E'\x1f', 'archive_unused_fx', new_inventory_id::text, fx_hash)), migration_time
                );
                INSERT INTO public.pricing_mutation_operations (
                    operation_id, profile_id, result_kind, normalized_payload_hash,
                    preview_hash, operation_recorded_at, success_summary, result_hash, created_at
                ) VALUES (
                    operation_id, profile_row.profile_id, 'archive_unused_fx',
                    md5(concat_ws(E'\x1f', 'archive_unused_fx', new_inventory_id::text, fx_hash)),
                    md5(concat_ws(E'\x1f', 'archive', new_inventory_id::text, fx_hash)),
                    migration_time,
                    jsonb_build_object(
                        'archived_fx_evidence_count', fx_count,
                        'template_revision_change_count', 0,
                        'epoch_change', FALSE
                    ),
                    md5(concat_ws(E'\x1f', 'result', fx_count::text)),
                    migration_time
                );
                INSERT INTO public.currency_migration_ledger (
                    operation_id, operation_kind, profile_id, old_epoch_id, old_epoch,
                    new_epoch_id, new_epoch, legacy_reporting_currency_evidence_id,
                    normalized_payload_hash, inventory_id, inventory_hash, item_count,
                    items_hash, committed_result, committed_at
                ) VALUES (
                    operation_id, 'archive_unused_fx', profile_row.profile_id,
                    (SELECT id FROM public.reporting_currency_epochs WHERE profile_id = profile_row.profile_id AND superseded_at IS NULL),
                    (SELECT epoch FROM public.reporting_currency_epochs WHERE profile_id = profile_row.profile_id AND superseded_at IS NULL),
                    NULL, NULL, NULL,
                    md5(concat_ws(E'\x1f', 'archive_unused_fx', new_inventory_id::text, fx_hash)),
                    new_inventory_id, fx_hash, 0, md5(''),
                    jsonb_build_object('archived_fx_evidence_count', fx_count),
                    migration_time
                );
                -- Superseding clean head proves the archived state: no
                -- blocking issues and no live dependencies remain. All child
                -- evidence rows belong to the archived generation-1
                -- inventory, so this head carries zero children.
                INSERT INTO public.pricing_migration_inventories (
                    profile_id, generation, supersedes_inventory_id, settings_generation,
                    epoch_generation, template_generation, reference_generation,
                    issue_codes, fx_evidence_count, fx_assessment_count, fx_dependency_count,
                    template_evidence_count, reporting_currency_evidence_count,
                    fx_evidence_hash_root, template_evidence_hash_root,
                    reporting_currency_evidence_hash_root, legacy_fx_source_count, created_at
                ) VALUES (
                    profile_row.profile_id, 2, new_inventory_id, 1, 1, 1, 1,
                    ARRAY[]::text[], 0, 0, 0,
                    0, 0,
                    NULL, NULL, NULL, 0,
                    migration_time
                ) RETURNING inventory_id INTO next_inventory_id;
            END IF;
            UPDATE public.user_settings
            SET pricing_migration_state = 'ready',
                legacy_migration_issues = '{}',
                updated_at = migration_time
            WHERE profile_id = profile_row.profile_id;
        ELSE
            UPDATE public.user_settings
            SET pricing_migration_state = 'legacy_pricing_migration_required',
                legacy_migration_issues = issue_codes,
                updated_at = migration_time
            WHERE profile_id = profile_row.profile_id;
        END IF;
    END LOOP;
END;
$$;
-- 11.7 finalization gates and phase transition (SPEC 11.1/6.3.1/6.3.2).
-- 11.7 finalization gates and phase transition (SPEC 11.1/6.3.1/6.3.2).
-- Upgrade paths reach `finalize_ready` only when no blocking inventory issue,
-- no unresolved telemetry quarantine and no unarchived legacy FX source
-- remains. When any gate fails the phase stays `legacy_writer_open` and
-- 000004 rejects, rolling the whole upgrade back so the operator can fix the
-- raw legacy data and re-run.
DO $$
DECLARE
    unresolved_inventory bigint;
    unresolved_quarantine bigint;
    unarchived_fx bigint;
    phase text;
    migration_time timestamptz;
BEGIN
    migration_time := clock_timestamp();
    SELECT transition.phase INTO phase FROM public.pricing_schema_transition_state AS transition WHERE transition.id = 1;
    IF phase = 'legacy_writer_open' THEN
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

        IF unresolved_inventory = 0 AND unresolved_quarantine = 0 AND unarchived_fx = 0 THEN
            UPDATE public.pricing_schema_transition_state
            SET phase = 'finalize_ready',
                lease_acquisition_open = FALSE,
                finalizer_fencing_token = finalizer_fencing_token + 1,
                updated_at = migration_time
            WHERE id = 1;
        END IF;
    END IF;
END;
$$;

-- ============================================================================
-- Part 12: hot partitioned pricing indexes (SPEC 6.5). The migration
-- transaction is fully isolated (no traffic, no concurrent writers), so
-- plain CREATE INDEX is safe here: ON ONLY parent indexes plus the matching
-- per-partition indexes. Future partitions attach the same shape through the
-- runtime partition ensurer.
-- ============================================================================

CREATE OR REPLACE FUNCTION public.prism_pricing_add_partition_indexes(parent_name text)
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    partition_name text;
BEGIN
    FOR partition_name IN
        SELECT child.relname
        FROM pg_inherits inheritance
        JOIN pg_class parent ON parent.oid = inheritance.inhparent
        JOIN pg_namespace parent_ns ON parent_ns.oid = parent.relnamespace
        JOIN pg_class child ON child.oid = inheritance.inhrelid
        JOIN pg_namespace child_ns ON child_ns.oid = child.relnamespace
        WHERE parent_ns.nspname = 'public' AND parent.relname = parent_name AND child_ns.nspname = 'public'
    LOOP
        EXECUTE format('CREATE INDEX IF NOT EXISTS %I ON public.%I USING btree (profile_id, pricing_status, created_at DESC)', partition_name || '_pricing_status_idx', partition_name);
        EXECUTE format('CREATE INDEX IF NOT EXISTS %I ON public.%I USING btree (profile_id, unpriced_reason, created_at DESC) WHERE pricing_status = ''unpriced''', partition_name || '_unpriced_reason_idx', partition_name);
        EXECUTE format('CREATE INDEX IF NOT EXISTS %I ON public.%I USING btree (profile_id, reporting_currency_epoch, created_at DESC)', partition_name || '_epoch_idx', partition_name);
    END LOOP;
END;
$$;

CREATE INDEX idx_request_logs_pricing_status ON ONLY public.request_logs USING btree (profile_id, pricing_status, created_at DESC);
CREATE INDEX idx_request_logs_unpriced_reason ON ONLY public.request_logs USING btree (profile_id, unpriced_reason, created_at DESC) WHERE (pricing_status = 'unpriced'::text);
CREATE INDEX idx_request_logs_reporting_currency_epoch ON ONLY public.request_logs USING btree (profile_id, reporting_currency_epoch, created_at DESC);
CREATE INDEX idx_usage_request_events_pricing_status ON ONLY public.usage_request_events USING btree (profile_id, pricing_status, created_at DESC);
CREATE INDEX idx_usage_request_events_unpriced_reason ON ONLY public.usage_request_events USING btree (profile_id, unpriced_reason, created_at DESC) WHERE (pricing_status = 'unpriced'::text);
CREATE INDEX idx_usage_request_events_reporting_currency_epoch ON ONLY public.usage_request_events USING btree (profile_id, reporting_currency_epoch, created_at DESC);

SELECT public.prism_pricing_add_partition_indexes('request_logs');
SELECT public.prism_pricing_add_partition_indexes('usage_request_events');

-- ============================================================================
-- Part 13: drop staging tables
-- ============================================================================

DROP TABLE public.prism_pricing_migration_template_stage;
DROP TABLE public.prism_pricing_migration_fx_stage;
DROP TABLE public.prism_pricing_migration_reporting_stage;
