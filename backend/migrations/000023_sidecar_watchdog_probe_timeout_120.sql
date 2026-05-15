-- Increase the watchdog probe timeout cap while preserving the worker's
-- 5-second safety margin. The low-priority worker runs with a 125s timeout,
-- so policy validation and final schema constraints accept at most 120s.

ALTER TABLE ONLY "public"."sidecar_watchdog_policies" DROP CONSTRAINT "ck_sidecar_watchdog_policies_thresholds";
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD CONSTRAINT "ck_sidecar_watchdog_policies_thresholds" CHECK (
    failure_threshold > 0
    AND failure_window_seconds > 0
    AND fallback_cooldown_seconds > 0
    AND manual_override_pause_seconds > 0
    AND quota_exceeded_priority >= 0
    AND using_priority >= 0
    AND quota_exceeded_priority <= using_priority
    AND working_priority >= 1
    AND empty_quota_priority >= 1
    AND initial_priority >= 1
    AND error_priority >= 1
    AND working_priority >= empty_quota_priority
    AND empty_quota_priority >= initial_priority
    AND initial_priority >= error_priority
    AND probe_concurrency >= 1
    AND probe_concurrency <= 8
    AND probe_timeout_seconds > 0
    AND probe_timeout_seconds <= 120
    AND probe_batch_cooldown_seconds > 0
    AND probe_jitter_min_ms >= 0
    AND probe_jitter_max_ms >= probe_jitter_min_ms
    AND cooldown_jitter_percent >= 0
    AND cooldown_jitter_percent <= 100
    AND rolling_refresh_after_seconds > 0
);

ALTER TABLE ONLY "public"."sidecar_watchdog_policy_revisions" DROP CONSTRAINT "ck_sidecar_watchdog_policy_revisions_values";
ALTER TABLE ONLY "public"."sidecar_watchdog_policy_revisions" ADD CONSTRAINT "ck_sidecar_watchdog_policy_revisions_values" CHECK (
    watchdog_sweep_interval_seconds > 0
    AND probe_concurrency >= 1
    AND probe_concurrency <= 8
    AND probe_timeout_seconds > 0
    AND probe_timeout_seconds <= 120
    AND probe_batch_cooldown_seconds > 0
    AND probe_jitter_min_ms >= 0
    AND probe_jitter_max_ms >= probe_jitter_min_ms
    AND cooldown_jitter_percent >= 0
    AND cooldown_jitter_percent <= 100
    AND using_priority >= 0
    AND quota_exceeded_priority >= 0
    AND quota_exceeded_priority <= using_priority
    AND working_priority >= 1
    AND empty_quota_priority >= 1
    AND initial_priority >= 1
    AND error_priority >= 1
    AND working_priority >= empty_quota_priority
    AND empty_quota_priority >= initial_priority
    AND initial_priority >= error_priority
    AND failure_threshold > 0
    AND failure_window_seconds > 0
    AND fallback_cooldown_seconds > 0
    AND manual_override_pause_seconds > 0
    AND rolling_refresh_after_seconds > 0
);
