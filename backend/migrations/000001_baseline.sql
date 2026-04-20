-- Go baseline migration derived from the live Alembic cutover schema.

CREATE TABLE "public"."app_auth_settings" (
    "id" SERIAL NOT NULL,
    "singleton_key" character varying(20) NOT NULL,
    "auth_enabled" boolean NOT NULL,
    "username" character varying(200),
    "email" character varying(320),
    "pending_email" character varying(320),
    "password_hash" text,
    "email_bound_at" timestamp with time zone,
    "email_verification_code_hash" character varying(64),
    "email_verification_expires_at" timestamp with time zone,
    "email_verification_attempt_count" integer NOT NULL,
    "must_change_password" boolean NOT NULL,
    "last_login_at" timestamp with time zone,
    "token_version" integer NOT NULL,
    "created_at" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL
);

CREATE TABLE "public"."audit_logs" (
    "id" SERIAL NOT NULL,
    "profile_id" integer NOT NULL,
    "request_log_id" integer,
    "vendor_id" integer,
    "model_id" character varying(200) NOT NULL,
    "endpoint_id" integer,
    "connection_id" integer,
    "endpoint_base_url" character varying(500),
    "endpoint_description" text,
    "request_method" character varying(10) NOT NULL,
    "request_url" character varying(2000) NOT NULL,
    "request_headers" text NOT NULL,
    "request_body" text,
    "response_status" integer NOT NULL,
    "response_headers" text,
    "response_body" text,
    "is_stream" boolean NOT NULL,
    "duration_ms" integer NOT NULL,
    "created_at" timestamp with time zone NOT NULL
);

CREATE TABLE "public"."connections" (
    "id" SERIAL NOT NULL,
    "profile_id" integer NOT NULL,
    "model_config_id" integer NOT NULL,
    "endpoint_id" integer NOT NULL,
    "pricing_template_id" integer,
    "qps_limit" integer,
    "max_in_flight_non_stream" integer,
    "max_in_flight_stream" integer,
    "is_active" boolean NOT NULL,
    "priority" integer NOT NULL,
    "name" text,
    "auth_type" character varying(50),
    "custom_headers" text,
    "health_status" character varying(20) NOT NULL,
    "health_detail" text,
    "last_health_check" timestamp with time zone,
    "created_at" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL,
    "openai_probe_endpoint_variant" character varying(40),
    "monitoring_probe_interval_seconds" integer DEFAULT 300 NOT NULL
);

CREATE TABLE "public"."endpoint_fx_rate_settings" (
    "id" SERIAL NOT NULL,
    "profile_id" integer NOT NULL,
    "model_id" character varying(200) NOT NULL,
    "endpoint_id" integer NOT NULL,
    "fx_rate" character varying(20) NOT NULL,
    "created_at" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL
);

CREATE TABLE "public"."endpoints" (
    "id" SERIAL NOT NULL,
    "profile_id" integer NOT NULL,
    "name" character varying(200) NOT NULL,
    "base_url" character varying(500) NOT NULL,
    "api_key" character varying(500) NOT NULL,
    "position" integer NOT NULL,
    "created_at" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL
);

CREATE TABLE "public"."header_blocklist_rules" (
    "id" SERIAL NOT NULL,
    "profile_id" integer,
    "name" character varying(200) NOT NULL,
    "match_type" character varying(20) NOT NULL,
    "pattern" character varying(200) NOT NULL,
    "enabled" boolean NOT NULL,
    "is_system" boolean NOT NULL,
    "created_at" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL
);

CREATE TABLE "public"."loadbalance_events" (
    "id" BIGSERIAL NOT NULL,
    "profile_id" integer NOT NULL,
    "connection_id" integer NOT NULL,
    "event_type" character varying(20) NOT NULL,
    "failure_kind" character varying(20),
    "consecutive_failures" integer NOT NULL,
    "cooldown_seconds" numeric(10,2) NOT NULL,
    "blocked_until_mono" numeric(20,6),
    "model_id" character varying(200),
    "endpoint_id" integer,
    "vendor_id" integer,
    "failure_threshold" integer,
    "backoff_multiplier" numeric(5,2),
    "max_cooldown_seconds" integer,
    "max_cooldown_strikes" integer,
    "ban_mode" character varying(20),
    "banned_until_at" timestamp with time zone,
    "created_at" timestamp with time zone NOT NULL
);

CREATE TABLE "public"."loadbalance_round_robin_state" (
    "id" SERIAL NOT NULL,
    "profile_id" integer NOT NULL,
    "model_config_id" integer NOT NULL,
    "next_cursor" integer NOT NULL,
    "created_at" timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    "updated_at" timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);

CREATE TABLE "public"."loadbalance_strategies" (
    "id" SERIAL NOT NULL,
    "profile_id" integer NOT NULL,
    "name" character varying(200) NOT NULL,
    "created_at" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL,
    "routing_policy" jsonb,
    "strategy_type" character varying(20) NOT NULL,
    "legacy_strategy_type" character varying(20),
    "auto_recovery" jsonb
);

CREATE TABLE "public"."model_configs" (
    "id" SERIAL NOT NULL,
    "profile_id" integer NOT NULL,
    "vendor_id" integer,
    "api_family" character varying(50) NOT NULL,
    "model_id" character varying(200) NOT NULL,
    "display_name" character varying(200),
    "model_type" character varying(20) NOT NULL,
    "loadbalance_strategy_id" integer,
    "is_enabled" boolean NOT NULL,
    "created_at" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL
);

