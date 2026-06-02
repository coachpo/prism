-- Prism fresh-install baseline schema.
-- Historical upgrade migrations are intentionally squashed for the pre-user product.

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: app_auth_settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.app_auth_settings (
    id integer NOT NULL,
    singleton_key character varying(20) NOT NULL,
    auth_enabled boolean NOT NULL,
    username character varying(200),
    email character varying(320),
    pending_email character varying(320),
    password_hash text,
    email_bound_at timestamp with time zone,
    email_verification_code_hash character varying(64),
    email_verification_expires_at timestamp with time zone,
    email_verification_attempt_count integer NOT NULL,
    must_change_password boolean NOT NULL,
    last_login_at timestamp with time zone,
    token_version integer NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL
);


--
-- Name: app_auth_settings_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.app_auth_settings_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: app_auth_settings_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.app_auth_settings_id_seq OWNED BY public.app_auth_settings.id;


--
-- Name: audit_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.audit_logs (
    id bigint NOT NULL,
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
    audit_capture_bodies_at_request boolean DEFAULT false NOT NULL
)
PARTITION BY RANGE (created_at);


--
-- Name: audit_logs_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.audit_logs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: audit_logs_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.audit_logs_id_seq OWNED BY public.audit_logs.id;


--
-- Name: connections; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.connections (
    id integer NOT NULL,
    profile_id integer NOT NULL,
    api_family character varying(50) NOT NULL,
    endpoint_id integer NOT NULL,
    context_window_tokens integer,
    context_window_tokens_overridden boolean DEFAULT false NOT NULL,
    default_output_token_reserve integer DEFAULT 4096 NOT NULL,
    default_output_token_reserve_overridden boolean DEFAULT false NOT NULL,
    max_context_utilization double precision DEFAULT 0.90 NOT NULL,
    max_context_utilization_overridden boolean DEFAULT false NOT NULL,
    preferred_context_utilization_threshold double precision,
    preferred_context_utilization_threshold_overridden boolean DEFAULT false NOT NULL,
    pricing_template_id integer,
    qps_limit integer,
    max_in_flight_non_stream integer,
    max_in_flight_stream integer,
    is_active boolean NOT NULL,
    priority integer NOT NULL,
    name text,
    auth_type character varying(50),
    custom_headers text,
    health_status character varying(20) NOT NULL,
    health_detail text,
    last_health_check timestamp with time zone,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    openai_probe_endpoint_variant character varying(40),
    monitoring_probe_interval_seconds integer DEFAULT 300 NOT NULL,
    CONSTRAINT ck_connections_context_window_tokens CHECK (((context_window_tokens IS NULL) OR (context_window_tokens >= 1))),
    CONSTRAINT ck_connections_default_output_token_reserve CHECK ((default_output_token_reserve >= 1)),
    CONSTRAINT ck_connections_max_context_utilization CHECK (((max_context_utilization > (0)::double precision) AND (max_context_utilization <= (1)::double precision))),
    CONSTRAINT ck_connections_preferred_context_utilization_threshold CHECK (((preferred_context_utilization_threshold IS NULL) OR (((preferred_context_utilization_threshold > (0)::double precision) AND (preferred_context_utilization_threshold <= (1)::double precision)) AND (preferred_context_utilization_threshold <= max_context_utilization)))),
    CONSTRAINT ck_connections_openai_probe_endpoint_variant CHECK (((openai_probe_endpoint_variant IS NULL) OR ((openai_probe_endpoint_variant)::text = ANY ((ARRAY['responses_minimal'::character varying, 'responses_reasoning_none'::character varying, 'chat_completions_minimal'::character varying, 'chat_completions_reasoning_none'::character varying])::text[]))))
);


--
-- Name: connections_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.connections_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: connections_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.connections_id_seq OWNED BY public.connections.id;


--
-- Name: email_outbox; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.email_outbox (
    id uuid NOT NULL,
    kind text NOT NULL,
    recipient_email text NOT NULL,
    template text NOT NULL,
    payload_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    email_secret_ciphertext text,
    idempotency_key text NOT NULL,
    status text DEFAULT 'queued'::text NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    max_attempts integer DEFAULT 8 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    locked_by text,
    locked_until timestamp with time zone,
    sent_at timestamp with time zone,
    dead_lettered_at timestamp with time zone,
    last_error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT email_outbox_attempts_check CHECK (((attempt_count >= 0) AND (max_attempts > 0) AND (attempt_count <= max_attempts))),
    CONSTRAINT email_outbox_status_check CHECK ((status = ANY (ARRAY['queued'::text, 'sending'::text, 'sent'::text, 'dead'::text])))
);


--
-- Name: endpoint_fx_rate_settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.endpoint_fx_rate_settings (
    id integer NOT NULL,
    profile_id integer NOT NULL,
    model_id character varying(200) NOT NULL,
    endpoint_id integer NOT NULL,
    fx_rate character varying(20) NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL
);


--
-- Name: endpoint_fx_rate_settings_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.endpoint_fx_rate_settings_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: endpoint_fx_rate_settings_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.endpoint_fx_rate_settings_id_seq OWNED BY public.endpoint_fx_rate_settings.id;


--
-- Name: endpoints; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.endpoints (
    id integer NOT NULL,
    profile_id integer NOT NULL,
    name character varying(200) NOT NULL,
    base_url character varying(500) NOT NULL,
    api_key character varying(500) NOT NULL,
    "position" integer NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL
);


--
-- Name: endpoints_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.endpoints_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: endpoints_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.endpoints_id_seq OWNED BY public.endpoints.id;


--
-- Name: header_blocklist_rules; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.header_blocklist_rules (
    id integer NOT NULL,
    profile_id integer,
    name character varying(200) NOT NULL,
    match_type character varying(20) NOT NULL,
    pattern character varying(200) NOT NULL,
    enabled boolean NOT NULL,
    is_system boolean NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT ck_hbr_profile_scope CHECK ((((is_system = true) AND (profile_id IS NULL)) OR ((is_system = false) AND (profile_id IS NOT NULL))))
);


--
-- Name: header_blocklist_rules_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.header_blocklist_rules_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: header_blocklist_rules_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.header_blocklist_rules_id_seq OWNED BY public.header_blocklist_rules.id;


--
-- Name: loadbalance_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.loadbalance_events (
    id bigint NOT NULL,
    profile_id integer NOT NULL,
    connection_id integer NOT NULL,
    event_type character varying(32) NOT NULL,
    failure_kind character varying(20),
    cycle_retry_attempts integer NOT NULL,
    cumulative_retry_attempts integer NOT NULL,
    next_retry_at timestamp with time zone,
    last_retry_delay_ms integer NOT NULL,
    model_id character varying(200),
    endpoint_id integer,
    vendor_id integer,
    ban_mode character varying(20),
    policy_cycle_retry_attempt_limit integer,
    policy_ban_cumulative_retry_attempt_threshold integer,
    banned_until_at timestamp with time zone,
    last_success_at timestamp with time zone,
    created_at timestamp with time zone NOT NULL,
    CONSTRAINT chk_event_type CHECK (((event_type)::text = ANY ((ARRAY['retry_scheduled'::character varying, 'retry_exhausted'::character varying, 'banned'::character varying, 'unbanned'::character varying, 'recovered'::character varying, 'admission_rejected'::character varying])::text[]))),
    CONSTRAINT chk_failure_kind CHECK ((((failure_kind)::text = ANY ((ARRAY['transient_http'::character varying, 'connect_error'::character varying, 'timeout'::character varying])::text[])) OR (failure_kind IS NULL))),
    CONSTRAINT chk_loadbalance_events_ban_mode CHECK ((((ban_mode)::text = ANY ((ARRAY['off'::character varying, 'temporary'::character varying, 'until_reset'::character varying])::text[])) OR (ban_mode IS NULL))),
    CONSTRAINT chk_loadbalance_events_cycle_attempts_nonneg CHECK ((cycle_retry_attempts >= 0)),
    CONSTRAINT chk_loadbalance_events_cumulative_attempts_nonneg CHECK ((cumulative_retry_attempts >= 0)),
    CONSTRAINT chk_loadbalance_events_policy_ban_threshold CHECK (((policy_ban_cumulative_retry_attempt_threshold IS NULL) OR ((policy_ban_cumulative_retry_attempt_threshold >= 0) AND (policy_ban_cumulative_retry_attempt_threshold <= 500)))),
    CONSTRAINT chk_loadbalance_events_policy_ban_threshold_gte_cycle_limit CHECK (((policy_cycle_retry_attempt_limit IS NULL) OR (policy_ban_cumulative_retry_attempt_threshold IS NULL) OR (policy_ban_cumulative_retry_attempt_threshold = 0) OR (policy_ban_cumulative_retry_attempt_threshold >= policy_cycle_retry_attempt_limit))),
    CONSTRAINT chk_loadbalance_events_policy_cycle_retry_attempt_limit CHECK (((policy_cycle_retry_attempt_limit IS NULL) OR ((policy_cycle_retry_attempt_limit >= 1) AND (policy_cycle_retry_attempt_limit <= 50)))),
    CONSTRAINT chk_loadbalance_events_retry_delay_nonneg CHECK ((last_retry_delay_ms >= 0))
)
PARTITION BY RANGE (created_at);


--
-- Name: loadbalance_events_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.loadbalance_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: loadbalance_events_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.loadbalance_events_id_seq OWNED BY public.loadbalance_events.id;


--
-- Name: loadbalance_round_robin_state; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.loadbalance_round_robin_state (
    id integer NOT NULL,
    profile_id integer NOT NULL,
    model_config_id integer NOT NULL,
    next_cursor integer NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    CONSTRAINT ck_loadbalance_round_robin_state_next_cursor_nonnegative CHECK ((next_cursor >= 0))
);


--
-- Name: loadbalance_round_robin_state_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.loadbalance_round_robin_state_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: loadbalance_round_robin_state_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.loadbalance_round_robin_state_id_seq OWNED BY public.loadbalance_round_robin_state.id;


