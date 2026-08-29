-- 000027_model_pi_catalog_bindings
-- Additive, data-preserving Pi catalog binding. A Prism model gains at most
-- one pi.dev binding row: the frozen selected coordinate (provider_id,
-- catalog_model_id, api) plus the trusted catalog_revision it was bound or
-- last refreshed against, immutable source snapshot columns for the seven
-- safe pi.dev leaves (name, reasoning, input, context_window, max_tokens,
-- thinking_level_map, compat), and per-field manual override columns that
-- refreshes never touch. This table is independent of model_catalog_bindings
-- (models.dev): it never reuses, rewrites, or is read by that table's rows,
-- and it never enters runtime planning or routing.

CREATE TABLE public.model_pi_catalog_bindings (
    model_config_id integer NOT NULL,
    provider_id character varying(200) NOT NULL,
    catalog_model_id character varying(200) NOT NULL,
    api character varying(40) NOT NULL,
    bind_source character varying(20) NOT NULL,
    catalog_revision character varying(128) NOT NULL,
    fetched_at timestamp with time zone NOT NULL,
    source_name text,
    source_reasoning boolean,
    source_input jsonb,
    source_context_window bigint,
    source_max_tokens bigint,
    source_thinking_level_map jsonb,
    source_compat jsonb,
    source_dropped_fields jsonb NOT NULL DEFAULT '[]'::jsonb,
    override_name text,
    override_reasoning boolean,
    override_input jsonb,
    override_context_window bigint,
    override_max_tokens bigint,
    override_thinking_level_map jsonb,
    override_compat jsonb,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT pk_model_pi_catalog_bindings PRIMARY KEY (model_config_id),
    CONSTRAINT ck_mpcb_api CHECK (api IN ('openai-responses', 'openai-completions', 'anthropic-messages', 'google-generative-ai')),
    CONSTRAINT ck_mpcb_bind_source CHECK (bind_source IN ('single_candidate', 'manual')),
    CONSTRAINT ck_mpcb_input_shape CHECK (
        source_input IS NULL OR jsonb_typeof(source_input) = 'array'
    ),
    CONSTRAINT ck_mpcb_override_input_shape CHECK (
        override_input IS NULL OR jsonb_typeof(override_input) = 'array'
    ),
    CONSTRAINT ck_mpcb_thinking_level_map_shape CHECK (
        source_thinking_level_map IS NULL OR jsonb_typeof(source_thinking_level_map) = 'object'
    ),
    CONSTRAINT ck_mpcb_override_thinking_level_map_shape CHECK (
        override_thinking_level_map IS NULL OR jsonb_typeof(override_thinking_level_map) = 'object'
    ),
    CONSTRAINT ck_mpcb_compat_shape CHECK (
        source_compat IS NULL OR jsonb_typeof(source_compat) = 'object'
    ),
    CONSTRAINT ck_mpcb_dropped_fields_shape CHECK (
        jsonb_typeof(source_dropped_fields) = 'array'
    ),
    CONSTRAINT ck_mpcb_override_compat_shape CHECK (
        override_compat IS NULL OR jsonb_typeof(override_compat) = 'object'
    ),
    CONSTRAINT ck_mpcb_positive_limits CHECK (
        (source_context_window IS NULL OR source_context_window > 0)
        AND (source_max_tokens IS NULL OR source_max_tokens > 0)
        AND (override_context_window IS NULL OR override_context_window > 0)
        AND (override_max_tokens IS NULL OR override_max_tokens > 0)
    ),
    CONSTRAINT model_pi_catalog_bindings_model_fkey
        FOREIGN KEY (model_config_id) REFERENCES public.model_configs(id) ON DELETE CASCADE
);