CREATE TABLE "public"."model_proxy_targets" (
    "id" SERIAL NOT NULL,
    "source_model_config_id" integer NOT NULL,
    "target_model_config_id" integer NOT NULL,
    "position" integer NOT NULL
);

CREATE TABLE "public"."password_reset_challenges" (
    "id" SERIAL NOT NULL,
    "auth_subject_id" integer NOT NULL,
    "otp_hash" character varying(64) NOT NULL,
    "expires_at" timestamp with time zone NOT NULL,
    "consumed_at" timestamp with time zone,
    "attempt_count" integer NOT NULL,
    "requested_ip" character varying(100),
    "created_at" timestamp with time zone NOT NULL
);

CREATE TABLE "public"."pricing_templates" (
    "id" SERIAL NOT NULL,
    "profile_id" integer NOT NULL,
    "name" character varying(200) NOT NULL,
    "description" text,
    "pricing_unit" character varying(20) NOT NULL,
    "pricing_currency_code" character varying(3) NOT NULL,
    "input_price" character varying(20) NOT NULL,
    "output_price" character varying(20) NOT NULL,
    "cached_input_price" character varying(20),
    "cache_creation_price" character varying(20),
    "reasoning_price" character varying(20),
    "version" integer NOT NULL,
    "created_at" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL
);

CREATE TABLE "public"."profiles" (
    "id" SERIAL NOT NULL,
    "name" character varying(200) NOT NULL,
    "description" text,
    "is_active" boolean NOT NULL,
    "is_default" boolean NOT NULL,
    "is_editable" boolean NOT NULL,
    "version" integer NOT NULL,
    "deleted_at" timestamp with time zone,
    "created_at" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL
);

CREATE TABLE "public"."proxy_api_keys" (
    "id" SERIAL NOT NULL,
    "name" character varying(200) NOT NULL,
    "key_prefix" character varying(200) NOT NULL,
    "key_hash" character varying(64) NOT NULL,
    "last_four" character varying(4) NOT NULL,
    "is_active" boolean NOT NULL,
    "expires_at" timestamp with time zone,
    "last_used_at" timestamp with time zone,
    "last_used_ip" character varying(100),
    "created_by_auth_subject_id" integer,
    "notes" text,
    "rotated_from_id" integer,
    "created_at" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL
);

CREATE TABLE "public"."refresh_tokens" (
    "id" SERIAL NOT NULL,
    "auth_subject_id" integer NOT NULL,
    "token_hash" character varying(64) NOT NULL,
    "session_duration" character varying(20) NOT NULL,
    "expires_at" timestamp with time zone NOT NULL,
    "rotated_from_id" integer,
    "revoked_at" timestamp with time zone,
    "last_used_at" timestamp with time zone,
    "user_agent" text,
    "ip_address" character varying(100),
    "created_at" timestamp with time zone NOT NULL
);

CREATE TABLE "public"."request_logs" (
    "id" SERIAL NOT NULL,
    "profile_id" integer NOT NULL,
    "model_id" character varying(200) NOT NULL,
    "api_family" character varying(50) NOT NULL,
    "vendor_id" integer,
    "vendor_key" character varying(100),
    "vendor_name" character varying(100),
    "resolved_target_model_id" character varying(200),
    "endpoint_id" integer,
    "connection_id" integer,
    "proxy_api_key_id" integer,
    "proxy_api_key_name_snapshot" character varying(200),
    "ingress_request_id" character varying(36),
    "attempt_number" integer,
    "provider_correlation_id" character varying(255),
    "endpoint_base_url" character varying(500),
    "status_code" integer NOT NULL,
    "response_time_ms" integer NOT NULL,
    "is_stream" boolean NOT NULL,
    "input_tokens" integer,
    "output_tokens" integer,
    "total_tokens" integer,
    "success_flag" boolean,
    "billable_flag" boolean,
    "priced_flag" boolean,
    "unpriced_reason" character varying(50),
    "reasoning_tokens" integer,
    "input_cost_micros" bigint,
    "output_cost_micros" bigint,
    "reasoning_cost_micros" bigint,
    "total_cost_original_micros" bigint,
    "total_cost_user_currency_micros" bigint,
    "currency_code_original" character varying(3),
    "report_currency_code" character varying(3),
    "report_currency_symbol" character varying(5),
    "fx_rate_used" character varying(20),
    "fx_rate_source" character varying(30),
    "pricing_snapshot_unit" character varying(10),
    "pricing_snapshot_input" character varying(20),
    "pricing_snapshot_output" character varying(20),
    "pricing_snapshot_reasoning" character varying(20),
    "cache_read_input_tokens" integer,
    "cache_creation_input_tokens" integer,
    "cache_read_input_cost_micros" bigint,
    "cache_creation_input_cost_micros" bigint,
    "pricing_snapshot_cache_read_input" character varying(20),
    "pricing_snapshot_cache_creation_input" character varying(20),
    "pricing_config_version_used" integer,
    "request_path" character varying(500) NOT NULL,
    "error_detail" text,
    "endpoint_description" text,
    "created_at" timestamp with time zone NOT NULL,
    "caller_user_agent" text,
    "upstream_user_agent" text,
    "completion_duration_ms" integer,
    "ttft_ms" integer,
    "audit_enabled_at_request" boolean DEFAULT false NOT NULL
);

