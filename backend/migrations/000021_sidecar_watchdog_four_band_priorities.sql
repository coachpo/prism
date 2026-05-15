-- Finalize watchdog priority response bands as editable persisted policy
-- fields. Legacy using/quota columns remain for older API shape, but
-- public priority-state derivation now reads the four-band thresholds.

ALTER TABLE ONLY "public"."sidecar_watchdog_policies" DROP CONSTRAINT "ck_sidecar_watchdog_policies_thresholds";
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "working_priority" integer DEFAULT 99 NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "empty_quota_priority" integer DEFAULT 90 NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ADD COLUMN "initial_priority" integer DEFAULT 50 NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ALTER COLUMN "using_priority" SET DEFAULT 99;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ALTER COLUMN "quota_exceeded_priority" SET DEFAULT 90;
ALTER TABLE ONLY "public"."sidecar_watchdog_policies" ALTER COLUMN "error_priority" SET DEFAULT 10;
UPDATE "public"."sidecar_watchdog_policies" SET using_priority = 99 WHERE using_priority = 1;
UPDATE "public"."sidecar_watchdog_policies" SET quota_exceeded_priority = 90 WHERE quota_exceeded_priority = 0;
UPDATE "public"."sidecar_watchdog_policies" SET error_priority = 10 WHERE error_priority = 0;

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
    AND probe_timeout_seconds <= 25
    AND probe_batch_cooldown_seconds > 0
    AND probe_jitter_min_ms >= 0
    AND probe_jitter_max_ms >= probe_jitter_min_ms
    AND cooldown_jitter_percent >= 0
    AND cooldown_jitter_percent <= 100
    AND rolling_refresh_after_seconds > 0
);

ALTER TABLE ONLY "public"."sidecar_watchdog_policy_revisions" ADD COLUMN "working_priority" integer DEFAULT 99 NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policy_revisions" ADD COLUMN "empty_quota_priority" integer DEFAULT 90 NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policy_revisions" ADD COLUMN "initial_priority" integer DEFAULT 50 NOT NULL;
ALTER TABLE ONLY "public"."sidecar_watchdog_policy_revisions" ALTER COLUMN "using_priority" SET DEFAULT 99;
ALTER TABLE ONLY "public"."sidecar_watchdog_policy_revisions" ALTER COLUMN "quota_exceeded_priority" SET DEFAULT 90;
ALTER TABLE ONLY "public"."sidecar_watchdog_policy_revisions" ALTER COLUMN "error_priority" SET DEFAULT 10;
UPDATE "public"."sidecar_watchdog_policy_revisions" SET using_priority = 99 WHERE using_priority = 1;
UPDATE "public"."sidecar_watchdog_policy_revisions" SET quota_exceeded_priority = 90 WHERE quota_exceeded_priority = 0;
UPDATE "public"."sidecar_watchdog_policy_revisions" SET error_priority = 10 WHERE error_priority = 0;

ALTER TABLE ONLY "public"."sidecar_watchdog_policy_revisions" DROP CONSTRAINT "ck_sidecar_watchdog_policy_revisions_values";
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
