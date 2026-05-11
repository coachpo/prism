-- Probe-first sidecar watchdog quota state. Policies gain bounded probe
-- budgets plus an internal discovery cursor; probe observations are sanitized
-- append-only machine records separate from operator action history.

ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "prioritized_priority" integer DEFAULT 1 NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "probe_batch_size" integer DEFAULT 3 NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "probe_timeout_seconds" integer DEFAULT 8 NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "probe_cursor_auth_id" text;

UPDATE "public"."sidecar_watchdog_policies"
SET prioritized_priority = deprioritized_priority + 1
WHERE prioritized_priority <= deprioritized_priority;

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
);

CREATE TABLE "public"."sidecar_watchdog_probe_observations" (
    "id" SERIAL NOT NULL,
    "sidecar_id" integer NOT NULL,
    "auth_id" text NOT NULL,
    "auth_index" text,
    "provider" text,
    "probed_at" timestamptz NOT NULL,
    "probe_status" text NOT NULL,
    "upstream_status_code" integer,
    "quota_exceeded" boolean DEFAULT false NOT NULL,
    "quota_reason" text,
    "quota_reset_at" timestamptz,
    "blocking_window" text,
    "windows_json" jsonb DEFAULT '[]'::jsonb NOT NULL,
    "error_code" text,
    "created_at" timestamptz DEFAULT now() NOT NULL
);
ALTER TABLE ONLY "public"."sidecar_watchdog_probe_observations" ADD CONSTRAINT "sidecar_watchdog_probe_observations_pkey" PRIMARY KEY (id);
ALTER TABLE ONLY "public"."sidecar_watchdog_probe_observations" ADD CONSTRAINT "ck_sidecar_watchdog_probe_observations_required_text" CHECK (btrim(auth_id) <> '' AND btrim(probe_status) <> '');
ALTER TABLE ONLY "public"."sidecar_watchdog_probe_observations" ADD CONSTRAINT "ck_sidecar_watchdog_probe_observations_upstream_status" CHECK (upstream_status_code IS NULL OR (upstream_status_code >= 100 AND upstream_status_code <= 599));
ALTER TABLE ONLY "public"."sidecar_watchdog_probe_observations" ADD CONSTRAINT "ck_sidecar_watchdog_probe_observations_windows_array" CHECK (jsonb_typeof(windows_json) = 'array');
ALTER TABLE ONLY "public"."sidecar_watchdog_probe_observations" ADD CONSTRAINT "sidecar_watchdog_probe_observations_sidecar_id_fkey" FOREIGN KEY (sidecar_id) REFERENCES "public"."sidecar_instances"(id) ON DELETE CASCADE;

CREATE INDEX "idx_sidecar_watchdog_probe_observations_sidecar_probed" ON "public"."sidecar_watchdog_probe_observations" USING btree (sidecar_id, probed_at);
CREATE INDEX "idx_sidecar_watchdog_probe_observations_auth_probed" ON "public"."sidecar_watchdog_probe_observations" USING btree (auth_id, probed_at DESC);
CREATE INDEX "idx_sidecar_watchdog_probe_observations_probed_at" ON "public"."sidecar_watchdog_probe_observations" USING btree (probed_at);
