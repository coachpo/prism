-- Clean-break sidecar watchdog probe concurrency. Preserve the existing
-- configured batch size once as probe_concurrency, then remove the old
-- serial batch-size policy column and timeout product rule.

ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "probe_concurrency" integer DEFAULT 3 NOT NULL;

UPDATE "public"."sidecar_watchdog_policies"
SET probe_concurrency = probe_batch_size;

ALTER TABLE ONLY "public"."sidecar_watchdog_policies" DROP CONSTRAINT "ck_sidecar_watchdog_policies_thresholds";
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" DROP COLUMN "probe_batch_size";
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
    AND probe_concurrency >= 1
    AND probe_concurrency <= 8
    AND probe_timeout_seconds > 0
    AND probe_timeout_seconds <= 25
    AND probe_batch_cooldown_seconds > 0
    AND probe_jitter_min_ms >= 0
    AND probe_jitter_max_ms >= probe_jitter_min_ms
    AND cooldown_jitter_percent >= 0
    AND cooldown_jitter_percent <= 100
    AND rolling_refresh_after_seconds > 0
);
