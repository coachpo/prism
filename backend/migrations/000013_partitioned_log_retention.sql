-- Clean-break log retention migration: log rows are intentionally not preserved.
-- PostgreSQL 16 partitioned roots are metadata-only: heap autovacuum reloptions
-- are rejected on parents, and parents expose no TOAST relation. Child partition
-- creation must apply the required heap and TOAST autovacuum reloptions to the
-- physical daily partitions.

DROP TABLE IF EXISTS public.audit_logs;
DROP TABLE IF EXISTS public.request_logs;
DROP TABLE IF EXISTS public.usage_request_events;
DROP TABLE IF EXISTS public.loadbalance_events;

CREATE TABLE public.audit_logs (
    id BIGSERIAL NOT NULL,
    profile_id integer NOT NULL,
    request_log_id bigint,
    request_log_created_at timestamp with time zone,
    ingress_request_id character varying(36),
    vendor_id integer,
    model_id character varying(200) NOT NULL,
    endpoint_id integer,
    connection_id integer,
    endpoint_base_url character varying(500),
    endpoint_description text,
    request_method character varying(10) NOT NULL,
    request_url character varying(2000) NOT NULL,
    request_headers text NOT NULL,
    request_body text,
    response_status integer NOT NULL,
    response_headers text,
    response_body text,
    is_stream boolean NOT NULL,
    duration_ms integer NOT NULL,
    created_at timestamp with time zone NOT NULL,
    request_body_stored boolean DEFAULT false NOT NULL,
    response_body_stored boolean DEFAULT false NOT NULL,
    audit_enabled_at_request boolean DEFAULT false NOT NULL,
    audit_capture_bodies_at_request boolean DEFAULT false NOT NULL,
    CONSTRAINT audit_logs_pkey PRIMARY KEY (created_at, id)
) PARTITION BY RANGE (created_at);

CREATE TABLE public.loadbalance_events (
    id BIGSERIAL NOT NULL,
    profile_id integer NOT NULL,
    connection_id integer NOT NULL,
    event_type character varying(20) NOT NULL,
    failure_kind character varying(20),
    consecutive_failures integer NOT NULL,
    cooldown_seconds numeric(10,2) NOT NULL,
    blocked_until_mono numeric(20,6),
    model_id character varying(200),
    endpoint_id integer,
    vendor_id integer,
    failure_threshold integer,
    backoff_multiplier numeric(5,2),
    max_cooldown_seconds integer,
    max_cooldown_strikes integer,
    ban_mode character varying(20),
    banned_until_at timestamp with time zone,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT chk_event_type CHECK (event_type IN ('opened', 'extended', 'max_cooldown_strike', 'banned', 'probe_eligible', 'recovered', 'not_opened')),
    CONSTRAINT chk_failure_kind CHECK (failure_kind IN ('transient_http', 'connect_error', 'timeout') OR failure_kind IS NULL),
    CONSTRAINT chk_loadbalance_events_ban_mode CHECK (ban_mode IN ('off', 'temporary', 'manual') OR ban_mode IS NULL),
    CONSTRAINT loadbalance_events_pkey PRIMARY KEY (created_at, id)
) PARTITION BY RANGE (created_at);

