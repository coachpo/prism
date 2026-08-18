-- 000022_pricing_input_tier
-- Additive single-threshold, two-card pricing. A revision with all six tier
-- columns NULL has no tier; every other revision carries the threshold and a
-- complete five-component tier card whose optional specialty fields mirror
-- the base card's NULL/configured shape.

ALTER TABLE public.pricing_template_revisions
    ADD COLUMN tier_input_tokens_above integer,
    ADD COLUMN tier_input_price character varying(20),
    ADD COLUMN tier_output_price character varying(20),
    ADD COLUMN tier_cached_input_price character varying(20),
    ADD COLUMN tier_cache_creation_price character varying(20),
    ADD COLUMN tier_reasoning_price character varying(20),
    ADD CONSTRAINT ck_ptr_tier_input_price
        CHECK (tier_input_price IS NULL OR prism_pricing_exact_decimal_canonical(tier_input_price) = tier_input_price),
    ADD CONSTRAINT ck_ptr_tier_output_price
        CHECK (tier_output_price IS NULL OR prism_pricing_exact_decimal_canonical(tier_output_price) = tier_output_price),
    ADD CONSTRAINT ck_ptr_tier_cached_input_price
        CHECK (tier_cached_input_price IS NULL OR prism_pricing_exact_decimal_canonical(tier_cached_input_price) = tier_cached_input_price),
    ADD CONSTRAINT ck_ptr_tier_cache_creation_price
        CHECK (tier_cache_creation_price IS NULL OR prism_pricing_exact_decimal_canonical(tier_cache_creation_price) = tier_cache_creation_price),
    ADD CONSTRAINT ck_ptr_tier_reasoning_price
        CHECK (tier_reasoning_price IS NULL OR prism_pricing_exact_decimal_canonical(tier_reasoning_price) = tier_reasoning_price),
    ADD CONSTRAINT ck_ptr_tier_threshold_positive
        CHECK (tier_input_tokens_above IS NULL OR tier_input_tokens_above >= 1),
    ADD CONSTRAINT ck_ptr_tier_all_or_none
        CHECK (
            (tier_input_tokens_above IS NULL
                AND tier_input_price IS NULL
                AND tier_output_price IS NULL
                AND tier_cached_input_price IS NULL
                AND tier_cache_creation_price IS NULL
                AND tier_reasoning_price IS NULL)
            OR (tier_input_tokens_above IS NOT NULL
                AND tier_input_price IS NOT NULL
                AND tier_output_price IS NOT NULL)
        ),
    ADD CONSTRAINT ck_ptr_tier_specialty_parity
        CHECK (
            tier_input_tokens_above IS NULL
            OR ((cached_input_price IS NULL) = (tier_cached_input_price IS NULL)
                AND (cache_creation_price IS NULL) = (tier_cache_creation_price IS NULL)
                AND (reasoning_price IS NULL) = (tier_reasoning_price IS NULL))
        );

ALTER TABLE public.request_logs
    ADD COLUMN pricing_tier_applied character varying(20),
    ADD COLUMN pricing_tier_threshold_tokens integer,
    ADD COLUMN pricing_tier_basis_tokens bigint,
    ADD CONSTRAINT pricing_tier_applied_check CHECK (
        pricing_tier_applied IS NULL
        OR pricing_tier_applied IN ('not_evaluated', 'not_applicable', 'base', 'tier')
    ) NOT VALID,
    ADD CONSTRAINT pricing_tier_evidence_check CHECK (
        (pricing_tier_applied IS NULL AND pricing_tier_threshold_tokens IS NULL AND pricing_tier_basis_tokens IS NULL)
        OR (pricing_tier_applied IN ('base', 'tier')
            AND pricing_tier_threshold_tokens IS NOT NULL
            AND pricing_tier_basis_tokens IS NOT NULL)
        OR (pricing_tier_applied IN ('not_evaluated', 'not_applicable')
            AND pricing_tier_threshold_tokens IS NULL
            AND pricing_tier_basis_tokens IS NULL)
    ) NOT VALID;

ALTER TABLE public.usage_request_events
    ADD COLUMN pricing_tier_applied character varying(20),
    ADD COLUMN pricing_tier_threshold_tokens integer,
    ADD COLUMN pricing_tier_basis_tokens bigint,
    ADD CONSTRAINT pricing_tier_applied_check CHECK (
        pricing_tier_applied IS NULL
        OR pricing_tier_applied IN ('not_evaluated', 'not_applicable', 'base', 'tier')
    ) NOT VALID,
    ADD CONSTRAINT pricing_tier_evidence_check CHECK (
        (pricing_tier_applied IS NULL AND pricing_tier_threshold_tokens IS NULL AND pricing_tier_basis_tokens IS NULL)
        OR (pricing_tier_applied IN ('base', 'tier')
            AND pricing_tier_threshold_tokens IS NOT NULL
            AND pricing_tier_basis_tokens IS NOT NULL)
        OR (pricing_tier_applied IN ('not_evaluated', 'not_applicable')
            AND pricing_tier_threshold_tokens IS NULL
            AND pricing_tier_basis_tokens IS NULL)
    ) NOT VALID;
