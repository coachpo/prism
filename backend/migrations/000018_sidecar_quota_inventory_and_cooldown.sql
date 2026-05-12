-- Quota inventory and cooldown schema. Clean break: remove the hidden policy
-- cursor, rename probe observations into quota evidence, and add scan-run plus
-- latest quota-state tables for resumable inventory work.

ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "probe_batch_cooldown_seconds" integer DEFAULT 30 NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "probe_last_batch_completed_at" timestamptz;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "quota_inventory_enabled" boolean DEFAULT true NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "initial_scan_enabled" boolean DEFAULT true NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "rolling_refresh_enabled" boolean DEFAULT true NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "rolling_refresh_after_seconds" integer DEFAULT 3600 NOT NULL;

ALTER TABLE ONLY "public"."sidecar_watchdog_policies" DROP CONSTRAINT "ck_sidecar_watchdog_policies_thresholds";
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD CONSTRAINT "ck_sidecar_watchdog_policies_thresholds" CHECK (
    failure_threshold > 0
    AND failure_window_seconds > 0
    AND fallback_cooldown_seconds > 0
    AND manual_override_pause_seconds > 0
    AND deprioritized_priority >= 0
    AND prioritized_priority >= 0
    AND deprioritized_priority < prioritized_priority
    AND probe_batch_size > 0
    AND probe_timeout_seconds > 0
    AND probe_timeout_seconds <= 25
    AND (probe_batch_size * probe_timeout_seconds) <= 25
    AND probe_batch_cooldown_seconds > 0
    AND rolling_refresh_after_seconds > 0
);

ALTER TABLE ONLY "public"."sidecar_watchdog_policies" DROP COLUMN "probe_cursor_auth_id";

ALTER TABLE ONLY "public"."sidecar_watchdog_probe_observations" RENAME TO "sidecar_quota_probe_observations";
ALTER SEQUENCE "public"."sidecar_watchdog_probe_observations_id_seq" RENAME TO "sidecar_quota_probe_observations_id_seq";
ALTER TABLE ONLY "public"."sidecar_quota_probe_observations" RENAME CONSTRAINT "sidecar_watchdog_probe_observations_pkey" TO "sidecar_quota_probe_observations_pkey";
ALTER TABLE ONLY "public"."sidecar_quota_probe_observations" RENAME CONSTRAINT "ck_sidecar_watchdog_probe_observations_required_text" TO "ck_sidecar_quota_probe_observations_required_text";
ALTER TABLE ONLY "public"."sidecar_quota_probe_observations" RENAME CONSTRAINT "ck_sidecar_watchdog_probe_observations_upstream_status" TO "ck_sidecar_quota_probe_observations_upstream_status";
ALTER TABLE ONLY "public"."sidecar_quota_probe_observations" RENAME CONSTRAINT "ck_sidecar_watchdog_probe_observations_windows_array" TO "ck_sidecar_quota_probe_observations_windows_array";
ALTER TABLE ONLY "public"."sidecar_quota_probe_observations" RENAME CONSTRAINT "sidecar_watchdog_probe_observations_sidecar_id_fkey" TO "sidecar_quota_probe_observations_sidecar_id_fkey";
ALTER INDEX "public"."idx_sidecar_watchdog_probe_observations_sidecar_probed" RENAME TO "idx_sidecar_quota_probe_observations_sidecar_probed";
ALTER INDEX "public"."idx_sidecar_watchdog_probe_observations_auth_probed" RENAME TO "idx_sidecar_quota_probe_observations_auth_probed";
ALTER INDEX "public"."idx_sidecar_watchdog_probe_observations_probed_at" RENAME TO "idx_sidecar_quota_probe_observations_probed_at";

CREATE TABLE "public"."sidecar_quota_scan_runs" (
    "id" BIGSERIAL NOT NULL,
    "sidecar_id" integer NOT NULL,
    "scan_type" text NOT NULL,
    "status" text NOT NULL,
    "requested_by" text,
    "cursor_auth_id" text,
    "planned_count" integer DEFAULT 0 NOT NULL,
    "attempted_count" integer DEFAULT 0 NOT NULL,
    "succeeded_count" integer DEFAULT 0 NOT NULL,
    "quota_exceeded_count" integer DEFAULT 0 NOT NULL,
    "failed_count" integer DEFAULT 0 NOT NULL,
    "unsupported_count" integer DEFAULT 0 NOT NULL,
    "missing_index_count" integer DEFAULT 0 NOT NULL,
    "cancel_requested_at" timestamptz,
    "started_at" timestamptz,
    "completed_at" timestamptz,
    "last_error_code" text,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL
);

CREATE TABLE "public"."sidecar_auth_quota_states" (
    "sidecar_id" integer NOT NULL,
    "auth_id" text NOT NULL,
    "auth_index" text,
    "auth_name" text,
    "provider" text,
    "snapshot_observed_at" timestamptz,
    "state" text NOT NULL,
    "probe_status" text,
    "quota_exceeded" boolean DEFAULT false NOT NULL,
    "quota_reason" text,
    "quota_reset_at" timestamptz,
    "blocking_window" text,
    "last_observation_id" integer,
    "last_probed_at" timestamptz,
    "last_error_code" text,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL
);

ALTER TABLE ONLY "public"."sidecar_quota_scan_runs" ADD CONSTRAINT "sidecar_quota_scan_runs_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."sidecar_quota_scan_runs" ADD CONSTRAINT "ck_sidecar_quota_scan_runs_scan_type" CHECK (scan_type IN ('initial', 'manual', 'scheduled'));
ALTER TABLE ONLY "public"."sidecar_quota_scan_runs" ADD CONSTRAINT "ck_sidecar_quota_scan_runs_status" CHECK (status IN ('queued', 'running', 'completed', 'cancelled', 'failed'));
ALTER TABLE ONLY "public"."sidecar_quota_scan_runs" ADD CONSTRAINT "sidecar_quota_scan_runs_sidecar_id_fkey" FOREIGN KEY (sidecar_id) REFERENCES "public"."sidecar_instances"(id) ON DELETE CASCADE;
CREATE UNIQUE INDEX "uq_sidecar_quota_scan_runs_active_sidecar" ON "public"."sidecar_quota_scan_runs" USING btree (sidecar_id) WHERE status IN ('queued', 'running');

ALTER TABLE ONLY "public"."sidecar_auth_quota_states" ADD CONSTRAINT "sidecar_auth_quota_states_pkey" PRIMARY KEY (sidecar_id, auth_id);
ALTER TABLE ONLY "public"."sidecar_auth_quota_states" ADD CONSTRAINT "ck_sidecar_auth_quota_states_state" CHECK (state IN ('unknown', 'healthy', 'quota_exceeded', 'probe_failed', 'unsupported', 'missing_auth_index', 'disabled', 'missing'));
ALTER TABLE ONLY "public"."sidecar_auth_quota_states" ADD CONSTRAINT "sidecar_auth_quota_states_sidecar_id_fkey" FOREIGN KEY (sidecar_id) REFERENCES "public"."sidecar_instances"(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."sidecar_auth_quota_states" ADD CONSTRAINT "sidecar_auth_quota_states_last_observation_id_fkey" FOREIGN KEY (last_observation_id) REFERENCES "public"."sidecar_quota_probe_observations"(id) ON DELETE SET NULL;