CREATE UNLOGGED TABLE "public"."routing_connection_runtime_leases" (
    "lease_token" character varying(64) NOT NULL,
    "profile_id" integer NOT NULL,
    "connection_id" integer NOT NULL,
    "lease_kind" character varying(20) NOT NULL,
    "expires_at" timestamp with time zone NOT NULL,
    "heartbeat_at" timestamp with time zone,
    "created_at" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL
);

CREATE UNLOGGED TABLE "public"."routing_connection_runtime_state" (
    "id" SERIAL NOT NULL,
    "profile_id" integer NOT NULL,
    "connection_id" integer NOT NULL,
    "window_started_at" timestamp with time zone,
    "window_request_count" integer NOT NULL,
    "in_flight_non_stream" integer NOT NULL,
    "in_flight_stream" integer NOT NULL,
    "consecutive_failures" integer NOT NULL,
    "last_failure_kind" character varying(20),
    "last_cooldown_seconds" numeric(10,2) NOT NULL,
    "max_cooldown_strikes" integer NOT NULL,
    "ban_mode" character varying(20) NOT NULL,
    "banned_until_at" timestamp with time zone,
    "open_until_at" timestamp with time zone,
    "probe_eligible_logged" boolean NOT NULL,
    "circuit_state" character varying(20) NOT NULL,
    "probe_available_at" timestamp with time zone,
    "live_p95_latency_ms" integer,
    "last_live_failure_kind" character varying(50),
    "last_live_failure_at" timestamp with time zone,
    "last_live_success_at" timestamp with time zone,
    "created_at" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL
);

CREATE TABLE "public"."usage_request_events" (
    "id" SERIAL NOT NULL,
    "profile_id" integer NOT NULL,
    "ingress_request_id" character varying(36) NOT NULL,
    "model_id" character varying(200) NOT NULL,
    "resolved_target_model_id" character varying(200),
    "api_family" character varying(50) NOT NULL,
    "endpoint_id" integer,
    "connection_id" integer,
    "proxy_api_key_id" integer,
    "proxy_api_key_name_snapshot" character varying(200),
    "status_code" integer NOT NULL,
    "success_flag" boolean NOT NULL,
    "input_tokens" integer,
    "output_tokens" integer,
    "total_tokens" integer,
    "cache_read_input_tokens" integer,
    "cache_creation_input_tokens" integer,
    "reasoning_tokens" integer,
    "input_cost_micros" bigint,
    "output_cost_micros" bigint,
    "cache_read_input_cost_micros" bigint,
    "cache_creation_input_cost_micros" bigint,
    "reasoning_cost_micros" bigint,
    "total_cost_original_micros" bigint,
    "total_cost_user_currency_micros" bigint,
    "currency_code_original" character varying(3),
    "report_currency_code" character varying(3),
    "report_currency_symbol" character varying(5),
    "fx_rate_used" character varying(20),
    "fx_rate_source" character varying(30),
    "pricing_snapshot_unit" character varying(10),
    "pricing_snapshot_input" character varying(20),
    "pricing_snapshot_output" character varying(20),
    "pricing_snapshot_cache_read_input" character varying(20),
    "pricing_snapshot_cache_creation_input" character varying(20),
    "pricing_snapshot_reasoning" character varying(20),
    "pricing_config_version_used" integer,
    "attempt_count" integer NOT NULL,
    "request_path" character varying(500) NOT NULL,
    "created_at" timestamp with time zone NOT NULL,
    "response_time_ms" integer,
    "completion_duration_ms" integer,
    "ttft_ms" integer,
    "billable_flag" boolean,
    "priced_flag" boolean,
    "unpriced_reason" character varying(50)
);

CREATE TABLE "public"."user_agent_client_rules" (
    "id" SERIAL NOT NULL,
    "profile_id" integer,
    "name" character varying(200) NOT NULL,
    "pattern" text NOT NULL,
    "enabled" boolean NOT NULL,
    "is_system" boolean NOT NULL,
    "created_at" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL
);

CREATE TABLE "public"."user_settings" (
    "id" SERIAL NOT NULL,
    "profile_id" integer NOT NULL,
    "report_currency_code" character varying(3) NOT NULL,
    "report_currency_symbol" character varying(5) NOT NULL,
    "timezone_preference" character varying(100),
    "created_at" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL
);

CREATE TABLE "public"."vendors" (
    "id" SERIAL NOT NULL,
    "key" character varying(100) NOT NULL,
    "name" character varying(100) NOT NULL,
    "description" text,
    "icon_key" character varying(100),
    "audit_enabled" boolean NOT NULL,
    "audit_capture_bodies" boolean NOT NULL,
    "created_at" timestamp with time zone NOT NULL,
    "updated_at" timestamp with time zone NOT NULL
);

CREATE TABLE "public"."webauthn_challenges" (
    "id" SERIAL NOT NULL,
    "challenge_key" character varying(100) NOT NULL,
    "challenge" bytea NOT NULL,
    "expires_at" timestamp with time zone NOT NULL,
    "created_at" timestamp with time zone NOT NULL
);