CREATE TABLE public.request_logs (
    id BIGSERIAL NOT NULL,
    profile_id integer NOT NULL,
    model_id character varying(200) NOT NULL,
    api_family character varying(50) NOT NULL,
    vendor_id integer,
    vendor_key character varying(100),
    vendor_name character varying(100),
    resolved_target_model_id character varying(200),
    endpoint_id integer,
    connection_id integer,
    proxy_api_key_id integer,
    proxy_api_key_name_snapshot character varying(200),
    ingress_request_id character varying(36),
    attempt_number integer,
    provider_correlation_id character varying(255),
    endpoint_base_url character varying(500),
    status_code integer NOT NULL,
    response_time_ms integer NOT NULL,
    is_stream boolean NOT NULL,
    input_tokens integer,
    output_tokens integer,
    total_tokens integer,
    success_flag boolean,
    billable_flag boolean,
    priced_flag boolean,
    unpriced_reason character varying(50),
    reasoning_tokens integer,
    input_cost_micros bigint,
    output_cost_micros bigint,
    reasoning_cost_micros bigint,
    total_cost_original_micros bigint,
    total_cost_user_currency_micros bigint,
    currency_code_original character varying(3),
    report_currency_code character varying(3),
    report_currency_symbol character varying(5),
    fx_rate_used character varying(20),
    fx_rate_source character varying(30),
    pricing_snapshot_unit character varying(10),
    pricing_snapshot_input character varying(20),
    pricing_snapshot_output character varying(20),
    pricing_snapshot_reasoning character varying(20),
    cache_read_input_tokens integer,
    cache_creation_input_tokens integer,
    cache_read_input_cost_micros bigint,
    cache_creation_input_cost_micros bigint,
    pricing_snapshot_cache_read_input character varying(20),
    pricing_snapshot_cache_creation_input character varying(20),
    pricing_config_version_used integer,
    request_path character varying(500) NOT NULL,
    error_detail text,
    endpoint_description text,
    created_at timestamp with time zone NOT NULL,
    caller_user_agent text,
    upstream_user_agent text,
    completion_duration_ms integer,
    ttft_ms integer,
    audit_enabled_at_request boolean DEFAULT false NOT NULL,
    audit_capture_bodies_at_request boolean DEFAULT false NOT NULL,
    request_generation_params jsonb,
    request_generation_params_status character varying(40),
    stream_outcome character varying(50) DEFAULT 'not_streaming' NOT NULL,
    stream_error_kind character varying(50),
    stream_error_detail text,
    CONSTRAINT request_logs_pkey PRIMARY KEY (created_at, id)
) PARTITION BY RANGE (created_at);

CREATE TABLE public.usage_request_events (
    id BIGSERIAL NOT NULL,
    profile_id integer NOT NULL,
    ingress_request_id character varying(36) NOT NULL,
    model_id character varying(200) NOT NULL,
    resolved_target_model_id character varying(200),
    api_family character varying(50) NOT NULL,
    endpoint_id integer,
    connection_id integer,
    proxy_api_key_id integer,
    proxy_api_key_name_snapshot character varying(200),
    status_code integer NOT NULL,
    success_flag boolean NOT NULL,
    input_tokens integer,
    output_tokens integer,
    total_tokens integer,
    cache_read_input_tokens integer,
    cache_creation_input_tokens integer,
    reasoning_tokens integer,
    input_cost_micros bigint,
    output_cost_micros bigint,
    cache_read_input_cost_micros bigint,
    cache_creation_input_cost_micros bigint,
    reasoning_cost_micros bigint,
    total_cost_original_micros bigint,
    total_cost_user_currency_micros bigint,
    currency_code_original character varying(3),
    report_currency_code character varying(3),
    report_currency_symbol character varying(5),
    fx_rate_used character varying(20),
    fx_rate_source character varying(30),
    pricing_snapshot_unit character varying(10),
    pricing_snapshot_input character varying(20),
    pricing_snapshot_output character varying(20),
    pricing_snapshot_cache_read_input character varying(20),
    pricing_snapshot_cache_creation_input character varying(20),
    pricing_snapshot_reasoning character varying(20),
    pricing_config_version_used integer,
    attempt_count integer NOT NULL,
    request_path character varying(500) NOT NULL,
    created_at timestamp with time zone NOT NULL,
    response_time_ms integer,
    completion_duration_ms integer,
    ttft_ms integer,
    billable_flag boolean,
    priced_flag boolean,
    unpriced_reason character varying(50),
    stream_outcome character varying(50) DEFAULT 'not_streaming' NOT NULL,
    stream_error_kind character varying(50),
    CONSTRAINT ck_usage_request_events_attempt_count_positive CHECK (attempt_count >= 1),
    CONSTRAINT usage_request_events_pkey PRIMARY KEY (created_at, id)
) PARTITION BY RANGE (created_at);

