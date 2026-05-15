CREATE TABLE "public"."sidecar_watchdog_policy_revisions" (
    "id" BIGSERIAL NOT NULL,
    "policy_id" integer NOT NULL,
    "sidecar_id" integer NOT NULL,
    "enabled" boolean DEFAULT false NOT NULL,
    "watchdog_sweep_interval_seconds" integer DEFAULT 3600 NOT NULL,
    "probe_concurrency" integer DEFAULT 3 NOT NULL,
    "probe_timeout_seconds" integer DEFAULT 8 NOT NULL,
    "probe_batch_cooldown_seconds" integer DEFAULT 30 NOT NULL,
    "probe_jitter_min_ms" integer DEFAULT 100 NOT NULL,
    "probe_jitter_max_ms" integer DEFAULT 1000 NOT NULL,
    "cooldown_jitter_percent" integer DEFAULT 20 NOT NULL,
    "using_priority" integer DEFAULT 1 NOT NULL,
    "quota_exceeded_priority" integer DEFAULT 0 NOT NULL,
    "error_priority" integer DEFAULT 0 NOT NULL,
    "failure_threshold" integer DEFAULT 3 NOT NULL,
    "failure_window_seconds" integer DEFAULT 3600 NOT NULL,
    "fallback_cooldown_seconds" integer DEFAULT 86400 NOT NULL,
    "manual_override_pause_seconds" integer DEFAULT 1800 NOT NULL,
    "quota_inventory_enabled" boolean DEFAULT true NOT NULL,
    "initial_scan_enabled" boolean DEFAULT true NOT NULL,
    "rolling_refresh_enabled" boolean DEFAULT true NOT NULL,
    "rolling_refresh_after_seconds" integer DEFAULT 3600 NOT NULL,
    "created_at" timestamptz DEFAULT now() NOT NULL
);
ALTER TABLE ONLY "public"."sidecar_watchdog_policy_revisions" ADD CONSTRAINT "sidecar_watchdog_policy_revisions_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."sidecar_watchdog_policy_revisions" ADD CONSTRAINT "sidecar_watchdog_policy_revisions_policy_id_fkey" FOREIGN KEY (policy_id) REFERENCES "public"."sidecar_watchdog_policies"(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."sidecar_watchdog_policy_revisions" ADD CONSTRAINT "sidecar_watchdog_policy_revisions_sidecar_id_fkey" FOREIGN KEY (sidecar_id) REFERENCES "public"."sidecar_instances"(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."sidecar_watchdog_policy_revisions" ADD CONSTRAINT "ck_sidecar_watchdog_policy_revisions_values" CHECK (
    watchdog_sweep_interval_seconds > 0
    AND probe_concurrency >= 1
    AND probe_concurrency <= 8
    AND probe_timeout_seconds > 0
    AND probe_timeout_seconds <= 25
    AND probe_batch_cooldown_seconds > 0
    AND probe_jitter_min_ms >= 0
    AND probe_jitter_max_ms >= probe_jitter_min_ms
    AND cooldown_jitter_percent >= 0
    AND cooldown_jitter_percent <= 100
    AND using_priority >= 0
    AND quota_exceeded_priority >= 0
    AND error_priority >= 0
    AND quota_exceeded_priority <= using_priority
    AND error_priority <= using_priority
    AND failure_threshold > 0
    AND failure_window_seconds > 0
    AND fallback_cooldown_seconds > 0
    AND manual_override_pause_seconds > 0
    AND rolling_refresh_after_seconds > 0
);
CREATE INDEX "idx_sidecar_watchdog_policy_revisions_sidecar" ON "public"."sidecar_watchdog_policy_revisions" USING btree (sidecar_id, id DESC);
CREATE INDEX "idx_sidecar_watchdog_policy_revisions_policy" ON "public"."sidecar_watchdog_policy_revisions" USING btree (policy_id, id DESC);

ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "active_revision_id" bigint;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "pending_revision_id" bigint;

