-- Quota inventory and cooldown schema. Clean break: remove hidden cursors,
-- rename probe observations into quota evidence, and expose the three-band
-- watchdog contract in policy, scan-run, and latest quota-state tables.

ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "probe_batch_cooldown_seconds" integer DEFAULT 30 NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "probe_last_batch_completed_at" timestamptz;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "probe_next_batch_after" timestamptz;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "error_priority" integer DEFAULT 0 NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "probe_jitter_min_ms" integer DEFAULT 100 NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "probe_jitter_max_ms" integer DEFAULT 1000 NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "cooldown_jitter_percent" integer DEFAULT 20 NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "quota_inventory_enabled" boolean DEFAULT true NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "initial_scan_enabled" boolean DEFAULT true NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "rolling_refresh_enabled" boolean DEFAULT true NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "rolling_refresh_after_seconds" integer DEFAULT 3600 NOT NULL;

ALTER TABLE ONLY "public"."sidecar_watchdog_policies" DROP CONSTRAINT "ck_sidecar_watchdog_policies_thresholds";
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" RENAME COLUMN "deprioritized_priority" TO "quota_exceeded_priority";
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" RENAME COLUMN "prioritized_priority" TO "using_priority";
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD CONSTRAINT "ck_sidecar_watchdog_policies_thresholds" CHECK (
    failure_threshold > 0
    AND failure_window_seconds > 0
    AND fallback_cooldown_seconds > 0
    AND manual_override_pause_seconds > 0
    AND quota_exceeded_priority >= 0
    AND using_priority >= 0
    AND error_priority >= 0
    AND quota_exceeded_priority <= using_priority
    AND error_priority <= using_priority
    AND probe_batch_size > 0
    AND probe_timeout_seconds > 0
    AND probe_timeout_seconds <= 25
    AND (probe_batch_size * probe_timeout_seconds) <= 25
    AND probe_batch_cooldown_seconds > 0
    AND probe_jitter_min_ms >= 0
    AND probe_jitter_max_ms >= probe_jitter_min_ms
    AND cooldown_jitter_percent >= 0
    AND cooldown_jitter_percent <= 100
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
ALTER TABLE ONLY "public"."sidecar_quota_probe_observations" ADD COLUMN "quota_band" text DEFAULT 'error' NOT NULL;
UPDATE "public"."sidecar_quota_probe_observations"
SET quota_band = CASE
    WHEN quota_exceeded THEN 'quota_exceeded'
    WHEN probe_status = 'probe_succeeded' THEN 'using'
    ELSE 'error'
END;
ALTER TABLE ONLY "public"."sidecar_quota_probe_observations" RENAME COLUMN "quota_reason" TO "reason_code";
ALTER TABLE ONLY "public"."sidecar_quota_probe_observations" ADD CONSTRAINT "ck_sidecar_quota_probe_observations_quota_band" CHECK (quota_band IN ('using', 'quota_exceeded', 'error'));

CREATE TABLE "public"."sidecar_quota_scan_runs" (
    "id" BIGSERIAL NOT NULL,
    "sidecar_id" integer NOT NULL,
    "scan_type" text NOT NULL,
    "status" text NOT NULL,
    "requested_by" text,
    "cursor_auth_id" text,
    "planned_count" integer DEFAULT 0 NOT NULL,
    "attempted_count" integer DEFAULT 0 NOT NULL,
    "using_count" integer DEFAULT 0 NOT NULL,
    "quota_exceeded_count" integer DEFAULT 0 NOT NULL,
    "error_count" integer DEFAULT 0 NOT NULL,
    "skipped_count" integer DEFAULT 0 NOT NULL,
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
    "quota_band" text NOT NULL,
    "probe_status" text,
    "quota_exceeded" boolean DEFAULT false NOT NULL,
    "reason_code" text,
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
ALTER TABLE ONLY "public"."sidecar_auth_quota_states" ADD CONSTRAINT "ck_sidecar_auth_quota_states_band" CHECK (quota_band IN ('using', 'quota_exceeded', 'error'));
ALTER TABLE ONLY "public"."sidecar_auth_quota_states" ADD CONSTRAINT "sidecar_auth_quota_states_sidecar_id_fkey" FOREIGN KEY (sidecar_id) REFERENCES "public"."sidecar_instances"(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."sidecar_auth_quota_states" ADD CONSTRAINT "sidecar_auth_quota_states_last_observation_id_fkey" FOREIGN KEY (last_observation_id) REFERENCES "public"."sidecar_quota_probe_observations"(id) ON DELETE SET NULL;
