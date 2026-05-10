-- Clean sidecar-control-plane domain. CLIProxyAPI remains the source of truth
-- for live auth/provider state; Prism stores instances, observations, and
-- watchdog reconciliation state in isolated sidecar tables.

CREATE TABLE "public"."sidecar_instances" (
    "id" SERIAL NOT NULL,
    "name" text NOT NULL,
    "base_url" text NOT NULL,
    "base_url_canonical" text NOT NULL,
    "management_password" text NOT NULL,
    "enabled" boolean DEFAULT true NOT NULL,
    "environment_label" text,
    "sync_interval_seconds" integer DEFAULT 300 NOT NULL,
    "request_timeout_seconds" integer DEFAULT 10 NOT NULL,
    "allow_private_network" boolean DEFAULT false NOT NULL,
    "allow_insecure_http" boolean DEFAULT false NOT NULL,
    "skip_tls_verify" boolean DEFAULT false NOT NULL,
    "last_sync_at" timestamptz,
    "last_successful_sync_at" timestamptz,
    "snapshot_stale_after" timestamptz,
    "last_sync_error" text,
    "management_auth_state" text DEFAULT 'unknown' NOT NULL,
    "auth_failure_pause_until" timestamptz,
    "deleted_at" timestamptz,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL
);

CREATE TABLE "public"."sidecar_auth_snapshots" (
    "id" SERIAL NOT NULL,
    "sidecar_id" integer NOT NULL,
    "auth_id" text NOT NULL,
    "auth_index" text,
    "name" text NOT NULL,
    "provider" text,
    "label" text,
    "status" text,
    "status_message" text,
    "disabled" boolean,
    "unavailable" boolean,
    "priority" integer,
    "quota_exceeded" boolean,
    "quota_reason" text,
    "quota_next_recover_at" timestamptz,
    "next_retry_after" timestamptz,
    "success_count" integer,
    "failed_count" integer,
    "recent_requests_json" jsonb DEFAULT '[]'::jsonb NOT NULL,
    "model_states_json" jsonb DEFAULT '{}'::jsonb NOT NULL,
    "snapshot_json" jsonb DEFAULT '{}'::jsonb NOT NULL,
    "observed_at" timestamptz NOT NULL,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL
);

CREATE TABLE "public"."sidecar_provider_snapshots" (
    "id" SERIAL NOT NULL,
    "sidecar_id" integer NOT NULL,
    "provider_key" text NOT NULL,
    "provider_item_key" text NOT NULL,
    "name" text,
    "label" text,
    "status" text,
    "disabled" boolean,
    "snapshot_json" jsonb DEFAULT '{}'::jsonb NOT NULL,
    "observed_at" timestamptz NOT NULL,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL
);

CREATE TABLE "public"."sidecar_watchdog_policies" (
    "id" SERIAL NOT NULL,
    "sidecar_id" integer NOT NULL,
    "enabled" boolean DEFAULT false NOT NULL,
    "failure_threshold" integer DEFAULT 3 NOT NULL,
    "failure_window_seconds" integer DEFAULT 3600 NOT NULL,
    "fallback_cooldown_seconds" integer DEFAULT 86400 NOT NULL,
    "deprioritized_priority" integer DEFAULT 0 NOT NULL,
    "manual_override_pause_seconds" integer DEFAULT 1800 NOT NULL,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL
);

CREATE TABLE "public"."sidecar_watchdog_holds" (
    "id" SERIAL NOT NULL,
    "sidecar_id" integer NOT NULL,
    "auth_id" text NOT NULL,
    "auth_index" text,
    "provider" text,
    "reason" text NOT NULL,
    "condition_hash" text NOT NULL,
    "previous_priority" integer,
    "target_priority" integer NOT NULL,
    "hold_until" timestamptz,
    "manual_pause_until" timestamptz,
    "status" text NOT NULL,
    "last_action_id" integer,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL,
    "released_at" timestamptz
);

CREATE TABLE "public"."sidecar_watchdog_actions" (
    "id" SERIAL NOT NULL,
    "sidecar_id" integer NOT NULL,
    "auth_snapshot_id" integer,
    "hold_id" integer,
    "auth_id" text,
    "auth_index" text,
    "provider" text,
    "action_type" text NOT NULL,
    "reason" text,
    "previous_priority" integer,
    "target_priority" integer,
    "hold_until" timestamptz,
    "status" text NOT NULL,
    "error_message" text,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL,
    "completed_at" timestamptz
);