WITH migrated_revisions AS (
    INSERT INTO "public"."sidecar_watchdog_policy_revisions" (
        policy_id, sidecar_id, enabled, watchdog_sweep_interval_seconds,
        probe_concurrency, probe_timeout_seconds, probe_batch_cooldown_seconds,
        probe_jitter_min_ms, probe_jitter_max_ms, cooldown_jitter_percent,
        using_priority, quota_exceeded_priority, error_priority,
        failure_threshold, failure_window_seconds, fallback_cooldown_seconds,
        manual_override_pause_seconds, quota_inventory_enabled, initial_scan_enabled,
        rolling_refresh_enabled, rolling_refresh_after_seconds
    )
    SELECT
        id, sidecar_id, enabled, rolling_refresh_after_seconds,
        probe_concurrency, probe_timeout_seconds, probe_batch_cooldown_seconds,
        probe_jitter_min_ms, probe_jitter_max_ms, cooldown_jitter_percent,
        using_priority, quota_exceeded_priority, error_priority,
        failure_threshold, failure_window_seconds, fallback_cooldown_seconds,
        manual_override_pause_seconds, quota_inventory_enabled, initial_scan_enabled,
        rolling_refresh_enabled, rolling_refresh_after_seconds
    FROM "public"."sidecar_watchdog_policies"
    RETURNING id, policy_id
)
UPDATE "public"."sidecar_watchdog_policies" policy
SET active_revision_id = migrated_revisions.id,
    pending_revision_id = NULL,
    updated_at = now()
FROM migrated_revisions
WHERE policy.id = migrated_revisions.policy_id;

ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD CONSTRAINT "sidecar_watchdog_policies_active_revision_id_fkey" FOREIGN KEY (active_revision_id) REFERENCES "public"."sidecar_watchdog_policy_revisions"(id) ON DELETE RESTRICT;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD CONSTRAINT "sidecar_watchdog_policies_pending_revision_id_fkey" FOREIGN KEY (pending_revision_id) REFERENCES "public"."sidecar_watchdog_policy_revisions"(id) ON DELETE SET NULL;
CREATE INDEX "idx_sidecar_watchdog_policies_active_revision" ON "public"."sidecar_watchdog_policies" USING btree (active_revision_id);
CREATE INDEX "idx_sidecar_watchdog_policies_pending_revision" ON "public"."sidecar_watchdog_policies" USING btree (pending_revision_id);

CREATE TABLE "public"."sidecar_watchdog_sweeps" (
    "sweep_id" text NOT NULL,
    "sidecar_id" integer NOT NULL,
    "policy_revision_id" bigint NOT NULL,
    "status" text NOT NULL,
    "snapshot_json" jsonb DEFAULT '[]'::jsonb NOT NULL,
    "next_item_index" integer DEFAULT 0 NOT NULL,
    "batch_index" integer DEFAULT 0 NOT NULL,
    "next_batch_after" timestamptz,
    "last_heartbeat_at" timestamptz,
    "lease_expires_at" timestamptz,
    "pause_reason" text,
    "failure_reason" text,
    "started_at" timestamptz DEFAULT now() NOT NULL,
    "completed_at" timestamptz,
    "created_at" timestamptz DEFAULT now() NOT NULL,
    "updated_at" timestamptz DEFAULT now() NOT NULL
);

ALTER TABLE ONLY "public"."sidecar_watchdog_sweeps" ADD CONSTRAINT "sidecar_watchdog_sweeps_pkey" PRIMARY KEY (sweep_id);
ALTER TABLE ONLY "public"."sidecar_watchdog_sweeps" ADD CONSTRAINT "sidecar_watchdog_sweeps_sidecar_id_fkey" FOREIGN KEY (sidecar_id) REFERENCES "public"."sidecar_instances"(id) ON DELETE CASCADE;
ALTER TABLE ONLY "public"."sidecar_watchdog_sweeps" ADD CONSTRAINT "sidecar_watchdog_sweeps_policy_revision_id_fkey" FOREIGN KEY (policy_revision_id) REFERENCES "public"."sidecar_watchdog_policy_revisions"(id) ON DELETE RESTRICT;
ALTER TABLE ONLY "public"."sidecar_watchdog_sweeps" ADD CONSTRAINT "ck_sidecar_watchdog_sweeps_status" CHECK (status IN ('running', 'paused', 'completed', 'failed', 'cancelled'));
ALTER TABLE ONLY "public"."sidecar_watchdog_sweeps" ADD CONSTRAINT "ck_sidecar_watchdog_sweeps_checkpoint" CHECK (next_item_index >= 0 AND batch_index >= 0 AND jsonb_typeof(snapshot_json) = 'array');
CREATE UNIQUE INDEX "uq_sidecar_watchdog_sweeps_active_sidecar" ON "public"."sidecar_watchdog_sweeps" USING btree (sidecar_id) WHERE status IN ('running', 'paused');
CREATE INDEX "idx_sidecar_watchdog_sweeps_sidecar_status" ON "public"."sidecar_watchdog_sweeps" USING btree (sidecar_id, status);
CREATE INDEX "idx_sidecar_watchdog_sweeps_revision" ON "public"."sidecar_watchdog_sweeps" USING btree (policy_revision_id);
