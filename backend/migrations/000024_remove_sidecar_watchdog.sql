-- Hard-delete the sidecar watchdog, quota inventory, and action-history schema.
-- Historical migrations remain intact; this terminal migration removes the live
-- watchdog-owned tables and the global retention setting for action history.

ALTER TABLE IF EXISTS ONLY "public"."sidecar_watchdog_policies"
    DROP CONSTRAINT IF EXISTS "sidecar_watchdog_policies_active_revision_id_fkey";
ALTER TABLE IF EXISTS ONLY "public"."sidecar_watchdog_policies"
    DROP CONSTRAINT IF EXISTS "sidecar_watchdog_policies_pending_revision_id_fkey";

DROP TABLE IF EXISTS "public"."sidecar_watchdog_sweep_items";
DROP TABLE IF EXISTS "public"."sidecar_watchdog_sweeps";
DROP TABLE IF EXISTS "public"."sidecar_watchdog_pending_actions";
DROP TABLE IF EXISTS "public"."sidecar_watchdog_actions";
DROP TABLE IF EXISTS "public"."sidecar_watchdog_holds";
DROP TABLE IF EXISTS "public"."sidecar_auth_quota_states";
DROP TABLE IF EXISTS "public"."sidecar_quota_scan_runs";
DROP TABLE IF EXISTS "public"."sidecar_quota_probe_observations";
DROP TABLE IF EXISTS "public"."sidecar_watchdog_policy_revisions";
DROP TABLE IF EXISTS "public"."sidecar_watchdog_policies";

ALTER TABLE IF EXISTS ONLY "public"."log_retention_settings"
    DROP CONSTRAINT IF EXISTS "log_retention_settings_sidecar_action_history_retention_days_check";
ALTER TABLE IF EXISTS ONLY "public"."log_retention_settings"
    DROP COLUMN IF EXISTS "sidecar_action_history_retention_days";