CREATE TABLE public.log_retention_settings (
    singleton_key character varying(20) NOT NULL,
    request_logs_retention_days integer,
    audit_logs_retention_days integer,
    statistics_retention_days integer,
    loadbalance_events_retention_days integer,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT log_retention_settings_pkey PRIMARY KEY (singleton_key),
    CONSTRAINT log_retention_settings_singleton_key_check CHECK (singleton_key = 'global'),
    CONSTRAINT log_retention_settings_request_logs_retention_days_check CHECK (request_logs_retention_days IS NULL OR request_logs_retention_days >= 1),
    CONSTRAINT log_retention_settings_audit_logs_retention_days_check CHECK (audit_logs_retention_days IS NULL OR audit_logs_retention_days >= 1),
    CONSTRAINT log_retention_settings_statistics_retention_days_check CHECK (statistics_retention_days IS NULL OR statistics_retention_days >= 1),
    CONSTRAINT log_retention_settings_loadbalance_events_retention_days_check CHECK (loadbalance_events_retention_days IS NULL OR loadbalance_events_retention_days >= 1)
);

INSERT INTO public.log_retention_settings (singleton_key)
VALUES ('global')
ON CONFLICT (singleton_key) DO NOTHING;

DO $$
BEGIN
    IF to_regclass('public.management_jobs') IS NOT NULL THEN
        ALTER TABLE public.management_jobs DROP CONSTRAINT IF EXISTS management_jobs_type_check;
        ALTER TABLE public.management_jobs
            ADD CONSTRAINT management_jobs_type_check CHECK (type IN ('audit_delete', 'log_retention'));
    END IF;
END $$;

CREATE INDEX ix_audit_logs_id ON public.audit_logs USING btree (id);
CREATE INDEX idx_audit_logs_connection_id ON public.audit_logs USING btree (connection_id);
CREATE INDEX idx_audit_logs_profile_created_at ON public.audit_logs USING btree (profile_id, created_at);
CREATE INDEX ix_audit_logs_connection_id ON public.audit_logs USING btree (connection_id);
CREATE INDEX ix_audit_logs_created_at ON public.audit_logs USING btree (created_at);
CREATE INDEX ix_audit_logs_endpoint_id ON public.audit_logs USING btree (endpoint_id);
CREATE INDEX ix_audit_logs_model_id ON public.audit_logs USING btree (model_id);
CREATE INDEX ix_audit_logs_profile_id ON public.audit_logs USING btree (profile_id);
CREATE INDEX ix_audit_logs_request_log_id ON public.audit_logs USING btree (request_log_id);
CREATE INDEX ix_audit_logs_response_status ON public.audit_logs USING btree (response_status);
CREATE INDEX ix_audit_logs_vendor_id ON public.audit_logs USING btree (vendor_id);
CREATE INDEX idx_audit_logs_profile_created_id_desc ON public.audit_logs USING btree (profile_id, created_at DESC, id DESC);
CREATE INDEX idx_audit_logs_profile_request_created_id_desc ON public.audit_logs USING btree (profile_id, request_log_id, created_at DESC, id DESC);
CREATE INDEX idx_audit_logs_profile_vendor_created_id_desc ON public.audit_logs USING btree (profile_id, vendor_id, created_at DESC, id DESC);
CREATE INDEX idx_audit_logs_profile_model_created_id_desc ON public.audit_logs USING btree (profile_id, model_id, created_at DESC, id DESC);
CREATE INDEX idx_audit_logs_profile_status_created_id_desc ON public.audit_logs USING btree (profile_id, response_status, created_at DESC, id DESC);
CREATE INDEX idx_audit_logs_profile_endpoint_created_id_desc ON public.audit_logs USING btree (profile_id, endpoint_id, created_at DESC, id DESC);
CREATE INDEX idx_audit_logs_profile_connection_created_id_desc ON public.audit_logs USING btree (profile_id, connection_id, created_at DESC, id DESC);

CREATE INDEX ix_loadbalance_events_id ON public.loadbalance_events USING btree (id);
CREATE INDEX idx_loadbalance_events_connection ON public.loadbalance_events USING btree (connection_id, created_at);
CREATE INDEX idx_loadbalance_events_event_type ON public.loadbalance_events USING btree (event_type);
CREATE INDEX idx_loadbalance_events_profile_created ON public.loadbalance_events USING btree (profile_id, created_at);
CREATE INDEX ix_loadbalance_events_created_at ON public.loadbalance_events USING btree (created_at);
CREATE INDEX ix_loadbalance_events_profile_id ON public.loadbalance_events USING btree (profile_id);

