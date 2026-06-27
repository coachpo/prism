DO $$
BEGIN
    IF to_regclass('public.connections') IS NOT NULL THEN
        ALTER TABLE public.connections
            DROP CONSTRAINT IF EXISTS ck_connections_context_window_tokens,
            DROP CONSTRAINT IF EXISTS ck_connections_default_output_token_reserve,
            DROP CONSTRAINT IF EXISTS ck_connections_max_context_utilization,
            DROP CONSTRAINT IF EXISTS ck_connections_preferred_context_utilization_threshold;

        ALTER TABLE public.connections
            DROP COLUMN IF EXISTS context_window_tokens,
            DROP COLUMN IF EXISTS context_window_tokens_overridden,
            DROP COLUMN IF EXISTS default_output_token_reserve,
            DROP COLUMN IF EXISTS default_output_token_reserve_overridden,
            DROP COLUMN IF EXISTS max_context_utilization,
            DROP COLUMN IF EXISTS max_context_utilization_overridden,
            DROP COLUMN IF EXISTS preferred_context_utilization_threshold,
            DROP COLUMN IF EXISTS preferred_context_utilization_threshold_overridden;
    END IF;

    IF to_regclass('public.model_configs') IS NOT NULL THEN
        ALTER TABLE public.model_configs
            DROP CONSTRAINT IF EXISTS ck_model_configs_facade_policy_contract;

        ALTER TABLE public.model_configs
            DROP COLUMN IF EXISTS facade_enabled,
            DROP COLUMN IF EXISTS facade_selection_policy,
            DROP COLUMN IF EXISTS facade_fallback_policy;
    END IF;
END $$;