--
-- Name: loadbalance_strategies; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.loadbalance_strategies (
    id integer NOT NULL,
    profile_id integer NOT NULL,
    name character varying(200) NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    legacy_strategy_type character varying(32) NOT NULL,
    failure_status_codes integer[] DEFAULT ARRAY[403, 422, 429, 500, 502, 503, 504, 529]::integer[] NOT NULL,
    ban_mode character varying(20) DEFAULT 'off'::character varying NOT NULL,
    retry_base_delay_ms integer DEFAULT 60000 NOT NULL,
    retry_backoff_multiplier double precision DEFAULT 2.0 NOT NULL,
    retry_jitter_ratio double precision DEFAULT 0.2 NOT NULL,
    retry_max_delay_ms integer DEFAULT 900000 NOT NULL,
    cycle_retry_attempt_limit integer DEFAULT 3 NOT NULL,
    ban_cumulative_retry_attempt_threshold integer DEFAULT 0 NOT NULL,
    ban_duration_seconds integer DEFAULT 0 NOT NULL,
    CONSTRAINT chk_loadbalance_strategies_ban_cumulative_retry_attempt_threshold CHECK (((((ban_mode)::text = 'off'::text) AND (ban_cumulative_retry_attempt_threshold = 0)) OR (((ban_mode)::text <> 'off'::text) AND (ban_cumulative_retry_attempt_threshold >= 1) AND (ban_cumulative_retry_attempt_threshold <= 500)))),
    CONSTRAINT chk_loadbalance_strategies_ban_duration CHECK (((((ban_mode)::text = 'temporary'::text) AND (ban_duration_seconds >= 1) AND (ban_duration_seconds <= 86400)) OR (((ban_mode)::text = ANY ((ARRAY['off'::character varying, 'until_reset'::character varying])::text[])) AND (ban_duration_seconds = 0)))),
    CONSTRAINT chk_loadbalance_strategies_ban_mode CHECK (((ban_mode)::text = ANY ((ARRAY['off'::character varying, 'temporary'::character varying, 'until_reset'::character varying])::text[]))),
    CONSTRAINT chk_loadbalance_strategies_ban_threshold_gte_cycle_limit CHECK ((((ban_mode)::text = 'off'::text) OR (ban_cumulative_retry_attempt_threshold >= cycle_retry_attempt_limit))),
    CONSTRAINT chk_loadbalance_strategies_cycle_retry_attempt_limit CHECK (((cycle_retry_attempt_limit >= 1) AND (cycle_retry_attempt_limit <= 50))),
    CONSTRAINT chk_loadbalance_strategies_legacy_strategy_type CHECK (((legacy_strategy_type)::text = ANY ((ARRAY['single'::character varying, 'fill-first'::character varying, 'round-robin'::character varying, 'cheapest_eligible_context'::character varying])::text[]))),
    CONSTRAINT chk_loadbalance_strategies_retry_backoff_multiplier CHECK (((retry_backoff_multiplier >= (1.0)::double precision) AND (retry_backoff_multiplier <= (10.0)::double precision))),
    CONSTRAINT chk_loadbalance_strategies_retry_base_delay_ms CHECK (((retry_base_delay_ms >= 0) AND (retry_base_delay_ms <= 86400000))),
    CONSTRAINT chk_loadbalance_strategies_retry_jitter_ratio CHECK (((retry_jitter_ratio >= (0.0)::double precision) AND (retry_jitter_ratio <= (1.0)::double precision))),
    CONSTRAINT chk_loadbalance_strategies_retry_max_delay_ms CHECK (((retry_max_delay_ms >= 1) AND (retry_max_delay_ms <= 86400000)))
);


--
-- Name: loadbalance_strategies_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.loadbalance_strategies_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: loadbalance_strategies_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.loadbalance_strategies_id_seq OWNED BY public.loadbalance_strategies.id;


--
-- Name: log_retention_settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.log_retention_settings (
    singleton_key character varying(20) NOT NULL,
    request_logs_retention_days integer,
    audit_logs_retention_days integer,
    statistics_retention_days integer,
    loadbalance_events_retention_days integer,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT log_retention_settings_audit_logs_retention_days_check CHECK (((audit_logs_retention_days IS NULL) OR (audit_logs_retention_days >= 1))),
    CONSTRAINT log_retention_settings_loadbalance_events_retention_days_check CHECK (((loadbalance_events_retention_days IS NULL) OR (loadbalance_events_retention_days >= 1))),
    CONSTRAINT log_retention_settings_request_logs_retention_days_check CHECK (((request_logs_retention_days IS NULL) OR (request_logs_retention_days >= 1))),
    CONSTRAINT log_retention_settings_singleton_key_check CHECK (((singleton_key)::text = 'global'::text)),
    CONSTRAINT log_retention_settings_statistics_retention_days_check CHECK (((statistics_retention_days IS NULL) OR (statistics_retention_days >= 1)))
);