CREATE TABLE "public"."webauthn_credentials" (
    "id" SERIAL NOT NULL,
    "auth_subject_id" integer NOT NULL,
    "credential_id" bytea NOT NULL,
    "public_key" bytea NOT NULL,
    "sign_count" bigint DEFAULT '0'::bigint NOT NULL,
    "device_name" character varying(200),
    "aaguid" bytea,
    "transports" text[],
    "backup_eligible" boolean DEFAULT false,
    "backup_state" boolean DEFAULT false,
    "last_used_at" timestamp with time zone,
    "last_used_ip" character varying(45),
    "created_at" timestamp with time zone DEFAULT now() NOT NULL,
    "updated_at" timestamp with time zone DEFAULT now() NOT NULL
);
ALTER TABLE ONLY "public"."app_auth_settings" ADD CONSTRAINT "app_auth_settings_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."app_auth_settings" ADD CONSTRAINT "uq_app_auth_settings_singleton_key" UNIQUE (singleton_key);
ALTER TABLE ONLY "public"."audit_logs" ADD CONSTRAINT "audit_logs_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."connections" ADD CONSTRAINT "ck_connections_openai_probe_endpoint_variant" CHECK (openai_probe_endpoint_variant IS NULL OR (openai_probe_endpoint_variant::text = ANY (ARRAY['responses_minimal'::character varying, 'responses_reasoning_none'::character varying, 'chat_completions_minimal'::character varying, 'chat_completions_reasoning_none'::character varying])));
ALTER TABLE ONLY "public"."connections" ADD CONSTRAINT "connections_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."endpoint_fx_rate_settings" ADD CONSTRAINT "endpoint_fx_rate_settings_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."endpoint_fx_rate_settings" ADD CONSTRAINT "uq_fx_profile_model_endpoint" UNIQUE (profile_id, model_id, endpoint_id);
ALTER TABLE ONLY "public"."endpoints" ADD CONSTRAINT "endpoints_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."endpoints" ADD CONSTRAINT "uq_endpoints_profile_name" UNIQUE (profile_id, name);
ALTER TABLE ONLY "public"."header_blocklist_rules" ADD CONSTRAINT "ck_hbr_profile_scope" CHECK (is_system = true AND profile_id IS NULL OR is_system = false AND profile_id IS NOT NULL);
ALTER TABLE ONLY "public"."header_blocklist_rules" ADD CONSTRAINT "header_blocklist_rules_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."header_blocklist_rules" ADD CONSTRAINT "uq_hbr_profile_match_pattern" UNIQUE (profile_id, match_type, pattern);
ALTER TABLE ONLY "public"."loadbalance_events" ADD CONSTRAINT "chk_event_type" CHECK (event_type::text = ANY (ARRAY['opened'::character varying, 'extended'::character varying, 'max_cooldown_strike'::character varying, 'banned'::character varying, 'probe_eligible'::character varying, 'recovered'::character varying, 'not_opened'::character varying]));
ALTER TABLE ONLY "public"."loadbalance_events" ADD CONSTRAINT "chk_failure_kind" CHECK ((failure_kind::text = ANY (ARRAY['transient_http'::character varying, 'connect_error'::character varying, 'timeout'::character varying])) OR failure_kind IS NULL);
ALTER TABLE ONLY "public"."loadbalance_events" ADD CONSTRAINT "chk_loadbalance_events_ban_mode" CHECK ((ban_mode::text = ANY (ARRAY['off'::character varying, 'temporary'::character varying, 'manual'::character varying])) OR ban_mode IS NULL);
ALTER TABLE ONLY "public"."loadbalance_events" ADD CONSTRAINT "loadbalance_events_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."loadbalance_round_robin_state" ADD CONSTRAINT "ck_loadbalance_round_robin_state_next_cursor_nonnegative" CHECK (next_cursor >= 0);
ALTER TABLE ONLY "public"."loadbalance_round_robin_state" ADD CONSTRAINT "loadbalance_round_robin_state_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."loadbalance_round_robin_state" ADD CONSTRAINT "uq_loadbalance_round_robin_state_profile_model" UNIQUE (profile_id, model_config_id);
ALTER TABLE ONLY "public"."loadbalance_strategies" ADD CONSTRAINT "chk_loadbalance_strategies_legacy_strategy_type" CHECK ((legacy_strategy_type::text = ANY (ARRAY['single'::character varying, 'fill-first'::character varying, 'round-robin'::character varying])) OR legacy_strategy_type IS NULL);
ALTER TABLE ONLY "public"."loadbalance_strategies" ADD CONSTRAINT "chk_loadbalance_strategies_shape" CHECK (strategy_type::text = 'legacy'::text AND legacy_strategy_type IS NOT NULL AND auto_recovery IS NOT NULL AND routing_policy IS NULL OR strategy_type::text = 'adaptive'::text AND legacy_strategy_type IS NULL AND auto_recovery IS NULL AND routing_policy IS NOT NULL);
ALTER TABLE ONLY "public"."loadbalance_strategies" ADD CONSTRAINT "chk_loadbalance_strategies_type" CHECK (strategy_type::text = ANY (ARRAY['legacy'::character varying, 'adaptive'::character varying]));
ALTER TABLE ONLY "public"."loadbalance_strategies" ADD CONSTRAINT "loadbalance_strategies_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."loadbalance_strategies" ADD CONSTRAINT "uq_loadbalance_strategies_profile_id_id" UNIQUE (profile_id, id);
ALTER TABLE ONLY "public"."loadbalance_strategies" ADD CONSTRAINT "uq_loadbalance_strategies_profile_name" UNIQUE (profile_id, name);
ALTER TABLE ONLY "public"."model_configs" ADD CONSTRAINT "chk_model_configs_strategy_attachment" CHECK (model_type::text = 'native'::text AND loadbalance_strategy_id IS NOT NULL OR model_type::text = 'proxy'::text AND loadbalance_strategy_id IS NULL);
ALTER TABLE ONLY "public"."model_configs" ADD CONSTRAINT "model_configs_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."model_configs" ADD CONSTRAINT "uq_model_configs_profile_model_id" UNIQUE (profile_id, model_id);
ALTER TABLE ONLY "public"."model_proxy_targets" ADD CONSTRAINT "model_proxy_targets_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."model_proxy_targets" ADD CONSTRAINT "uq_model_proxy_targets_source_position" UNIQUE (source_model_config_id, "position");
ALTER TABLE ONLY "public"."model_proxy_targets" ADD CONSTRAINT "uq_model_proxy_targets_source_target" UNIQUE (source_model_config_id, target_model_config_id);
ALTER TABLE ONLY "public"."password_reset_challenges" ADD CONSTRAINT "password_reset_challenges_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."pricing_templates" ADD CONSTRAINT "pricing_templates_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."pricing_templates" ADD CONSTRAINT "uq_pricing_templates_profile_name" UNIQUE (profile_id, name);
ALTER TABLE ONLY "public"."profiles" ADD CONSTRAINT "profiles_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."profiles" ADD CONSTRAINT "profiles_name_key" UNIQUE (name);
ALTER TABLE ONLY "public"."proxy_api_keys" ADD CONSTRAINT "proxy_api_keys_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."proxy_api_keys" ADD CONSTRAINT "uq_proxy_api_keys_prefix" UNIQUE (key_prefix);
ALTER TABLE ONLY "public"."refresh_tokens" ADD CONSTRAINT "refresh_tokens_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."refresh_tokens" ADD CONSTRAINT "refresh_tokens_token_hash_key" UNIQUE (token_hash);
ALTER TABLE ONLY "public"."request_logs" ADD CONSTRAINT "request_logs_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."routing_connection_runtime_leases" ADD CONSTRAINT "ck_routing_connection_runtime_leases_kind" CHECK (lease_kind::text = ANY (ARRAY['stream'::character varying, 'non_stream'::character varying, 'half_open_probe'::character varying]));
ALTER TABLE ONLY "public"."routing_connection_runtime_leases" ADD CONSTRAINT "routing_connection_runtime_leases_pkey" PRIMARY KEY (lease_token);
ALTER TABLE ONLY "public"."routing_connection_runtime_state" ADD CONSTRAINT "ck_rt_state_ban_mode" CHECK (ban_mode::text = ANY (ARRAY['off'::character varying, 'temporary'::character varying, 'manual'::character varying]));
ALTER TABLE ONLY "public"."routing_connection_runtime_state" ADD CONSTRAINT "ck_rt_state_circuit_state" CHECK (circuit_state::text = ANY (ARRAY['closed'::character varying, 'open'::character varying, 'half_open'::character varying]));
ALTER TABLE ONLY "public"."routing_connection_runtime_state" ADD CONSTRAINT "ck_rt_state_last_failure_kind" CHECK ((last_failure_kind::text = ANY (ARRAY['transient_http'::character varying, 'connect_error'::character varying, 'timeout'::character varying])) OR last_failure_kind IS NULL);
ALTER TABLE ONLY "public"."routing_connection_runtime_state" ADD CONSTRAINT "ck_rt_state_max_strikes_nonneg" CHECK (max_cooldown_strikes >= 0);
ALTER TABLE ONLY "public"."routing_connection_runtime_state" ADD CONSTRAINT "ck_rt_state_non_stream_nonneg" CHECK (in_flight_non_stream >= 0);
ALTER TABLE ONLY "public"."routing_connection_runtime_state" ADD CONSTRAINT "ck_rt_state_stream_nonneg" CHECK (in_flight_stream >= 0);
ALTER TABLE ONLY "public"."routing_connection_runtime_state" ADD CONSTRAINT "ck_rt_state_window_count_nonneg" CHECK (window_request_count >= 0);
ALTER TABLE ONLY "public"."routing_connection_runtime_state" ADD CONSTRAINT "routing_connection_runtime_state_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."routing_connection_runtime_state" ADD CONSTRAINT "uq_routing_connection_runtime_state_profile_connection" UNIQUE (profile_id, connection_id);
ALTER TABLE ONLY "public"."usage_request_events" ADD CONSTRAINT "ck_usage_request_events_attempt_count_positive" CHECK (attempt_count >= 1);
ALTER TABLE ONLY "public"."usage_request_events" ADD CONSTRAINT "usage_request_events_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."usage_request_events" ADD CONSTRAINT "uq_usage_request_events_profile_ingress_request" UNIQUE (profile_id, ingress_request_id);
ALTER TABLE ONLY "public"."user_agent_client_rules" ADD CONSTRAINT "ck_uacr_profile_scope" CHECK (is_system = true AND profile_id IS NULL OR is_system = false AND profile_id IS NOT NULL);
ALTER TABLE ONLY "public"."user_agent_client_rules" ADD CONSTRAINT "user_agent_client_rules_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."user_settings" ADD CONSTRAINT "user_settings_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."user_settings" ADD CONSTRAINT "uq_user_settings_profile_id" UNIQUE (profile_id);
ALTER TABLE ONLY "public"."vendors" ADD CONSTRAINT "vendors_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."vendors" ADD CONSTRAINT "vendors_key_key" UNIQUE (key);
ALTER TABLE ONLY "public"."vendors" ADD CONSTRAINT "vendors_name_key" UNIQUE (name);
ALTER TABLE ONLY "public"."webauthn_challenges" ADD CONSTRAINT "webauthn_challenges_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."webauthn_credentials" ADD CONSTRAINT "webauthn_credentials_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."webauthn_credentials" ADD CONSTRAINT "uq_credential_id" UNIQUE (credential_id);
ALTER TABLE ONLY "public"."audit_logs" ADD CONSTRAINT "audit_logs_profile_id_fkey" FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE RESTRICT;
ALTER TABLE ONLY "public"."audit_logs" ADD CONSTRAINT "audit_logs_request_log_id_fkey" FOREIGN KEY (request_log_id) REFERENCES request_logs(id) ON DELETE SET NULL;
ALTER TABLE ONLY "public"."audit_logs" ADD CONSTRAINT "fk_audit_logs_vendor_id_set_null" FOREIGN KEY (vendor_id) REFERENCES vendors(id) ON DELETE SET NULL;
ALTER TABLE ONLY "public"."connections" ADD CONSTRAINT "connections_endpoint_id_fkey" FOREIGN KEY (endpoint_id) REFERENCES endpoints(id) ON DELETE RESTRICT;
ALTER TABLE ONLY "public"."connections" ADD CONSTRAINT "connections_model_config_id_fkey" FOREIGN KEY (model_config_id) REFERENCES model_configs(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."connections" ADD CONSTRAINT "connections_pricing_template_id_fkey" FOREIGN KEY (pricing_template_id) REFERENCES pricing_templates(id) ON DELETE RESTRICT;
ALTER TABLE ONLY "public"."connections" ADD CONSTRAINT "connections_profile_id_fkey" FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."endpoint_fx_rate_settings" ADD CONSTRAINT "endpoint_fx_rate_settings_endpoint_id_fkey" FOREIGN KEY (endpoint_id) REFERENCES endpoints(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."endpoint_fx_rate_settings" ADD CONSTRAINT "endpoint_fx_rate_settings_profile_id_fkey" FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."endpoints" ADD CONSTRAINT "endpoints_profile_id_fkey" FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."header_blocklist_rules" ADD CONSTRAINT "header_blocklist_rules_profile_id_fkey" FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."loadbalance_events" ADD CONSTRAINT "fk_loadbalance_events_vendor_id_set_null" FOREIGN KEY (vendor_id) REFERENCES vendors(id) ON DELETE SET NULL;
ALTER TABLE ONLY "public"."loadbalance_events" ADD CONSTRAINT "loadbalance_events_profile_id_fkey" FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE RESTRICT;
ALTER TABLE ONLY "public"."loadbalance_round_robin_state" ADD CONSTRAINT "loadbalance_round_robin_state_model_config_id_fkey" FOREIGN KEY (model_config_id) REFERENCES model_configs(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."loadbalance_strategies" ADD CONSTRAINT "loadbalance_strategies_profile_id_fkey" FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."model_configs" ADD CONSTRAINT "fk_model_configs_profile_loadbalance_strategy" FOREIGN KEY (profile_id, loadbalance_strategy_id) REFERENCES loadbalance_strategies(profile_id, id) ON DELETE RESTRICT;
ALTER TABLE ONLY "public"."model_configs" ADD CONSTRAINT "fk_model_configs_vendor_id_set_null" FOREIGN KEY (vendor_id) REFERENCES vendors(id) ON DELETE SET NULL;
ALTER TABLE ONLY "public"."model_configs" ADD CONSTRAINT "model_configs_profile_id_fkey" FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."model_proxy_targets" ADD CONSTRAINT "model_proxy_targets_source_model_config_id_fkey" FOREIGN KEY (source_model_config_id) REFERENCES model_configs(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."model_proxy_targets" ADD CONSTRAINT "model_proxy_targets_target_model_config_id_fkey" FOREIGN KEY (target_model_config_id) REFERENCES model_configs(id) ON DELETE RESTRICT;
ALTER TABLE ONLY "public"."password_reset_challenges" ADD CONSTRAINT "password_reset_challenges_auth_subject_id_fkey" FOREIGN KEY (auth_subject_id) REFERENCES app_auth_settings(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."pricing_templates" ADD CONSTRAINT "pricing_templates_profile_id_fkey" FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."proxy_api_keys" ADD CONSTRAINT "proxy_api_keys_created_by_auth_subject_id_fkey" FOREIGN KEY (created_by_auth_subject_id) REFERENCES app_auth_settings(id) ON DELETE SET NULL;
ALTER TABLE ONLY "public"."proxy_api_keys" ADD CONSTRAINT "proxy_api_keys_rotated_from_id_fkey" FOREIGN KEY (rotated_from_id) REFERENCES proxy_api_keys(id) ON DELETE SET NULL;
ALTER TABLE ONLY "public"."refresh_tokens" ADD CONSTRAINT "refresh_tokens_auth_subject_id_fkey" FOREIGN KEY (auth_subject_id) REFERENCES app_auth_settings(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."refresh_tokens" ADD CONSTRAINT "refresh_tokens_rotated_from_id_fkey" FOREIGN KEY (rotated_from_id) REFERENCES refresh_tokens(id) ON DELETE SET NULL;
ALTER TABLE ONLY "public"."request_logs" ADD CONSTRAINT "request_logs_profile_id_fkey" FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE RESTRICT;
ALTER TABLE ONLY "public"."request_logs" ADD CONSTRAINT "request_logs_proxy_api_key_id_fkey" FOREIGN KEY (proxy_api_key_id) REFERENCES proxy_api_keys(id) ON DELETE SET NULL;
ALTER TABLE ONLY "public"."routing_connection_runtime_leases" ADD CONSTRAINT "routing_connection_runtime_leases_connection_id_fkey" FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."routing_connection_runtime_leases" ADD CONSTRAINT "routing_connection_runtime_leases_profile_id_fkey" FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."routing_connection_runtime_state" ADD CONSTRAINT "routing_connection_runtime_state_connection_id_fkey" FOREIGN KEY (connection_id) REFERENCES connections(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."routing_connection_runtime_state" ADD CONSTRAINT "routing_connection_runtime_state_profile_id_fkey" FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."usage_request_events" ADD CONSTRAINT "usage_request_events_profile_id_fkey" FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE RESTRICT;
ALTER TABLE ONLY "public"."usage_request_events" ADD CONSTRAINT "usage_request_events_proxy_api_key_id_fkey" FOREIGN KEY (proxy_api_key_id) REFERENCES proxy_api_keys(id) ON DELETE SET NULL;
ALTER TABLE ONLY "public"."user_agent_client_rules" ADD CONSTRAINT "user_agent_client_rules_profile_id_fkey" FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."user_settings" ADD CONSTRAINT "user_settings_profile_id_fkey" FOREIGN KEY (profile_id) REFERENCES profiles(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."webauthn_credentials" ADD CONSTRAINT "webauthn_credentials_auth_subject_id_fkey" FOREIGN KEY (auth_subject_id) REFERENCES app_auth_settings(id) ON DELETE CASCADE;

CREATE INDEX idx_audit_logs_connection_id ON audit_logs USING btree (connection_id);
CREATE INDEX idx_audit_logs_profile_created_at ON audit_logs USING btree (profile_id, created_at);
CREATE INDEX ix_audit_logs_connection_id ON audit_logs USING btree (connection_id);
CREATE INDEX ix_audit_logs_created_at ON audit_logs USING btree (created_at);
CREATE INDEX ix_audit_logs_endpoint_id ON audit_logs USING btree (endpoint_id);
CREATE INDEX ix_audit_logs_model_id ON audit_logs USING btree (model_id);
CREATE INDEX ix_audit_logs_profile_id ON audit_logs USING btree (profile_id);
CREATE UNIQUE INDEX ix_audit_logs_request_log_id ON audit_logs USING btree (request_log_id);
CREATE INDEX ix_audit_logs_response_status ON audit_logs USING btree (response_status);
CREATE INDEX ix_audit_logs_vendor_id ON audit_logs USING btree (vendor_id);
CREATE INDEX idx_connections_endpoint_id ON connections USING btree (endpoint_id);
CREATE INDEX idx_connections_is_active ON connections USING btree (is_active);
CREATE INDEX idx_connections_model_config_id ON connections USING btree (model_config_id);
CREATE INDEX idx_connections_pricing_template_id ON connections USING btree (pricing_template_id);
CREATE INDEX idx_connections_priority ON connections USING btree (priority);
CREATE INDEX idx_connections_profile_id ON connections USING btree (profile_id);
CREATE INDEX idx_connections_profile_model_active_priority ON connections USING btree (profile_id, model_config_id, is_active, priority);
CREATE INDEX ix_connections_profile_id ON connections USING btree (profile_id);
CREATE INDEX idx_fx_endpoint_id ON endpoint_fx_rate_settings USING btree (endpoint_id);
CREATE INDEX idx_fx_profile_model_endpoint ON endpoint_fx_rate_settings USING btree (profile_id, model_id, endpoint_id);
CREATE INDEX ix_endpoint_fx_rate_settings_profile_id ON endpoint_fx_rate_settings USING btree (profile_id);
CREATE INDEX idx_endpoints_profile_position ON endpoints USING btree (profile_id, "position");
CREATE INDEX ix_endpoints_profile_id ON endpoints USING btree (profile_id);
CREATE INDEX idx_hbr_enabled ON header_blocklist_rules USING btree (enabled);
CREATE INDEX ix_header_blocklist_rules_profile_id ON header_blocklist_rules USING btree (profile_id);
CREATE UNIQUE INDEX uq_hbr_system_match_pattern ON header_blocklist_rules USING btree (match_type, pattern) WHERE is_system = true;
CREATE INDEX idx_loadbalance_events_connection ON loadbalance_events USING btree (connection_id, created_at);
CREATE INDEX idx_loadbalance_events_event_type ON loadbalance_events USING btree (event_type);
CREATE INDEX idx_loadbalance_events_profile_created ON loadbalance_events USING btree (profile_id, created_at);
CREATE INDEX ix_loadbalance_events_created_at ON loadbalance_events USING btree (created_at);
CREATE INDEX ix_loadbalance_events_profile_id ON loadbalance_events USING btree (profile_id);
CREATE INDEX idx_loadbalance_round_robin_state_profile_model ON loadbalance_round_robin_state USING btree (profile_id, model_config_id);
CREATE INDEX idx_loadbalance_strategies_profile_id ON loadbalance_strategies USING btree (profile_id);
CREATE INDEX ix_loadbalance_strategies_profile_id ON loadbalance_strategies USING btree (profile_id);
CREATE INDEX idx_model_configs_loadbalance_strategy_id ON model_configs USING btree (loadbalance_strategy_id);
CREATE INDEX idx_model_configs_profile_model_enabled ON model_configs USING btree (profile_id, model_id, is_enabled);
CREATE INDEX ix_model_configs_profile_id ON model_configs USING btree (profile_id);
CREATE INDEX idx_model_proxy_targets_source_position ON model_proxy_targets USING btree (source_model_config_id, "position");
CREATE INDEX idx_model_proxy_targets_target_model ON model_proxy_targets USING btree (target_model_config_id);
CREATE INDEX idx_password_reset_challenges_consumed_at ON password_reset_challenges USING btree (consumed_at);
CREATE INDEX idx_password_reset_challenges_expires_at ON password_reset_challenges USING btree (expires_at);
CREATE INDEX ix_password_reset_challenges_auth_subject_id ON password_reset_challenges USING btree (auth_subject_id);
CREATE INDEX idx_pricing_templates_profile_id ON pricing_templates USING btree (profile_id);
CREATE INDEX ix_pricing_templates_profile_id ON pricing_templates USING btree (profile_id);
CREATE INDEX idx_profiles_deleted_at ON profiles USING btree (deleted_at);
CREATE UNIQUE INDEX uq_profiles_single_active ON profiles USING btree (is_active) WHERE is_active = true;
CREATE UNIQUE INDEX uq_profiles_single_default ON profiles USING btree (is_default) WHERE is_default = true;
CREATE INDEX idx_proxy_api_keys_is_active ON proxy_api_keys USING btree (is_active);
CREATE INDEX idx_refresh_tokens_expires_at ON refresh_tokens USING btree (expires_at);
CREATE INDEX idx_refresh_tokens_revoked_at ON refresh_tokens USING btree (revoked_at);
CREATE INDEX ix_refresh_tokens_auth_subject_id ON refresh_tokens USING btree (auth_subject_id);
CREATE INDEX idx_request_logs_billable_flag ON request_logs USING btree (billable_flag);
CREATE INDEX idx_request_logs_ingress_request_id ON request_logs USING btree (ingress_request_id);
CREATE INDEX idx_request_logs_priced_flag ON request_logs USING btree (priced_flag);
CREATE INDEX idx_request_logs_profile_created_at ON request_logs USING btree (profile_id, created_at);
CREATE INDEX ix_request_logs_api_family ON request_logs USING btree (api_family);
CREATE INDEX ix_request_logs_connection_id ON request_logs USING btree (connection_id);
CREATE INDEX ix_request_logs_created_at ON request_logs USING btree (created_at);
CREATE INDEX ix_request_logs_endpoint_id ON request_logs USING btree (endpoint_id);
CREATE INDEX ix_request_logs_model_id ON request_logs USING btree (model_id);
CREATE INDEX ix_request_logs_profile_id ON request_logs USING btree (profile_id);
CREATE INDEX ix_request_logs_proxy_api_key_id ON request_logs USING btree (proxy_api_key_id);
CREATE INDEX ix_request_logs_status_code ON request_logs USING btree (status_code);
CREATE INDEX ix_request_logs_vendor_id ON request_logs USING btree (vendor_id);
CREATE INDEX idx_routing_connection_runtime_leases_expires_at ON routing_connection_runtime_leases USING btree (expires_at);
CREATE INDEX idx_routing_connection_runtime_leases_profile_connection ON routing_connection_runtime_leases USING btree (profile_id, connection_id);
CREATE INDEX idx_routing_connection_runtime_state_profile_connection ON routing_connection_runtime_state USING btree (profile_id, connection_id);
CREATE INDEX idx_usage_request_events_ingress_request_id ON usage_request_events USING btree (ingress_request_id);
CREATE INDEX idx_usage_request_events_profile_created_at ON usage_request_events USING btree (profile_id, created_at);
CREATE INDEX ix_usage_request_events_api_family ON usage_request_events USING btree (api_family);
CREATE INDEX ix_usage_request_events_connection_id ON usage_request_events USING btree (connection_id);
CREATE INDEX ix_usage_request_events_created_at ON usage_request_events USING btree (created_at);
CREATE INDEX ix_usage_request_events_endpoint_id ON usage_request_events USING btree (endpoint_id);
CREATE INDEX ix_usage_request_events_model_id ON usage_request_events USING btree (model_id);
CREATE INDEX ix_usage_request_events_profile_id ON usage_request_events USING btree (profile_id);
CREATE INDEX ix_usage_request_events_proxy_api_key_id ON usage_request_events USING btree (proxy_api_key_id);
CREATE INDEX idx_uacr_enabled ON user_agent_client_rules USING btree (enabled);
CREATE INDEX ix_user_agent_client_rules_profile_id ON user_agent_client_rules USING btree (profile_id);
CREATE UNIQUE INDEX uq_uacr_system_pattern ON user_agent_client_rules USING btree (pattern) WHERE is_system = true;
CREATE INDEX ix_user_settings_profile_id ON user_settings USING btree (profile_id);
CREATE INDEX idx_webauthn_challenges_challenge_key ON webauthn_challenges USING btree (challenge_key);
CREATE INDEX idx_webauthn_challenges_expires_at ON webauthn_challenges USING btree (expires_at);
CREATE UNIQUE INDEX ix_webauthn_challenges_challenge_key ON webauthn_challenges USING btree (challenge_key);
CREATE INDEX idx_webauthn_credentials_auth_subject ON webauthn_credentials USING btree (auth_subject_id);
CREATE INDEX idx_webauthn_credentials_last_used ON webauthn_credentials USING btree (last_used_at);
