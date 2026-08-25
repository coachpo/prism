-- 000024_model_catalog_metadata
-- Additive, data-preserving catalog metadata. A Prism model gains at most one
-- models.dev binding row: immutable source snapshot columns refreshed only by
-- explicit operator refreshes, plus per-field manual override columns that
-- refreshes never touch. Pricing templates may carry the offering coordinates
-- of the models.dev offering they were imported from (one source-linked
-- template per offering), and revisions record whether they were authored
-- manually or imported from a catalog revision.

CREATE TABLE public.model_catalog_bindings (
    model_config_id integer NOT NULL,
    provider_id character varying(100) NOT NULL,
    catalog_model_id character varying(200) NOT NULL,
    match_source character varying(20) NOT NULL,
    catalog_revision character varying(128) NOT NULL,
    fetched_at timestamp with time zone NOT NULL,
    source_name text,
    source_description text,
    source_family text,
    source_release_date character varying(10),
    source_last_updated character varying(10),
    source_knowledge character varying(10),
    source_attachment boolean,
    source_reasoning boolean,
    source_tool_call boolean,
    source_structured_output boolean,
    source_temperature boolean,
    source_modalities_input jsonb,
    source_modalities_output jsonb,
    source_limit_context bigint,
    source_limit_input bigint,
    source_limit_output bigint,
    source_open_weights boolean,
    source_status character varying(20),
    override_name text,
    override_description text,
    override_family text,
    override_release_date character varying(10),
    override_last_updated character varying(10),
    override_knowledge character varying(10),
    override_attachment boolean,
    override_reasoning boolean,
    override_tool_call boolean,
    override_structured_output boolean,
    override_temperature boolean,
    override_modalities_input jsonb,
    override_modalities_output jsonb,
    override_limit_context bigint,
    override_limit_input bigint,
    override_limit_output bigint,
    override_open_weights boolean,
    override_status character varying(20),
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_model_catalog_bindings PRIMARY KEY (model_config_id),
    CONSTRAINT ck_mcb_match_source CHECK (match_source IN ('unique_match', 'manual')),
    CONSTRAINT ck_mcb_status CHECK (source_status IS NULL OR source_status IN ('alpha', 'beta', 'deprecated')),
    CONSTRAINT ck_mcb_override_status CHECK (override_status IS NULL OR override_status IN ('alpha', 'beta', 'deprecated')),
    CONSTRAINT ck_mcb_modalities_input_shape CHECK (
        source_modalities_input IS NULL OR jsonb_typeof(source_modalities_input) = 'array'
    ),
    CONSTRAINT ck_mcb_modalities_output_shape CHECK (
        source_modalities_output IS NULL OR jsonb_typeof(source_modalities_output) = 'array'
    ),
    CONSTRAINT ck_mcb_override_modalities_input_shape CHECK (
        override_modalities_input IS NULL OR jsonb_typeof(override_modalities_input) = 'array'
    ),
    CONSTRAINT ck_mcb_override_modalities_output_shape CHECK (
        override_modalities_output IS NULL OR jsonb_typeof(override_modalities_output) = 'array'
    ),
    CONSTRAINT ck_mcb_nonnegative_limits CHECK (
        COALESCE(source_limit_context, 0) >= 0
        AND COALESCE(source_limit_input, 0) >= 0
        AND COALESCE(source_limit_output, 0) >= 0
        AND COALESCE(override_limit_context, 0) >= 0
        AND COALESCE(override_limit_input, 0) >= 0
        AND COALESCE(override_limit_output, 0) >= 0
    ),
    CONSTRAINT model_catalog_bindings_model_fkey
        FOREIGN KEY (model_config_id) REFERENCES public.model_configs(id) ON DELETE CASCADE
);

-- Catalog metadata is management-only: it never participates in runtime
-- planning or routing, so it lives beside model_configs instead of inside it.

ALTER TABLE public.pricing_templates
    ADD COLUMN catalog_provider_id character varying(100),
    ADD COLUMN catalog_model_id character varying(200);

CREATE UNIQUE INDEX uq_pricing_templates_catalog_offering
    ON public.pricing_templates (catalog_provider_id, catalog_model_id)
    WHERE catalog_provider_id IS NOT NULL AND catalog_model_id IS NOT NULL;

ALTER TABLE public.pricing_template_revisions
    ADD COLUMN revision_source character varying(16) NOT NULL DEFAULT 'manual',
    ADD COLUMN catalog_revision character varying(128);

ALTER TABLE public.pricing_template_revisions
    ADD CONSTRAINT ck_ptr_revision_source CHECK (revision_source IN ('manual', 'catalog')),
    ADD CONSTRAINT ck_ptr_revision_source_evidence CHECK (
        (revision_source = 'catalog' AND catalog_revision IS NOT NULL)
        OR (revision_source = 'manual' AND catalog_revision IS NULL)
    );