--
-- Name: management_job_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.management_job_events (
    id bigint NOT NULL,
    job_id text NOT NULL,
    event_type text NOT NULL,
    message text DEFAULT ''::text NOT NULL,
    rows_deleted bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: management_job_events_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.management_job_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: management_job_events_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.management_job_events_id_seq OWNED BY public.management_job_events.id;


--
-- Name: management_jobs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.management_jobs (
    id text NOT NULL,
    type text NOT NULL,
    state text NOT NULL,
    requested_by text NOT NULL,
    requested_at timestamp with time zone NOT NULL,
    started_at timestamp with time zone,
    finished_at timestamp with time zone,
    priority text DEFAULT 'maintenance'::text NOT NULL,
    idempotency_key text,
    profile_id integer NOT NULL,
    scope_json jsonb NOT NULL,
    reason text NOT NULL,
    rows_matched_estimate bigint,
    rows_deleted bigint DEFAULT 0 NOT NULL,
    batches_completed bigint DEFAULT 0 NOT NULL,
    progress_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    cancel_requested boolean DEFAULT false NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    max_attempts integer DEFAULT 8 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    locked_by text,
    locked_until timestamp with time zone,
    last_heartbeat_at timestamp with time zone,
    error_code text,
    error_message text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT management_jobs_attempts_check CHECK (((attempt_count >= 0) AND (max_attempts > 0) AND (attempt_count <= max_attempts))),
    CONSTRAINT management_jobs_state_check CHECK ((state = ANY (ARRAY['queued'::text, 'running'::text, 'cancel_requested'::text, 'cancelled'::text, 'succeeded'::text, 'failed'::text]))),
    CONSTRAINT management_jobs_type_check CHECK ((type = ANY (ARRAY['audit_delete'::text, 'log_retention'::text])))
);


--
-- Name: management_outbox; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.management_outbox (
    id bigint NOT NULL,
    operation_id text NOT NULL,
    event_type text NOT NULL,
    aggregate_type text NOT NULL,
    aggregate_id text NOT NULL,
    aggregate_version bigint,
    dedupe_key text NOT NULL,
    payload jsonb NOT NULL,
    status text NOT NULL,
    attempt_count integer DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT now() NOT NULL,
    locked_by text,
    locked_at timestamp with time zone,
    last_error text,
    actor_id text,
    trace_id text,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    processed_at timestamp with time zone,
    CONSTRAINT management_outbox_status_check CHECK ((status = ANY (ARRAY['pending'::text, 'processing'::text, 'retry'::text, 'succeeded'::text, 'failed_permanent'::text])))
);


--
-- Name: management_outbox_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.management_outbox_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: management_outbox_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.management_outbox_id_seq OWNED BY public.management_outbox.id;


--
-- Name: management_stat_buckets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.management_stat_buckets (
    bucket_start timestamp with time zone NOT NULL,
    bucket_size text NOT NULL,
    metric text NOT NULL,
    dimension_key text DEFAULT ''::text NOT NULL,
    dimension_value text DEFAULT ''::text NOT NULL,
    value numeric NOT NULL,
    source_high_water_mark timestamp with time zone NOT NULL,
    generated_at timestamp with time zone NOT NULL
);


--
-- Name: management_stat_refresh_state; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.management_stat_refresh_state (
    job_name text NOT NULL,
    last_source_high_water_mark timestamp with time zone NOT NULL,
    last_success_at timestamp with time zone,
    last_error text,
    updated_at timestamp with time zone NOT NULL
);


--
-- Name: model_configs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.model_configs (
    id integer NOT NULL,
    profile_id integer NOT NULL,
    vendor_id integer,
    api_family character varying(50) NOT NULL,
    model_id character varying(200) NOT NULL,
    display_name character varying(200),
    loadbalance_strategy_id integer,
    context_window_tokens integer,
    default_output_token_reserve integer DEFAULT 4096 NOT NULL,
    max_context_utilization double precision DEFAULT 0.90 NOT NULL,
    preferred_context_utilization_threshold double precision,
    is_enabled boolean NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT ck_model_configs_context_window_tokens CHECK (((context_window_tokens IS NULL) OR (context_window_tokens >= 1))),
    CONSTRAINT ck_model_configs_default_output_token_reserve CHECK ((default_output_token_reserve >= 1)),
    CONSTRAINT ck_model_configs_max_context_utilization CHECK (((max_context_utilization > (0)::double precision) AND (max_context_utilization <= (1)::double precision))),
    CONSTRAINT ck_model_configs_preferred_context_utilization_threshold CHECK (((preferred_context_utilization_threshold IS NULL) OR (((preferred_context_utilization_threshold > (0)::double precision) AND (preferred_context_utilization_threshold <= (1)::double precision)) AND (preferred_context_utilization_threshold <= max_context_utilization))))
);


--
-- Name: model_configs_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.model_configs_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: model_configs_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.model_configs_id_seq OWNED BY public.model_configs.id;


--
-- Name: model_access_targets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.model_access_targets (
    id integer NOT NULL,
    profile_id integer NOT NULL,
    source_model_config_id integer NOT NULL,
    target_type character varying(20) NOT NULL,
    target_model_config_id integer,
    target_connection_id integer,
    "position" integer NOT NULL,
    weight integer,
    target_priority integer,
    is_enabled boolean NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT chk_model_access_targets_position_nonnegative CHECK (("position" >= 0)),
    CONSTRAINT chk_model_access_targets_target_metadata CHECK (((((target_type)::text = 'model'::text) AND (weight IS NOT NULL) AND (weight >= 1) AND (target_priority IS NOT NULL) AND (target_priority >= 0)) OR (((target_type)::text = 'connection'::text) AND (weight IS NULL) AND (target_priority IS NULL)))),
    CONSTRAINT chk_model_access_targets_target_type CHECK (((target_type)::text = ANY ((ARRAY['model'::character varying, 'connection'::character varying])::text[]))),
    CONSTRAINT chk_model_access_targets_one_target CHECK (((((target_type)::text = 'model'::text) AND (target_model_config_id IS NOT NULL) AND (target_connection_id IS NULL)) OR (((target_type)::text = 'connection'::text) AND (target_model_config_id IS NULL) AND (target_connection_id IS NOT NULL))))
);


--
-- Name: model_access_targets_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.model_access_targets_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: model_access_targets_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.model_access_targets_id_seq OWNED BY public.model_access_targets.id;


--
-- Name: password_reset_challenges; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.password_reset_challenges (
    id integer NOT NULL,
    auth_subject_id integer NOT NULL,
    otp_hash character varying(64) NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    attempt_count integer NOT NULL,
    requested_ip character varying(100),
    created_at timestamp with time zone NOT NULL
);


--
-- Name: password_reset_challenges_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.password_reset_challenges_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: password_reset_challenges_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.password_reset_challenges_id_seq OWNED BY public.password_reset_challenges.id;


--
-- Name: pricing_templates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.pricing_templates (
    id integer NOT NULL,
    profile_id integer NOT NULL,
    name character varying(200) NOT NULL,
    description text,
    pricing_unit character varying(20) NOT NULL,
    pricing_currency_code character varying(3) NOT NULL,
    input_price character varying(20) NOT NULL,
    output_price character varying(20) NOT NULL,
    cached_input_price character varying(20) DEFAULT '0'::character varying NOT NULL,
    cache_creation_price character varying(20) DEFAULT '0'::character varying NOT NULL,
    reasoning_price character varying(20) DEFAULT '0'::character varying NOT NULL,
    version integer NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL
);


--
-- Name: pricing_templates_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.pricing_templates_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: pricing_templates_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.pricing_templates_id_seq OWNED BY public.pricing_templates.id;


--
-- Name: profiles; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.profiles (
    id integer NOT NULL,
    name character varying(200) NOT NULL,
    description text,
    is_active boolean NOT NULL,
    is_default boolean NOT NULL,
    is_editable boolean NOT NULL,
    version integer NOT NULL,
    deleted_at timestamp with time zone,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL
);


--
-- Name: profiles_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.profiles_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: profiles_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.profiles_id_seq OWNED BY public.profiles.id;


--
-- Name: proxy_api_keys; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.proxy_api_keys (
    id integer NOT NULL,
    name character varying(200) NOT NULL,
    key_prefix character varying(200) NOT NULL,
    key_hash character varying(64) NOT NULL,
    last_four character varying(4) NOT NULL,
    is_active boolean NOT NULL,
    expires_at timestamp with time zone,
    last_used_at timestamp with time zone,
    last_used_ip character varying(100),
    created_by_auth_subject_id integer,
    notes text,
    rotated_from_id integer,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL
);


--
-- Name: proxy_api_keys_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.proxy_api_keys_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: proxy_api_keys_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.proxy_api_keys_id_seq OWNED BY public.proxy_api_keys.id;


--
-- Name: refresh_tokens; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.refresh_tokens (
    id integer NOT NULL,
    auth_subject_id integer NOT NULL,
    token_hash character varying(64) NOT NULL,
    session_duration character varying(20) NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    rotated_from_id integer,
    revoked_at timestamp with time zone,
    last_used_at timestamp with time zone,
    user_agent text,
    ip_address character varying(100),
    created_at timestamp with time zone NOT NULL
);


--
-- Name: refresh_tokens_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.refresh_tokens_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: refresh_tokens_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.refresh_tokens_id_seq OWNED BY public.refresh_tokens.id;


--
-- Name: request_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.request_logs (
    id bigint NOT NULL,
    profile_id integer NOT NULL,
    model_id character varying(200) NOT NULL,
    api_family character varying(50) NOT NULL,
    operation_name character varying(120),
    upstream_operation_name character varying(120),
    operation_translation_mode character varying(80),
    vendor_id integer,
    vendor_key character varying(100),
    vendor_name character varying(100),
    resolved_target_model_id character varying(200),
    endpoint_id integer,
    connection_id integer,
    selected_terminal_target_id integer,
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
    upstream_request_path character varying(500),
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
    context_routing jsonb,
    stream_outcome character varying(50) DEFAULT 'not_streaming'::character varying NOT NULL,
    stream_error_kind character varying(50),
    stream_error_detail text
)
PARTITION BY RANGE (created_at);


--
-- Name: request_logs_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.request_logs_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: request_logs_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.request_logs_id_seq OWNED BY public.request_logs.id;


--
-- Name: routing_connection_runtime_leases; Type: TABLE; Schema: public; Owner: -
--

CREATE UNLOGGED TABLE public.routing_connection_runtime_leases (
    lease_token character varying(64) NOT NULL,
    profile_id integer NOT NULL,
    connection_id integer NOT NULL,
    lease_kind character varying(20) NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    heartbeat_at timestamp with time zone,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT ck_routing_connection_runtime_leases_kind CHECK (((lease_kind)::text = ANY ((ARRAY['stream'::character varying, 'non_stream'::character varying])::text[])))
);


--
-- Name: routing_connection_runtime_state; Type: TABLE; Schema: public; Owner: -
--

CREATE UNLOGGED TABLE public.routing_connection_runtime_state (
    id integer NOT NULL,
    profile_id integer NOT NULL,
    connection_id integer NOT NULL,
    window_started_at timestamp with time zone,
    window_request_count integer NOT NULL,
    in_flight_non_stream integer NOT NULL,
    in_flight_stream integer NOT NULL,
    cycle_retry_attempts integer NOT NULL,
    cumulative_retry_attempts integer NOT NULL,
    next_retry_at timestamp with time zone,
    last_retry_delay_ms integer NOT NULL,
    ban_mode character varying(20) NOT NULL,
    banned_until_at timestamp with time zone,
    last_failure_kind character varying(20),
    last_success_at timestamp with time zone,
    live_p95_latency_ms integer,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT ck_rt_state_ban_mode CHECK (((ban_mode)::text = ANY ((ARRAY['off'::character varying, 'temporary'::character varying, 'until_reset'::character varying])::text[]))),
    CONSTRAINT ck_rt_state_last_failure_kind CHECK ((((last_failure_kind)::text = ANY ((ARRAY['transient_http'::character varying, 'connect_error'::character varying, 'timeout'::character varying])::text[])) OR (last_failure_kind IS NULL))),
    CONSTRAINT ck_rt_state_cycle_attempts_nonneg CHECK ((cycle_retry_attempts >= 0)),
    CONSTRAINT ck_rt_state_cumulative_attempts_nonneg CHECK ((cumulative_retry_attempts >= 0)),
    CONSTRAINT ck_rt_state_retry_delay_nonneg CHECK ((last_retry_delay_ms >= 0)),
    CONSTRAINT ck_rt_state_non_stream_nonneg CHECK ((in_flight_non_stream >= 0)),
    CONSTRAINT ck_rt_state_stream_nonneg CHECK ((in_flight_stream >= 0)),
    CONSTRAINT ck_rt_state_window_count_nonneg CHECK ((window_request_count >= 0))
);


--
-- Name: routing_connection_runtime_state_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE UNLOGGED SEQUENCE public.routing_connection_runtime_state_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: routing_connection_runtime_state_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.routing_connection_runtime_state_id_seq OWNED BY public.routing_connection_runtime_state.id;


--
-- Name: runtime_cache_generations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.runtime_cache_generations (
    domain text NOT NULL,
    scope_type text DEFAULT 'global'::text NOT NULL,
    scope_id text DEFAULT '*'::text NOT NULL,
    version bigint DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_by text,
    reason text,
    CONSTRAINT runtime_cache_generations_version_check CHECK ((version >= 0))
);


--
-- Name: runtime_telemetry_outbox; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.runtime_telemetry_outbox (
    id bigint NOT NULL,
    profile_id integer NOT NULL,
    ingress_request_id character varying(36) NOT NULL,
    payload jsonb NOT NULL,
    created_at timestamp with time zone NOT NULL
);


--
-- Name: runtime_telemetry_outbox_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.runtime_telemetry_outbox_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: runtime_telemetry_outbox_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.runtime_telemetry_outbox_id_seq OWNED BY public.runtime_telemetry_outbox.id;


--
-- Name: sidecar_instances; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sidecar_instances (
    id integer NOT NULL,
    name text NOT NULL,
    base_url text NOT NULL,
    base_url_canonical text NOT NULL,
    management_password text NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    environment_label text,
    sync_interval_seconds integer DEFAULT 300 NOT NULL,
    request_timeout_seconds integer DEFAULT 10 NOT NULL,
    allow_private_network boolean DEFAULT false NOT NULL,
    allow_insecure_http boolean DEFAULT false NOT NULL,
    skip_tls_verify boolean DEFAULT false NOT NULL,
    last_sync_at timestamp with time zone,
    last_successful_sync_at timestamp with time zone,
    snapshot_stale_after timestamp with time zone,
    last_sync_error text,
    management_auth_state text DEFAULT 'unknown'::text NOT NULL,
    auth_failure_pause_until timestamp with time zone,
    deleted_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT ck_sidecar_instances_management_auth_state CHECK ((management_auth_state = ANY (ARRAY['unknown'::text, 'valid'::text, 'invalid_management_auth'::text]))),
    CONSTRAINT ck_sidecar_instances_request_timeout_positive CHECK ((request_timeout_seconds > 0)),
    CONSTRAINT ck_sidecar_instances_sync_interval_positive CHECK ((sync_interval_seconds > 0))
);


--
-- Name: sidecar_instances_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sidecar_instances_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: sidecar_instances_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sidecar_instances_id_seq OWNED BY public.sidecar_instances.id;


--
-- Name: sidecar_provider_snapshots; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.sidecar_provider_snapshots (
    id integer NOT NULL,
    sidecar_id integer NOT NULL,
    provider_key text NOT NULL,
    provider_item_key text NOT NULL,
    name text,
    label text,
    status text,
    disabled boolean,
    snapshot_json jsonb DEFAULT '{}'::jsonb NOT NULL,
    observed_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: sidecar_provider_snapshots_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.sidecar_provider_snapshots_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: sidecar_provider_snapshots_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.sidecar_provider_snapshots_id_seq OWNED BY public.sidecar_provider_snapshots.id;


--
-- Name: usage_request_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.usage_request_events (
    id bigint NOT NULL,
    profile_id integer NOT NULL,
    ingress_request_id character varying(36) NOT NULL,
    model_id character varying(200) NOT NULL,
    resolved_target_model_id character varying(200),
    api_family character varying(50) NOT NULL,
    operation_name character varying(120),
    upstream_operation_name character varying(120),
    operation_translation_mode character varying(80),
    endpoint_id integer,
    connection_id integer,
    selected_terminal_target_id integer,
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
    upstream_request_path character varying(500),
    created_at timestamp with time zone NOT NULL,
    response_time_ms integer,
    completion_duration_ms integer,
    ttft_ms integer,
    billable_flag boolean,
    priced_flag boolean,
    unpriced_reason character varying(50),
    stream_outcome character varying(50) DEFAULT 'not_streaming'::character varying NOT NULL,
    stream_error_kind character varying(50),
    context_routing jsonb,
    CONSTRAINT ck_usage_request_events_attempt_count_positive CHECK ((attempt_count >= 1))
)
PARTITION BY RANGE (created_at);


--
-- Name: usage_request_events_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.usage_request_events_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: usage_request_events_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.usage_request_events_id_seq OWNED BY public.usage_request_events.id;


--
-- Name: user_agent_client_rules; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_agent_client_rules (
    id integer NOT NULL,
    profile_id integer,
    name character varying(200) NOT NULL,
    pattern text NOT NULL,
    enabled boolean NOT NULL,
    is_system boolean NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    CONSTRAINT ck_uacr_profile_scope CHECK ((((is_system = true) AND (profile_id IS NULL)) OR ((is_system = false) AND (profile_id IS NOT NULL))))
);


--
-- Name: user_agent_client_rules_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.user_agent_client_rules_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: user_agent_client_rules_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.user_agent_client_rules_id_seq OWNED BY public.user_agent_client_rules.id;


--
-- Name: user_settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_settings (
    id integer NOT NULL,
    profile_id integer NOT NULL,
    report_currency_code character varying(3) NOT NULL,
    report_currency_symbol character varying(5) NOT NULL,
    timezone_preference character varying(100),
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    request_logs_retention_days integer,
    statistics_retention_days integer,
    audit_logs_retention_days integer,
    CONSTRAINT user_settings_audit_logs_retention_days_check CHECK (((audit_logs_retention_days IS NULL) OR (audit_logs_retention_days >= 1))),
    CONSTRAINT user_settings_request_logs_retention_days_check CHECK (((request_logs_retention_days IS NULL) OR (request_logs_retention_days >= 1))),
    CONSTRAINT user_settings_statistics_retention_days_check CHECK (((statistics_retention_days IS NULL) OR (statistics_retention_days >= 1)))
);


--
-- Name: user_settings_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.user_settings_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: user_settings_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.user_settings_id_seq OWNED BY public.user_settings.id;


--
-- Name: vendors; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.vendors (
    id integer NOT NULL,
    key character varying(100) NOT NULL,
    name character varying(100) NOT NULL,
    description text,
    icon_key character varying(100),
    audit_enabled boolean NOT NULL,
    audit_capture_bodies boolean NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL
);


--
-- Name: vendors_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.vendors_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: vendors_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.vendors_id_seq OWNED BY public.vendors.id;


--
-- Name: webauthn_challenges; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.webauthn_challenges (
    id integer NOT NULL,
    challenge_key character varying(100) NOT NULL,
    challenge bytea NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone NOT NULL
);


--
-- Name: webauthn_challenges_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.webauthn_challenges_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: webauthn_challenges_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.webauthn_challenges_id_seq OWNED BY public.webauthn_challenges.id;


--
-- Name: webauthn_credentials; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.webauthn_credentials (
    id integer NOT NULL,
    auth_subject_id integer NOT NULL,
    credential_id bytea NOT NULL,
    public_key bytea NOT NULL,
    sign_count bigint DEFAULT '0'::bigint NOT NULL,
    device_name character varying(200),
    aaguid bytea,
    transports text[],
    backup_eligible boolean DEFAULT false,
    backup_state boolean DEFAULT false,
    last_used_at timestamp with time zone,
    last_used_ip character varying(45),
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: webauthn_credentials_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.webauthn_credentials_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: webauthn_credentials_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.webauthn_credentials_id_seq OWNED BY public.webauthn_credentials.id;


--
-- Name: app_auth_settings id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.app_auth_settings ALTER COLUMN id SET DEFAULT nextval('public.app_auth_settings_id_seq'::regclass);


--
-- Name: audit_logs id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_logs ALTER COLUMN id SET DEFAULT nextval('public.audit_logs_id_seq'::regclass);


--
-- Name: connections id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.connections ALTER COLUMN id SET DEFAULT nextval('public.connections_id_seq'::regclass);


--
-- Name: endpoint_fx_rate_settings id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.endpoint_fx_rate_settings ALTER COLUMN id SET DEFAULT nextval('public.endpoint_fx_rate_settings_id_seq'::regclass);


--
-- Name: endpoints id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.endpoints ALTER COLUMN id SET DEFAULT nextval('public.endpoints_id_seq'::regclass);


--
-- Name: header_blocklist_rules id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.header_blocklist_rules ALTER COLUMN id SET DEFAULT nextval('public.header_blocklist_rules_id_seq'::regclass);


--
-- Name: loadbalance_events id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.loadbalance_events ALTER COLUMN id SET DEFAULT nextval('public.loadbalance_events_id_seq'::regclass);


--
-- Name: loadbalance_round_robin_state id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.loadbalance_round_robin_state ALTER COLUMN id SET DEFAULT nextval('public.loadbalance_round_robin_state_id_seq'::regclass);


--
-- Name: loadbalance_strategies id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.loadbalance_strategies ALTER COLUMN id SET DEFAULT nextval('public.loadbalance_strategies_id_seq'::regclass);


--
-- Name: management_job_events id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.management_job_events ALTER COLUMN id SET DEFAULT nextval('public.management_job_events_id_seq'::regclass);


--
-- Name: management_outbox id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.management_outbox ALTER COLUMN id SET DEFAULT nextval('public.management_outbox_id_seq'::regclass);


--
-- Name: model_configs id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_configs ALTER COLUMN id SET DEFAULT nextval('public.model_configs_id_seq'::regclass);


--
-- Name: model_access_targets id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_access_targets ALTER COLUMN id SET DEFAULT nextval('public.model_access_targets_id_seq'::regclass);


--
-- Name: password_reset_challenges id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.password_reset_challenges ALTER COLUMN id SET DEFAULT nextval('public.password_reset_challenges_id_seq'::regclass);


--
-- Name: pricing_templates id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pricing_templates ALTER COLUMN id SET DEFAULT nextval('public.pricing_templates_id_seq'::regclass);


--
-- Name: profiles id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.profiles ALTER COLUMN id SET DEFAULT nextval('public.profiles_id_seq'::regclass);


--
-- Name: proxy_api_keys id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.proxy_api_keys ALTER COLUMN id SET DEFAULT nextval('public.proxy_api_keys_id_seq'::regclass);


--
-- Name: refresh_tokens id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.refresh_tokens ALTER COLUMN id SET DEFAULT nextval('public.refresh_tokens_id_seq'::regclass);


--
-- Name: request_logs id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.request_logs ALTER COLUMN id SET DEFAULT nextval('public.request_logs_id_seq'::regclass);


--
-- Name: routing_connection_runtime_state id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.routing_connection_runtime_state ALTER COLUMN id SET DEFAULT nextval('public.routing_connection_runtime_state_id_seq'::regclass);


--
-- Name: runtime_telemetry_outbox id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.runtime_telemetry_outbox ALTER COLUMN id SET DEFAULT nextval('public.runtime_telemetry_outbox_id_seq'::regclass);


--
-- Name: sidecar_instances id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sidecar_instances ALTER COLUMN id SET DEFAULT nextval('public.sidecar_instances_id_seq'::regclass);


--
-- Name: sidecar_provider_snapshots id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sidecar_provider_snapshots ALTER COLUMN id SET DEFAULT nextval('public.sidecar_provider_snapshots_id_seq'::regclass);


--
-- Name: usage_request_events id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usage_request_events ALTER COLUMN id SET DEFAULT nextval('public.usage_request_events_id_seq'::regclass);


--
-- Name: user_agent_client_rules id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_agent_client_rules ALTER COLUMN id SET DEFAULT nextval('public.user_agent_client_rules_id_seq'::regclass);


--
-- Name: user_settings id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_settings ALTER COLUMN id SET DEFAULT nextval('public.user_settings_id_seq'::regclass);


--
-- Name: vendors id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vendors ALTER COLUMN id SET DEFAULT nextval('public.vendors_id_seq'::regclass);


--
-- Name: webauthn_challenges id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webauthn_challenges ALTER COLUMN id SET DEFAULT nextval('public.webauthn_challenges_id_seq'::regclass);


--
-- Name: webauthn_credentials id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webauthn_credentials ALTER COLUMN id SET DEFAULT nextval('public.webauthn_credentials_id_seq'::regclass);


--
-- Name: app_auth_settings app_auth_settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.app_auth_settings
    ADD CONSTRAINT app_auth_settings_pkey PRIMARY KEY (id);


--
-- Name: audit_logs audit_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.audit_logs
    ADD CONSTRAINT audit_logs_pkey PRIMARY KEY (created_at, id);


--
-- Name: connections connections_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.connections
    ADD CONSTRAINT connections_pkey PRIMARY KEY (id);


--
-- Name: connections uq_connections_id_profile; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.connections
    ADD CONSTRAINT uq_connections_id_profile UNIQUE (id, profile_id);


--
-- Name: email_outbox email_outbox_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.email_outbox
    ADD CONSTRAINT email_outbox_pkey PRIMARY KEY (id);


--
-- Name: endpoint_fx_rate_settings endpoint_fx_rate_settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.endpoint_fx_rate_settings
    ADD CONSTRAINT endpoint_fx_rate_settings_pkey PRIMARY KEY (id);


--
-- Name: endpoints endpoints_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.endpoints
    ADD CONSTRAINT endpoints_pkey PRIMARY KEY (id);


--
-- Name: header_blocklist_rules header_blocklist_rules_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.header_blocklist_rules
    ADD CONSTRAINT header_blocklist_rules_pkey PRIMARY KEY (id);


--
-- Name: loadbalance_events loadbalance_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.loadbalance_events
    ADD CONSTRAINT loadbalance_events_pkey PRIMARY KEY (created_at, id);


--
-- Name: loadbalance_round_robin_state loadbalance_round_robin_state_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.loadbalance_round_robin_state
    ADD CONSTRAINT loadbalance_round_robin_state_pkey PRIMARY KEY (id);


--
-- Name: loadbalance_strategies loadbalance_strategies_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.loadbalance_strategies
    ADD CONSTRAINT loadbalance_strategies_pkey PRIMARY KEY (id);


--
-- Name: log_retention_settings log_retention_settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.log_retention_settings
    ADD CONSTRAINT log_retention_settings_pkey PRIMARY KEY (singleton_key);


--
-- Name: management_job_events management_job_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.management_job_events
    ADD CONSTRAINT management_job_events_pkey PRIMARY KEY (id);


--
-- Name: management_jobs management_jobs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.management_jobs
    ADD CONSTRAINT management_jobs_pkey PRIMARY KEY (id);


--
-- Name: management_outbox management_outbox_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.management_outbox
    ADD CONSTRAINT management_outbox_pkey PRIMARY KEY (id);


--
-- Name: management_stat_buckets management_stat_buckets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.management_stat_buckets
    ADD CONSTRAINT management_stat_buckets_pkey PRIMARY KEY (bucket_start, bucket_size, metric, dimension_key, dimension_value);


--
-- Name: management_stat_refresh_state management_stat_refresh_state_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.management_stat_refresh_state
    ADD CONSTRAINT management_stat_refresh_state_pkey PRIMARY KEY (job_name);


--
-- Name: model_configs model_configs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_configs
    ADD CONSTRAINT model_configs_pkey PRIMARY KEY (id);


--
-- Name: model_access_targets model_access_targets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_access_targets
    ADD CONSTRAINT model_access_targets_pkey PRIMARY KEY (id);


--
-- Name: password_reset_challenges password_reset_challenges_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.password_reset_challenges
    ADD CONSTRAINT password_reset_challenges_pkey PRIMARY KEY (id);


--
-- Name: pricing_templates pricing_templates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pricing_templates
    ADD CONSTRAINT pricing_templates_pkey PRIMARY KEY (id);


--
-- Name: profiles profiles_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.profiles
    ADD CONSTRAINT profiles_name_key UNIQUE (name);


--
-- Name: profiles profiles_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.profiles
    ADD CONSTRAINT profiles_pkey PRIMARY KEY (id);


--
-- Name: proxy_api_keys proxy_api_keys_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.proxy_api_keys
    ADD CONSTRAINT proxy_api_keys_pkey PRIMARY KEY (id);


--
-- Name: refresh_tokens refresh_tokens_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.refresh_tokens
    ADD CONSTRAINT refresh_tokens_pkey PRIMARY KEY (id);


--
-- Name: refresh_tokens refresh_tokens_token_hash_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.refresh_tokens
    ADD CONSTRAINT refresh_tokens_token_hash_key UNIQUE (token_hash);


--
-- Name: request_logs request_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.request_logs
    ADD CONSTRAINT request_logs_pkey PRIMARY KEY (created_at, id);


--
-- Name: routing_connection_runtime_leases routing_connection_runtime_leases_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.routing_connection_runtime_leases
    ADD CONSTRAINT routing_connection_runtime_leases_pkey PRIMARY KEY (lease_token);


--
-- Name: routing_connection_runtime_state routing_connection_runtime_state_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.routing_connection_runtime_state
    ADD CONSTRAINT routing_connection_runtime_state_pkey PRIMARY KEY (id);


--
-- Name: runtime_cache_generations runtime_cache_generations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.runtime_cache_generations
    ADD CONSTRAINT runtime_cache_generations_pkey PRIMARY KEY (domain, scope_type, scope_id);


--
-- Name: runtime_telemetry_outbox runtime_telemetry_outbox_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.runtime_telemetry_outbox
    ADD CONSTRAINT runtime_telemetry_outbox_pkey PRIMARY KEY (id);


--
-- Name: sidecar_instances sidecar_instances_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sidecar_instances
    ADD CONSTRAINT sidecar_instances_pkey PRIMARY KEY (id);


--
-- Name: sidecar_provider_snapshots sidecar_provider_snapshots_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sidecar_provider_snapshots
    ADD CONSTRAINT sidecar_provider_snapshots_pkey PRIMARY KEY (id);


--
-- Name: app_auth_settings uq_app_auth_settings_singleton_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.app_auth_settings
    ADD CONSTRAINT uq_app_auth_settings_singleton_key UNIQUE (singleton_key);


--
-- Name: webauthn_credentials uq_credential_id; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webauthn_credentials
    ADD CONSTRAINT uq_credential_id UNIQUE (credential_id);


--
-- Name: endpoints uq_endpoints_profile_name; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.endpoints
    ADD CONSTRAINT uq_endpoints_profile_name UNIQUE (profile_id, name);


--
-- Name: endpoint_fx_rate_settings uq_fx_profile_model_endpoint; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.endpoint_fx_rate_settings
    ADD CONSTRAINT uq_fx_profile_model_endpoint UNIQUE (profile_id, model_id, endpoint_id);


--
-- Name: header_blocklist_rules uq_hbr_profile_match_pattern; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.header_blocklist_rules
    ADD CONSTRAINT uq_hbr_profile_match_pattern UNIQUE (profile_id, match_type, pattern);


--
-- Name: loadbalance_round_robin_state uq_loadbalance_round_robin_state_profile_model; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.loadbalance_round_robin_state
    ADD CONSTRAINT uq_loadbalance_round_robin_state_profile_model UNIQUE (profile_id, model_config_id);


--
-- Name: loadbalance_strategies uq_loadbalance_strategies_profile_id_id; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.loadbalance_strategies
    ADD CONSTRAINT uq_loadbalance_strategies_profile_id_id UNIQUE (profile_id, id);


--
-- Name: loadbalance_strategies uq_loadbalance_strategies_profile_name; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.loadbalance_strategies
    ADD CONSTRAINT uq_loadbalance_strategies_profile_name UNIQUE (profile_id, name);


--
-- Name: model_configs uq_model_configs_profile_model_id; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_configs
    ADD CONSTRAINT uq_model_configs_profile_model_id UNIQUE (profile_id, model_id);


--
-- Name: model_configs uq_model_configs_id_profile; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_configs
    ADD CONSTRAINT uq_model_configs_id_profile UNIQUE (id, profile_id);


--
-- Name: model_access_targets uq_model_access_targets_source_position; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_access_targets
    ADD CONSTRAINT uq_model_access_targets_source_position UNIQUE (source_model_config_id, "position") DEFERRABLE INITIALLY DEFERRED;


--
-- Name: pricing_templates uq_pricing_templates_profile_name; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pricing_templates
    ADD CONSTRAINT uq_pricing_templates_profile_name UNIQUE (profile_id, name);


--
-- Name: proxy_api_keys uq_proxy_api_keys_prefix; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.proxy_api_keys
    ADD CONSTRAINT uq_proxy_api_keys_prefix UNIQUE (key_prefix);


--
-- Name: routing_connection_runtime_state uq_routing_connection_runtime_state_profile_connection; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.routing_connection_runtime_state
    ADD CONSTRAINT uq_routing_connection_runtime_state_profile_connection UNIQUE (profile_id, connection_id);


--
-- Name: sidecar_provider_snapshots uq_sidecar_provider_snapshots_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sidecar_provider_snapshots
    ADD CONSTRAINT uq_sidecar_provider_snapshots_key UNIQUE (sidecar_id, provider_key, provider_item_key);


--
-- Name: user_settings uq_user_settings_profile_id; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_settings
    ADD CONSTRAINT uq_user_settings_profile_id UNIQUE (profile_id);


--
-- Name: usage_request_events usage_request_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usage_request_events
    ADD CONSTRAINT usage_request_events_pkey PRIMARY KEY (created_at, id);


--
-- Name: user_agent_client_rules user_agent_client_rules_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_agent_client_rules
    ADD CONSTRAINT user_agent_client_rules_pkey PRIMARY KEY (id);


--
-- Name: user_settings user_settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_settings
    ADD CONSTRAINT user_settings_pkey PRIMARY KEY (id);


--
-- Name: vendors vendors_key_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vendors
    ADD CONSTRAINT vendors_key_key UNIQUE (key);


--
-- Name: vendors vendors_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vendors
    ADD CONSTRAINT vendors_name_key UNIQUE (name);


--
-- Name: vendors vendors_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.vendors
    ADD CONSTRAINT vendors_pkey PRIMARY KEY (id);


--
-- Name: webauthn_challenges webauthn_challenges_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webauthn_challenges
    ADD CONSTRAINT webauthn_challenges_pkey PRIMARY KEY (id);


--
-- Name: webauthn_credentials webauthn_credentials_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webauthn_credentials
    ADD CONSTRAINT webauthn_credentials_pkey PRIMARY KEY (id);


--
-- Name: idx_audit_logs_connection_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_connection_id ON ONLY public.audit_logs USING btree (connection_id);


--
-- Name: idx_audit_logs_profile_connection_created_id_desc; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_profile_connection_created_id_desc ON ONLY public.audit_logs USING btree (profile_id, connection_id, created_at DESC, id DESC);


--
-- Name: idx_audit_logs_profile_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_profile_created_at ON ONLY public.audit_logs USING btree (profile_id, created_at);


--
-- Name: idx_audit_logs_profile_created_id_desc; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_profile_created_id_desc ON ONLY public.audit_logs USING btree (profile_id, created_at DESC, id DESC);


--
-- Name: idx_audit_logs_profile_endpoint_created_id_desc; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_profile_endpoint_created_id_desc ON ONLY public.audit_logs USING btree (profile_id, endpoint_id, created_at DESC, id DESC);


--
-- Name: idx_audit_logs_profile_model_created_id_desc; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_profile_model_created_id_desc ON ONLY public.audit_logs USING btree (profile_id, model_id, created_at DESC, id DESC);


--
-- Name: idx_audit_logs_profile_request_created_id_desc; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_profile_request_created_id_desc ON ONLY public.audit_logs USING btree (profile_id, request_log_id, created_at DESC, id DESC);


--
-- Name: idx_audit_logs_profile_status_created_id_desc; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_profile_status_created_id_desc ON ONLY public.audit_logs USING btree (profile_id, response_status, created_at DESC, id DESC);


--
-- Name: idx_audit_logs_profile_vendor_created_id_desc; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_audit_logs_profile_vendor_created_id_desc ON ONLY public.audit_logs USING btree (profile_id, vendor_id, created_at DESC, id DESC);


--
-- Name: idx_connections_endpoint_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_connections_endpoint_id ON public.connections USING btree (endpoint_id);


--
-- Name: idx_connections_api_family; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_connections_api_family ON public.connections USING btree (api_family);


--
-- Name: idx_connections_is_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_connections_is_active ON public.connections USING btree (is_active);


--
-- Name: idx_connections_pricing_template_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_connections_pricing_template_id ON public.connections USING btree (pricing_template_id);


--
-- Name: idx_connections_priority; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_connections_priority ON public.connections USING btree (priority);


--
-- Name: idx_connections_profile_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_connections_profile_id ON public.connections USING btree (profile_id);


--
-- Name: idx_connections_profile_family_active_priority; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_connections_profile_family_active_priority ON public.connections USING btree (profile_id, api_family, is_active, priority);


--
-- Name: idx_email_outbox_dead_letters; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_email_outbox_dead_letters ON public.email_outbox USING btree (dead_lettered_at DESC) WHERE (status = 'dead'::text);


--
-- Name: idx_email_outbox_due; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_email_outbox_due ON public.email_outbox USING btree (next_attempt_at, created_at, id) WHERE (status = 'queued'::text);


--
-- Name: idx_email_outbox_idempotency_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_email_outbox_idempotency_key ON public.email_outbox USING btree (idempotency_key);


--
-- Name: idx_email_outbox_kind; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_email_outbox_kind ON public.email_outbox USING btree (kind);


--
-- Name: idx_email_outbox_stale_locks; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_email_outbox_stale_locks ON public.email_outbox USING btree (locked_until) WHERE (status = 'sending'::text);


--
-- Name: idx_endpoints_profile_position; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_endpoints_profile_position ON public.endpoints USING btree (profile_id, "position");


--
-- Name: idx_fx_endpoint_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_fx_endpoint_id ON public.endpoint_fx_rate_settings USING btree (endpoint_id);


--
-- Name: idx_fx_profile_model_endpoint; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_fx_profile_model_endpoint ON public.endpoint_fx_rate_settings USING btree (profile_id, model_id, endpoint_id);


--
-- Name: idx_hbr_enabled; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_hbr_enabled ON public.header_blocklist_rules USING btree (enabled);


--
-- Name: idx_loadbalance_events_connection; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_loadbalance_events_connection ON ONLY public.loadbalance_events USING btree (connection_id, created_at);


--
-- Name: idx_loadbalance_events_event_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_loadbalance_events_event_type ON ONLY public.loadbalance_events USING btree (event_type);


--
-- Name: idx_loadbalance_events_profile_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_loadbalance_events_profile_created ON ONLY public.loadbalance_events USING btree (profile_id, created_at);


--
-- Name: idx_loadbalance_round_robin_state_profile_model; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_loadbalance_round_robin_state_profile_model ON public.loadbalance_round_robin_state USING btree (profile_id, model_config_id);


--
-- Name: idx_loadbalance_strategies_profile_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_loadbalance_strategies_profile_id ON public.loadbalance_strategies USING btree (profile_id);


--
-- Name: idx_management_job_events_job_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_management_job_events_job_created ON public.management_job_events USING btree (job_id, created_at, id);


--
-- Name: idx_management_jobs_due; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_management_jobs_due ON public.management_jobs USING btree (next_attempt_at, created_at, id) WHERE (state = ANY (ARRAY['queued'::text, 'running'::text]));


--
-- Name: idx_management_jobs_idempotency; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_management_jobs_idempotency ON public.management_jobs USING btree (type, requested_by, idempotency_key) WHERE (idempotency_key IS NOT NULL);


--
-- Name: idx_management_jobs_type_state_updated; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_management_jobs_type_state_updated ON public.management_jobs USING btree (type, state, updated_at DESC);


--
-- Name: idx_management_outbox_aggregate; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_management_outbox_aggregate ON public.management_outbox USING btree (aggregate_type, aggregate_id, aggregate_version);


--
-- Name: idx_management_outbox_dedupe_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_management_outbox_dedupe_key ON public.management_outbox USING btree (dedupe_key);


--
-- Name: idx_management_outbox_operation; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_management_outbox_operation ON public.management_outbox USING btree (operation_id);


--
-- Name: idx_management_outbox_polling; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_management_outbox_polling ON public.management_outbox USING btree (status, next_attempt_at, created_at, id);


--
-- Name: idx_management_stat_buckets_dashboard_profile; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_management_stat_buckets_dashboard_profile ON public.management_stat_buckets USING btree (dimension_key, dimension_value, bucket_size, metric);


--
-- Name: idx_model_configs_loadbalance_strategy_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_model_configs_loadbalance_strategy_id ON public.model_configs USING btree (loadbalance_strategy_id);


--
-- Name: idx_model_configs_profile_model_enabled; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_model_configs_profile_model_enabled ON public.model_configs USING btree (profile_id, model_id, is_enabled);


--
-- Name: idx_model_access_targets_connection; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_model_access_targets_connection ON public.model_access_targets USING btree (target_connection_id) WHERE (target_connection_id IS NOT NULL);


--
-- Name: uq_model_access_targets_connection_owner; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_model_access_targets_connection_owner ON public.model_access_targets USING btree (target_connection_id) WHERE (target_connection_id IS NOT NULL);


--
-- Name: idx_model_access_targets_profile_source_position; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_model_access_targets_profile_source_position ON public.model_access_targets USING btree (profile_id, source_model_config_id, "position");


--
-- Name: idx_model_access_targets_target_model; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_model_access_targets_target_model ON public.model_access_targets USING btree (target_model_config_id) WHERE (target_model_config_id IS NOT NULL);


--
-- Name: uq_model_access_targets_source_target_connection; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_model_access_targets_source_target_connection ON public.model_access_targets USING btree (source_model_config_id, target_connection_id) WHERE (target_connection_id IS NOT NULL);


--
-- Name: uq_model_access_targets_source_target_model; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_model_access_targets_source_target_model ON public.model_access_targets USING btree (source_model_config_id, target_model_config_id) WHERE (target_model_config_id IS NOT NULL);


--
-- Name: idx_password_reset_challenges_consumed_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_password_reset_challenges_consumed_at ON public.password_reset_challenges USING btree (consumed_at);


--
-- Name: idx_password_reset_challenges_expires_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_password_reset_challenges_expires_at ON public.password_reset_challenges USING btree (expires_at);


--
-- Name: idx_pricing_templates_profile_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_pricing_templates_profile_id ON public.pricing_templates USING btree (profile_id);


--
-- Name: idx_profiles_deleted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_profiles_deleted_at ON public.profiles USING btree (deleted_at);


--
-- Name: idx_proxy_api_keys_is_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_proxy_api_keys_is_active ON public.proxy_api_keys USING btree (is_active);


--
-- Name: idx_refresh_tokens_expires_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_refresh_tokens_expires_at ON public.refresh_tokens USING btree (expires_at);


--
-- Name: idx_refresh_tokens_revoked_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_refresh_tokens_revoked_at ON public.refresh_tokens USING btree (revoked_at);


--
-- Name: idx_request_logs_billable_flag; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_request_logs_billable_flag ON ONLY public.request_logs USING btree (billable_flag);


--
-- Name: idx_request_logs_ingress_request_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_request_logs_ingress_request_id ON ONLY public.request_logs USING btree (ingress_request_id);


--
-- Name: idx_request_logs_priced_flag; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_request_logs_priced_flag ON ONLY public.request_logs USING btree (priced_flag);


--
-- Name: idx_request_logs_profile_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_request_logs_profile_created_at ON ONLY public.request_logs USING btree (profile_id, created_at);


--
-- Name: idx_routing_connection_runtime_leases_expires_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_routing_connection_runtime_leases_expires_at ON public.routing_connection_runtime_leases USING btree (expires_at);


--
-- Name: idx_routing_connection_runtime_leases_profile_connection; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_routing_connection_runtime_leases_profile_connection ON public.routing_connection_runtime_leases USING btree (profile_id, connection_id);


--
-- Name: idx_routing_connection_runtime_state_profile_connection; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_routing_connection_runtime_state_profile_connection ON public.routing_connection_runtime_state USING btree (profile_id, connection_id);


--
-- Name: idx_runtime_cache_generations_domain_scope; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_runtime_cache_generations_domain_scope ON public.runtime_cache_generations USING btree (domain, scope_type, scope_id, version);


--
-- Name: idx_runtime_telemetry_outbox_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_runtime_telemetry_outbox_created_at ON public.runtime_telemetry_outbox USING btree (created_at, id);


--
-- Name: idx_runtime_telemetry_outbox_profile_ingress_request_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_runtime_telemetry_outbox_profile_ingress_request_id ON public.runtime_telemetry_outbox USING btree (profile_id, ingress_request_id);


--
-- Name: idx_sidecar_provider_snapshots_sidecar_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_sidecar_provider_snapshots_sidecar_id ON public.sidecar_provider_snapshots USING btree (sidecar_id);


--
-- Name: idx_uacr_enabled; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_uacr_enabled ON public.user_agent_client_rules USING btree (enabled);


--
-- Name: idx_usage_request_events_ingress_request_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_usage_request_events_ingress_request_id ON ONLY public.usage_request_events USING btree (ingress_request_id);


--
-- Name: idx_usage_request_events_profile_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_usage_request_events_profile_created_at ON ONLY public.usage_request_events USING btree (profile_id, created_at);


--
-- Name: idx_usage_request_events_profile_ingress_request; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_usage_request_events_profile_ingress_request ON ONLY public.usage_request_events USING btree (profile_id, ingress_request_id);


--
-- Name: idx_webauthn_challenges_challenge_key; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webauthn_challenges_challenge_key ON public.webauthn_challenges USING btree (challenge_key);


--
-- Name: idx_webauthn_challenges_expires_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webauthn_challenges_expires_at ON public.webauthn_challenges USING btree (expires_at);


--
-- Name: idx_webauthn_credentials_auth_subject; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webauthn_credentials_auth_subject ON public.webauthn_credentials USING btree (auth_subject_id);


--
-- Name: idx_webauthn_credentials_last_used; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_webauthn_credentials_last_used ON public.webauthn_credentials USING btree (last_used_at);


--
-- Name: ix_audit_logs_connection_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_audit_logs_connection_id ON ONLY public.audit_logs USING btree (connection_id);


--
-- Name: ix_audit_logs_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_audit_logs_created_at ON ONLY public.audit_logs USING btree (created_at);


--
-- Name: ix_audit_logs_endpoint_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_audit_logs_endpoint_id ON ONLY public.audit_logs USING btree (endpoint_id);


--
-- Name: ix_audit_logs_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_audit_logs_id ON ONLY public.audit_logs USING btree (id);


--
-- Name: ix_audit_logs_model_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_audit_logs_model_id ON ONLY public.audit_logs USING btree (model_id);


--
-- Name: ix_audit_logs_profile_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_audit_logs_profile_id ON ONLY public.audit_logs USING btree (profile_id);


--
-- Name: ix_audit_logs_request_log_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_audit_logs_request_log_id ON ONLY public.audit_logs USING btree (request_log_id);


--
-- Name: ix_audit_logs_response_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_audit_logs_response_status ON ONLY public.audit_logs USING btree (response_status);


--
-- Name: ix_audit_logs_vendor_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_audit_logs_vendor_id ON ONLY public.audit_logs USING btree (vendor_id);


--
-- Name: ix_connections_profile_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_connections_profile_id ON public.connections USING btree (profile_id);


--
-- Name: ix_endpoint_fx_rate_settings_profile_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_endpoint_fx_rate_settings_profile_id ON public.endpoint_fx_rate_settings USING btree (profile_id);


--
-- Name: ix_endpoints_profile_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_endpoints_profile_id ON public.endpoints USING btree (profile_id);


--
-- Name: ix_header_blocklist_rules_profile_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_header_blocklist_rules_profile_id ON public.header_blocklist_rules USING btree (profile_id);


--
-- Name: ix_loadbalance_events_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_loadbalance_events_created_at ON ONLY public.loadbalance_events USING btree (created_at);


--
-- Name: ix_loadbalance_events_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_loadbalance_events_id ON ONLY public.loadbalance_events USING btree (id);


--
-- Name: ix_loadbalance_events_profile_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_loadbalance_events_profile_id ON ONLY public.loadbalance_events USING btree (profile_id);


--
-- Name: ix_loadbalance_strategies_profile_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_loadbalance_strategies_profile_id ON public.loadbalance_strategies USING btree (profile_id);


--
-- Name: ix_model_configs_profile_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_model_configs_profile_id ON public.model_configs USING btree (profile_id);


--
-- Name: ix_password_reset_challenges_auth_subject_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_password_reset_challenges_auth_subject_id ON public.password_reset_challenges USING btree (auth_subject_id);


--
-- Name: ix_pricing_templates_profile_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_pricing_templates_profile_id ON public.pricing_templates USING btree (profile_id);


--
-- Name: ix_refresh_tokens_auth_subject_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_refresh_tokens_auth_subject_id ON public.refresh_tokens USING btree (auth_subject_id);


--
-- Name: ix_request_logs_api_family; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_request_logs_api_family ON ONLY public.request_logs USING btree (api_family);


--
-- Name: ix_request_logs_connection_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_request_logs_connection_id ON ONLY public.request_logs USING btree (connection_id);


--
-- Name: ix_request_logs_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_request_logs_created_at ON ONLY public.request_logs USING btree (created_at);


--
-- Name: ix_request_logs_endpoint_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_request_logs_endpoint_id ON ONLY public.request_logs USING btree (endpoint_id);


--
-- Name: ix_request_logs_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_request_logs_id ON ONLY public.request_logs USING btree (id);


--
-- Name: ix_request_logs_model_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_request_logs_model_id ON ONLY public.request_logs USING btree (model_id);


--
-- Name: ix_request_logs_profile_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_request_logs_profile_id ON ONLY public.request_logs USING btree (profile_id);


--
-- Name: ix_request_logs_proxy_api_key_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_request_logs_proxy_api_key_id ON ONLY public.request_logs USING btree (proxy_api_key_id);


--
-- Name: ix_request_logs_status_code; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_request_logs_status_code ON ONLY public.request_logs USING btree (status_code);


--
-- Name: ix_request_logs_vendor_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_request_logs_vendor_id ON ONLY public.request_logs USING btree (vendor_id);


--
-- Name: ix_usage_request_events_api_family; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_usage_request_events_api_family ON ONLY public.usage_request_events USING btree (api_family);


--
-- Name: ix_usage_request_events_connection_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_usage_request_events_connection_id ON ONLY public.usage_request_events USING btree (connection_id);


--
-- Name: ix_usage_request_events_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_usage_request_events_created_at ON ONLY public.usage_request_events USING btree (created_at);


--
-- Name: ix_usage_request_events_endpoint_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_usage_request_events_endpoint_id ON ONLY public.usage_request_events USING btree (endpoint_id);


--
-- Name: ix_usage_request_events_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_usage_request_events_id ON ONLY public.usage_request_events USING btree (id);


--
-- Name: ix_usage_request_events_model_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_usage_request_events_model_id ON ONLY public.usage_request_events USING btree (model_id);


--
-- Name: ix_usage_request_events_profile_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_usage_request_events_profile_id ON ONLY public.usage_request_events USING btree (profile_id);


--
-- Name: ix_usage_request_events_proxy_api_key_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_usage_request_events_proxy_api_key_id ON ONLY public.usage_request_events USING btree (proxy_api_key_id);


--
-- Name: ix_user_agent_client_rules_profile_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_user_agent_client_rules_profile_id ON public.user_agent_client_rules USING btree (profile_id);


--
-- Name: ix_user_settings_profile_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX ix_user_settings_profile_id ON public.user_settings USING btree (profile_id);


--
-- Name: ix_webauthn_challenges_challenge_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX ix_webauthn_challenges_challenge_key ON public.webauthn_challenges USING btree (challenge_key);


--
-- Name: uq_hbr_system_match_pattern; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_hbr_system_match_pattern ON public.header_blocklist_rules USING btree (match_type, pattern) WHERE (is_system = true);


--
-- Name: uq_profiles_single_active; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_profiles_single_active ON public.profiles USING btree (is_active) WHERE (is_active = true);


--
-- Name: uq_profiles_single_default; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_profiles_single_default ON public.profiles USING btree (is_default) WHERE (is_default = true);


--
-- Name: uq_sidecar_instances_live_base_url_canonical; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_sidecar_instances_live_base_url_canonical ON public.sidecar_instances USING btree (base_url_canonical) WHERE (deleted_at IS NULL);


--
-- Name: uq_sidecar_instances_live_name; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_sidecar_instances_live_name ON public.sidecar_instances USING btree (lower(name)) WHERE (deleted_at IS NULL);


--
-- Name: uq_uacr_system_pattern; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX uq_uacr_system_pattern ON public.user_agent_client_rules USING btree (pattern) WHERE (is_system = true);


--
-- Name: audit_logs audit_logs_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.audit_logs
    ADD CONSTRAINT audit_logs_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE RESTRICT;


--
-- Name: connections connections_endpoint_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.connections
    ADD CONSTRAINT connections_endpoint_id_fkey FOREIGN KEY (endpoint_id) REFERENCES public.endpoints(id) ON DELETE RESTRICT;


--
-- Name: connections connections_pricing_template_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.connections
    ADD CONSTRAINT connections_pricing_template_id_fkey FOREIGN KEY (pricing_template_id) REFERENCES public.pricing_templates(id) ON DELETE RESTRICT;


--
-- Name: connections connections_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.connections
    ADD CONSTRAINT connections_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE CASCADE;


--
-- Name: endpoint_fx_rate_settings endpoint_fx_rate_settings_endpoint_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.endpoint_fx_rate_settings
    ADD CONSTRAINT endpoint_fx_rate_settings_endpoint_id_fkey FOREIGN KEY (endpoint_id) REFERENCES public.endpoints(id) ON DELETE CASCADE;


--
-- Name: endpoint_fx_rate_settings endpoint_fx_rate_settings_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.endpoint_fx_rate_settings
    ADD CONSTRAINT endpoint_fx_rate_settings_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE CASCADE;


--
-- Name: endpoints endpoints_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.endpoints
    ADD CONSTRAINT endpoints_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE CASCADE;


--
-- Name: audit_logs fk_audit_logs_vendor_id_set_null; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.audit_logs
    ADD CONSTRAINT fk_audit_logs_vendor_id_set_null FOREIGN KEY (vendor_id) REFERENCES public.vendors(id) ON DELETE SET NULL;


--
-- Name: loadbalance_events fk_loadbalance_events_vendor_id_set_null; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.loadbalance_events
    ADD CONSTRAINT fk_loadbalance_events_vendor_id_set_null FOREIGN KEY (vendor_id) REFERENCES public.vendors(id) ON DELETE SET NULL;


--
-- Name: model_configs fk_model_configs_profile_loadbalance_strategy; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_configs
    ADD CONSTRAINT fk_model_configs_profile_loadbalance_strategy FOREIGN KEY (profile_id, loadbalance_strategy_id) REFERENCES public.loadbalance_strategies(profile_id, id) ON DELETE RESTRICT;


--
-- Name: model_configs fk_model_configs_vendor_id_set_null; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_configs
    ADD CONSTRAINT fk_model_configs_vendor_id_set_null FOREIGN KEY (vendor_id) REFERENCES public.vendors(id) ON DELETE SET NULL;


--
-- Name: header_blocklist_rules header_blocklist_rules_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.header_blocklist_rules
    ADD CONSTRAINT header_blocklist_rules_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE CASCADE;


--
-- Name: loadbalance_events loadbalance_events_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.loadbalance_events
    ADD CONSTRAINT loadbalance_events_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE RESTRICT;


--
-- Name: loadbalance_round_robin_state loadbalance_round_robin_state_model_config_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.loadbalance_round_robin_state
    ADD CONSTRAINT loadbalance_round_robin_state_model_config_id_fkey FOREIGN KEY (model_config_id) REFERENCES public.model_configs(id) ON DELETE CASCADE;


--
-- Name: loadbalance_strategies loadbalance_strategies_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.loadbalance_strategies
    ADD CONSTRAINT loadbalance_strategies_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE CASCADE;


--
-- Name: management_job_events management_job_events_job_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.management_job_events
    ADD CONSTRAINT management_job_events_job_id_fkey FOREIGN KEY (job_id) REFERENCES public.management_jobs(id) ON DELETE CASCADE;


--
-- Name: model_configs model_configs_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_configs
    ADD CONSTRAINT model_configs_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE CASCADE;


--
-- Name: model_access_targets model_access_targets_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_access_targets
    ADD CONSTRAINT model_access_targets_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE CASCADE;


--
-- Name: model_access_targets model_access_targets_source_model_profile_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_access_targets
    ADD CONSTRAINT model_access_targets_source_model_profile_fkey FOREIGN KEY (source_model_config_id, profile_id) REFERENCES public.model_configs(id, profile_id) ON DELETE CASCADE;


--
-- Name: model_access_targets model_access_targets_target_connection_profile_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_access_targets
    ADD CONSTRAINT model_access_targets_target_connection_profile_fkey FOREIGN KEY (target_connection_id, profile_id) REFERENCES public.connections(id, profile_id) ON DELETE RESTRICT;


--
-- Name: model_access_targets model_access_targets_target_model_profile_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.model_access_targets
    ADD CONSTRAINT model_access_targets_target_model_profile_fkey FOREIGN KEY (target_model_config_id, profile_id) REFERENCES public.model_configs(id, profile_id) ON DELETE RESTRICT;


--
-- Name: password_reset_challenges password_reset_challenges_auth_subject_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.password_reset_challenges
    ADD CONSTRAINT password_reset_challenges_auth_subject_id_fkey FOREIGN KEY (auth_subject_id) REFERENCES public.app_auth_settings(id) ON DELETE CASCADE;


--
-- Name: pricing_templates pricing_templates_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.pricing_templates
    ADD CONSTRAINT pricing_templates_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE CASCADE;


--
-- Name: proxy_api_keys proxy_api_keys_created_by_auth_subject_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.proxy_api_keys
    ADD CONSTRAINT proxy_api_keys_created_by_auth_subject_id_fkey FOREIGN KEY (created_by_auth_subject_id) REFERENCES public.app_auth_settings(id) ON DELETE SET NULL;


--
-- Name: proxy_api_keys proxy_api_keys_rotated_from_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.proxy_api_keys
    ADD CONSTRAINT proxy_api_keys_rotated_from_id_fkey FOREIGN KEY (rotated_from_id) REFERENCES public.proxy_api_keys(id) ON DELETE SET NULL;


--
-- Name: refresh_tokens refresh_tokens_auth_subject_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.refresh_tokens
    ADD CONSTRAINT refresh_tokens_auth_subject_id_fkey FOREIGN KEY (auth_subject_id) REFERENCES public.app_auth_settings(id) ON DELETE CASCADE;


--
-- Name: refresh_tokens refresh_tokens_rotated_from_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.refresh_tokens
    ADD CONSTRAINT refresh_tokens_rotated_from_id_fkey FOREIGN KEY (rotated_from_id) REFERENCES public.refresh_tokens(id) ON DELETE SET NULL;


--
-- Name: request_logs request_logs_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.request_logs
    ADD CONSTRAINT request_logs_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE RESTRICT;


--
-- Name: request_logs request_logs_proxy_api_key_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.request_logs
    ADD CONSTRAINT request_logs_proxy_api_key_id_fkey FOREIGN KEY (proxy_api_key_id) REFERENCES public.proxy_api_keys(id) ON DELETE SET NULL;


--
-- Name: routing_connection_runtime_leases routing_connection_runtime_leases_connection_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.routing_connection_runtime_leases
    ADD CONSTRAINT routing_connection_runtime_leases_connection_id_fkey FOREIGN KEY (connection_id) REFERENCES public.connections(id) ON DELETE CASCADE;


--
-- Name: routing_connection_runtime_leases routing_connection_runtime_leases_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.routing_connection_runtime_leases
    ADD CONSTRAINT routing_connection_runtime_leases_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE CASCADE;


--
-- Name: routing_connection_runtime_state routing_connection_runtime_state_connection_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.routing_connection_runtime_state
    ADD CONSTRAINT routing_connection_runtime_state_connection_id_fkey FOREIGN KEY (connection_id) REFERENCES public.connections(id) ON DELETE CASCADE;


--
-- Name: routing_connection_runtime_state routing_connection_runtime_state_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.routing_connection_runtime_state
    ADD CONSTRAINT routing_connection_runtime_state_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE CASCADE;


--
-- Name: runtime_telemetry_outbox runtime_telemetry_outbox_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.runtime_telemetry_outbox
    ADD CONSTRAINT runtime_telemetry_outbox_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE RESTRICT;


--
-- Name: sidecar_provider_snapshots sidecar_provider_snapshots_sidecar_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.sidecar_provider_snapshots
    ADD CONSTRAINT sidecar_provider_snapshots_sidecar_id_fkey FOREIGN KEY (sidecar_id) REFERENCES public.sidecar_instances(id) ON DELETE CASCADE;


--
-- Name: usage_request_events usage_request_events_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.usage_request_events
    ADD CONSTRAINT usage_request_events_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE RESTRICT;


--
-- Name: usage_request_events usage_request_events_proxy_api_key_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE public.usage_request_events
    ADD CONSTRAINT usage_request_events_proxy_api_key_id_fkey FOREIGN KEY (proxy_api_key_id) REFERENCES public.proxy_api_keys(id) ON DELETE SET NULL;


--
-- Name: user_agent_client_rules user_agent_client_rules_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_agent_client_rules
    ADD CONSTRAINT user_agent_client_rules_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE CASCADE;


--
-- Name: user_settings user_settings_profile_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_settings
    ADD CONSTRAINT user_settings_profile_id_fkey FOREIGN KEY (profile_id) REFERENCES public.profiles(id) ON DELETE CASCADE;


--
-- Name: webauthn_credentials webauthn_credentials_auth_subject_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.webauthn_credentials
    ADD CONSTRAINT webauthn_credentials_auth_subject_id_fkey FOREIGN KEY (auth_subject_id) REFERENCES public.app_auth_settings(id) ON DELETE CASCADE;


-- Migration-owned singleton/bootstrap rows.
INSERT INTO public.runtime_cache_generations (domain, scope_type, scope_id, version, reason)
VALUES
    ('auth', 'global', '*', 0, 'bootstrap'),
    ('runtime_planning', 'global', '*', 0, 'bootstrap'),
    ('profile_runtime', 'global', '*', 0, 'bootstrap'),
    ('model_catalog', 'global', '*', 0, 'bootstrap')
ON CONFLICT (domain, scope_type, scope_id) DO NOTHING;

INSERT INTO public.log_retention_settings (singleton_key)
VALUES ('global')
ON CONFLICT (singleton_key) DO NOTHING;