CREATE INDEX ix_request_logs_id ON public.request_logs USING btree (id);
CREATE INDEX idx_request_logs_billable_flag ON public.request_logs USING btree (billable_flag);
CREATE INDEX idx_request_logs_ingress_request_id ON public.request_logs USING btree (ingress_request_id);
CREATE INDEX idx_request_logs_priced_flag ON public.request_logs USING btree (priced_flag);
CREATE INDEX idx_request_logs_profile_created_at ON public.request_logs USING btree (profile_id, created_at);
CREATE INDEX ix_request_logs_api_family ON public.request_logs USING btree (api_family);
CREATE INDEX ix_request_logs_connection_id ON public.request_logs USING btree (connection_id);
CREATE INDEX ix_request_logs_created_at ON public.request_logs USING btree (created_at);
CREATE INDEX ix_request_logs_endpoint_id ON public.request_logs USING btree (endpoint_id);
CREATE INDEX ix_request_logs_model_id ON public.request_logs USING btree (model_id);
CREATE INDEX ix_request_logs_profile_id ON public.request_logs USING btree (profile_id);
CREATE INDEX ix_request_logs_proxy_api_key_id ON public.request_logs USING btree (proxy_api_key_id);
CREATE INDEX ix_request_logs_status_code ON public.request_logs USING btree (status_code);
CREATE INDEX ix_request_logs_vendor_id ON public.request_logs USING btree (vendor_id);

CREATE INDEX ix_usage_request_events_id ON public.usage_request_events USING btree (id);
CREATE INDEX idx_usage_request_events_ingress_request_id ON public.usage_request_events USING btree (ingress_request_id);
CREATE INDEX idx_usage_request_events_profile_created_at ON public.usage_request_events USING btree (profile_id, created_at);
CREATE INDEX idx_usage_request_events_profile_ingress_request ON public.usage_request_events USING btree (profile_id, ingress_request_id);
CREATE INDEX ix_usage_request_events_api_family ON public.usage_request_events USING btree (api_family);
CREATE INDEX ix_usage_request_events_connection_id ON public.usage_request_events USING btree (connection_id);
CREATE INDEX ix_usage_request_events_created_at ON public.usage_request_events USING btree (created_at);
CREATE INDEX ix_usage_request_events_endpoint_id ON public.usage_request_events USING btree (endpoint_id);
CREATE INDEX ix_usage_request_events_model_id ON public.usage_request_events USING btree (model_id);
CREATE INDEX ix_usage_request_events_profile_id ON public.usage_request_events USING btree (profile_id);
CREATE INDEX ix_usage_request_events_proxy_api_key_id ON public.usage_request_events USING btree (proxy_api_key_id);

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'profiles') THEN
        ALTER TABLE public.audit_logs
            ADD CONSTRAINT audit_logs_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE RESTRICT;
        ALTER TABLE public.loadbalance_events
            ADD CONSTRAINT loadbalance_events_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE RESTRICT;
        ALTER TABLE public.request_logs
            ADD CONSTRAINT request_logs_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE RESTRICT;
        ALTER TABLE public.usage_request_events
            ADD CONSTRAINT usage_request_events_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE RESTRICT;
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'proxy_api_keys') THEN
        ALTER TABLE public.request_logs
            ADD CONSTRAINT request_logs_proxy_api_key_id_fkey FOREIGN KEY (proxy_api_key_id) REFERENCES public.proxy_api_keys(id) ON DELETE SET NULL;
        ALTER TABLE public.usage_request_events
            ADD CONSTRAINT usage_request_events_proxy_api_key_id_fkey FOREIGN KEY (proxy_api_key_id) REFERENCES public.proxy_api_keys(id) ON DELETE SET NULL;
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'vendors') THEN
        ALTER TABLE public.audit_logs
            ADD CONSTRAINT fk_audit_logs_vendor_id_set_null FOREIGN KEY (vendor_id) REFERENCES public.vendors(id) ON DELETE SET NULL;
        ALTER TABLE public.loadbalance_events
            ADD CONSTRAINT fk_loadbalance_events_vendor_id_set_null FOREIGN KEY (vendor_id) REFERENCES public.vendors(id) ON DELETE SET NULL;
    END IF;
END $$;