ALTER TABLE ONLY "public"."sidecar_instances" ADD CONSTRAINT "sidecar_instances_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."sidecar_instances" ADD CONSTRAINT "ck_sidecar_instances_sync_interval_positive" CHECK (sync_interval_seconds > 0);
ALTER TABLE ONLY "public"."sidecar_instances" ADD CONSTRAINT "ck_sidecar_instances_request_timeout_positive" CHECK (request_timeout_seconds > 0);
ALTER TABLE ONLY "public"."sidecar_instances" ADD CONSTRAINT "ck_sidecar_instances_management_auth_state" CHECK (management_auth_state IN ('unknown', 'valid', 'invalid_management_auth'));
ALTER TABLE ONLY "public"."sidecar_auth_snapshots" ADD CONSTRAINT "sidecar_auth_snapshots_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."sidecar_auth_snapshots" ADD CONSTRAINT "uq_sidecar_auth_snapshots_key" UNIQUE (sidecar_id, auth_id);
ALTER TABLE ONLY "public"."sidecar_provider_snapshots" ADD CONSTRAINT "sidecar_provider_snapshots_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."sidecar_provider_snapshots" ADD CONSTRAINT "uq_sidecar_provider_snapshots_key" UNIQUE (sidecar_id, provider_key, provider_item_key);
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD CONSTRAINT "sidecar_watchdog_policies_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD CONSTRAINT "uq_sidecar_watchdog_policies_sidecar_id" UNIQUE (sidecar_id);
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD CONSTRAINT "ck_sidecar_watchdog_policies_thresholds" CHECK (failure_threshold > 0 AND failure_window_seconds > 0 AND fallback_cooldown_seconds > 0 AND manual_override_pause_seconds > 0 AND deprioritized_priority >= 0);
ALTER TABLE ONLY "public"."sidecar_watchdog_holds" ADD CONSTRAINT "sidecar_watchdog_holds_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."sidecar_watchdog_actions" ADD CONSTRAINT "sidecar_watchdog_actions_pkey" PRIMARY KEY (id);

ALTER TABLE ONLY "public"."sidecar_auth_snapshots" ADD CONSTRAINT "sidecar_auth_snapshots_sidecar_id_fkey" FOREIGN KEY (sidecar_id) REFERENCES "public"."sidecar_instances"(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."sidecar_provider_snapshots" ADD CONSTRAINT "sidecar_provider_snapshots_sidecar_id_fkey" FOREIGN KEY (sidecar_id) REFERENCES "public"."sidecar_instances"(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD CONSTRAINT "sidecar_watchdog_policies_sidecar_id_fkey" FOREIGN KEY (sidecar_id) REFERENCES "public"."sidecar_instances"(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."sidecar_watchdog_holds" ADD CONSTRAINT "sidecar_watchdog_holds_sidecar_id_fkey" FOREIGN KEY (sidecar_id) REFERENCES "public"."sidecar_instances"(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."sidecar_watchdog_actions" ADD CONSTRAINT "sidecar_watchdog_actions_sidecar_id_fkey" FOREIGN KEY (sidecar_id) REFERENCES "public"."sidecar_instances"(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."sidecar_watchdog_actions" ADD CONSTRAINT "sidecar_watchdog_actions_auth_snapshot_id_fkey" FOREIGN KEY (auth_snapshot_id) REFERENCES "public"."sidecar_auth_snapshots"(id) ON DELETE SET NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_actions" ADD CONSTRAINT "sidecar_watchdog_actions_hold_id_fkey" FOREIGN KEY (hold_id) REFERENCES "public"."sidecar_watchdog_holds"(id) ON DELETE SET NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_holds" ADD CONSTRAINT "sidecar_watchdog_holds_last_action_id_fkey" FOREIGN KEY (last_action_id) REFERENCES "public"."sidecar_watchdog_actions"(id) ON DELETE SET NULL;

CREATE UNIQUE INDEX "uq_sidecar_instances_live_name" ON "public"."sidecar_instances" USING btree (lower(name)) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX "uq_sidecar_instances_live_base_url_canonical" ON "public"."sidecar_instances" USING btree (base_url_canonical) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX "uq_sidecar_watchdog_holds_active_auth" ON "public"."sidecar_watchdog_holds" USING btree (sidecar_id, auth_id) WHERE status IN ('active', 'paused');
CREATE INDEX "idx_sidecar_auth_snapshots_sidecar_id" ON "public"."sidecar_auth_snapshots" USING btree (sidecar_id);
CREATE INDEX "idx_sidecar_provider_snapshots_sidecar_id" ON "public"."sidecar_provider_snapshots" USING btree (sidecar_id);
CREATE INDEX "idx_sidecar_watchdog_holds_sidecar_status" ON "public"."sidecar_watchdog_holds" USING btree (sidecar_id, status);
CREATE INDEX "idx_sidecar_watchdog_actions_sidecar_created" ON "public"."sidecar_watchdog_actions" USING btree (sidecar_id, created_at DESC);
